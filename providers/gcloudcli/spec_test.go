package gcloudcli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/takezoh/credproxy/container"
	"github.com/takezoh/credproxy/internal/testenv"
)

func newTestConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		RunBase:         t.TempDir(),
		ContainerRunDir: "/run/credproxy",
	}
}

func emptyGCPConfig(string) GCPConfig { return GCPConfig{} }

// newStubbedBuilder returns a SpecBuilder whose metadata servers serve over a
// loopback listener instead of a Unix socket, plus an accessor for the socket
// paths ContainerSpec asked to bind.
//
// What these tests check is spec construction and the per-project bookkeeping
// behind it: the token targets that Materialize and the refresh sweep write
// through, deduplicated per project. The Unix socket only carries the metadata
// server; where AF_UNIX is denied, none of that bookkeeping ever happens and a
// real regression in it would look exactly like the environment. The bound
// paths keep the transport wiring observable all the same — a builder that
// bound the wrong path is still caught. Real Unix-socket behavior is covered by
// TestSpecBuilder_metadataServer_servesOverUnixSocket.
//
// The builder's root context is cancelled at cleanup, which is what stops each
// metadata server, so no listener outlives its test.
func newStubbedBuilder(t *testing.T, cfg Config, gcpFor func(string) GCPConfig) (*SpecBuilder, func() []string) {
	t.Helper()

	var mu sync.Mutex
	var bound []string
	prev := metadataListen
	t.Cleanup(func() { metadataListen = prev })
	metadataListen = func(sockPath string) (net.Listener, error) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, fmt.Errorf("stub metadata listener: %w", err)
		}
		mu.Lock()
		bound = append(bound, sockPath)
		mu.Unlock()
		return ln, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return NewSpecBuilder(ctx, cfg, gcpFor), func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), bound...)
	}
}

// metaSockHostPath is the per-project Unix socket ContainerSpec must bind.
func metaSockHostPath(cfg Config, projectPath string) string {
	return filepath.Join(cfg.RunBase, container.ProjectRunHash(projectPath), "gcp-metadata.sock")
}

func TestSpecBuilder_emptyConfig_zeroSpec(t *testing.T) {
	cfg := newTestConfig(t)
	b := NewSpecBuilder(context.Background(), cfg, emptyGCPConfig)
	spec, err := b.ContainerSpec(context.Background(), "/proj")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(spec.Env) != 0 || len(spec.Mounts) != 0 || len(spec.BridgeSpecs) != 0 {
		t.Errorf("expected zero spec, got env=%v mounts=%v bridges=%v", spec.Env, spec.Mounts, spec.BridgeSpecs)
	}
}

func TestSpecBuilder_missingAccount_returnsError(t *testing.T) {
	cfg := newTestConfig(t)
	b := NewSpecBuilder(context.Background(), cfg, func(string) GCPConfig {
		return GCPConfig{Active: "proj-x"}
	})
	_, err := b.ContainerSpec(context.Background(), "/proj")
	if err == nil {
		t.Fatal("expected error when account is missing, got nil")
	}
}

func TestSpecBuilder_missingActive_returnsError(t *testing.T) {
	cfg := newTestConfig(t)
	b := NewSpecBuilder(context.Background(), cfg, func(string) GCPConfig {
		return GCPConfig{Account: "user@example.com"}
	})
	_, err := b.ContainerSpec(context.Background(), "/proj")
	if err == nil {
		t.Fatal("expected error when active is missing, got nil")
	}
}

func TestSpecBuilder_SAMode_missingProjects_returnsError(t *testing.T) {
	cfg := newTestConfig(t)
	b := NewSpecBuilder(context.Background(), cfg, func(string) GCPConfig {
		return GCPConfig{
			Account:        "user@example.com",
			ServiceAccount: "sa@proj.iam.gserviceaccount.com",
			Active:         "proj-x",
		}
	})
	_, err := b.ContainerSpec(context.Background(), "/proj")
	if err == nil {
		t.Fatal("expected error when SA mode has no projects, got nil")
	}
}

func TestSpecBuilder_userAccountProxy_injectsEnvAndBridgeSpec(t *testing.T) {
	cfg := newTestConfig(t)
	b, bound := newStubbedBuilder(t, cfg, func(string) GCPConfig {
		return GCPConfig{
			Account: "user@example.com",
			Active:  "proj-x",
		}
	})

	spec, err := b.ContainerSpec(context.Background(), "/myproject")
	if err != nil {
		t.Fatalf("ContainerSpec: %v", err)
	}

	wantConfigPath := cfg.ContainerRunDir + "/gcloud-config"
	if spec.Env[ConfigDirEnv] != wantConfigPath {
		t.Errorf("env[%s] = %q, want %q", ConfigDirEnv, spec.Env[ConfigDirEnv], wantConfigPath)
	}
	if spec.Env[MetadataHostEnv] != metadataListenAddr {
		t.Errorf("env[%s] = %q, want %q", MetadataHostEnv, spec.Env[MetadataHostEnv], metadataListenAddr)
	}
	if len(spec.BridgeSpecs) != 1 {
		t.Fatalf("expected 1 BridgeSpec, got %d", len(spec.BridgeSpecs))
	}
	if spec.BridgeSpecs[0].ListenAddr != metadataListenAddr {
		t.Errorf("BridgeSpec.ListenAddr = %q, want %q", spec.BridgeSpecs[0].ListenAddr, metadataListenAddr)
	}
	wantSock := cfg.ContainerRunDir + "/gcp-metadata.sock"
	if spec.BridgeSpecs[0].ContainerSocketPath != wantSock {
		t.Errorf("BridgeSpec.ContainerSocketPath = %q, want %q", spec.BridgeSpecs[0].ContainerSocketPath, wantSock)
	}
	// The container-side path above is only half the bridge: the host side must
	// be this project's own socket, or the bridge lands on the wrong project.
	wantHostSock := metaSockHostPath(cfg, "/myproject")
	if got := bound(); len(got) != 1 || got[0] != wantHostSock {
		t.Errorf("bound metadata sockets = %v, want [%s]", got, wantHostSock)
	}
}

func TestSpecBuilder_userAccountProxy_tokenPathInConfig(t *testing.T) {
	cfg := newTestConfig(t)
	b, _ := newStubbedBuilder(t, cfg, func(string) GCPConfig {
		return GCPConfig{Account: "user@example.com", Active: "proj-x"}
	})

	if _, err := b.ContainerSpec(context.Background(), "/myproject"); err != nil {
		t.Fatalf("ContainerSpec: %v", err)
	}

	projectDir := filepath.Join(cfg.RunBase, container.ProjectRunHash("/myproject"))
	configFile := filepath.Join(projectDir, "gcloud-config", "configurations", "config_proj-x")
	data, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	wantTokenPath := cfg.ContainerRunDir + "/gcloud-token"
	if !strings.Contains(string(data), wantTokenPath) {
		t.Errorf("config missing access_token_file path %q; content:\n%s", wantTokenPath, data)
	}
}

func TestSpecBuilder_userAccountProxy_configContainsUserAccount(t *testing.T) {
	cfg := newTestConfig(t)
	b, _ := newStubbedBuilder(t, cfg, func(string) GCPConfig {
		return GCPConfig{
			Account: "user@example.com",
			Active:  "proj-x",
		}
	})

	if _, err := b.ContainerSpec(context.Background(), "/myproject"); err != nil {
		t.Fatalf("ContainerSpec: %v", err)
	}

	projectDir := filepath.Join(cfg.RunBase, container.ProjectRunHash("/myproject"))
	configFile := filepath.Join(projectDir, "gcloud-config", "configurations", "config_proj-x")
	data, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "user@example.com") {
		t.Errorf("config file does not contain user account; content:\n%s", content)
	}
	if !strings.Contains(content, "access_token_file") {
		t.Errorf("config file must contain access_token_file; content:\n%s", content)
	}
}

func TestSpecBuilder_userAccountProxy_activeIsOnlyProject(t *testing.T) {
	cfg := newTestConfig(t)
	b, _ := newStubbedBuilder(t, cfg, func(string) GCPConfig {
		return GCPConfig{Account: "user@example.com", Active: "general"}
	})

	if _, err := b.ContainerSpec(context.Background(), "/myproject"); err != nil {
		t.Fatalf("ContainerSpec: %v", err)
	}

	projectDir := filepath.Join(cfg.RunBase, container.ProjectRunHash("/myproject"))
	active, err := os.ReadFile(filepath.Join(projectDir, "gcloud-config", "active_config"))
	if err != nil {
		t.Fatalf("read active_config: %v", err)
	}
	if string(active) != "general" {
		t.Errorf("active_config = %q, want %q", string(active), "general")
	}
	if _, err := os.Stat(filepath.Join(projectDir, "gcloud-config", "configurations", "config_general")); err != nil {
		t.Errorf("config_general not created: %v", err)
	}
}

func TestSpecBuilder_withConfig_injectsEnvAndBridgeSpec(t *testing.T) {
	cfg := newTestConfig(t)
	b, _ := newStubbedBuilder(t, cfg, func(string) GCPConfig {
		return GCPConfig{
			ServiceAccount: "sa@proj.iam.gserviceaccount.com",
			Account:        "user@example.com",
			Active:         "proj-a",
			Projects:       []string{"proj-a", "proj-b"},
		}
	})

	spec, err := b.ContainerSpec(context.Background(), "/myproject")
	if err != nil {
		t.Fatalf("ContainerSpec: %v", err)
	}

	wantConfigPath := cfg.ContainerRunDir + "/gcloud-config"
	if spec.Env[ConfigDirEnv] != wantConfigPath {
		t.Errorf("env[%s] = %q, want %q", ConfigDirEnv, spec.Env[ConfigDirEnv], wantConfigPath)
	}
	if len(spec.Mounts) != 0 {
		t.Errorf("expected 0 mounts, got %d: %v", len(spec.Mounts), spec.Mounts)
	}
	if len(spec.BridgeSpecs) != 1 {
		t.Errorf("expected 1 BridgeSpec, got %d", len(spec.BridgeSpecs))
	}
}

func TestSpecBuilder_metadataServerDeduplication(t *testing.T) {
	cfg := newTestConfig(t)
	gcpCfg := GCPConfig{
		ServiceAccount: "sa@proj.iam.gserviceaccount.com",
		Account:        "user@example.com",
		Active:         "p",
		Projects:       []string{"p"},
	}
	b, bound := newStubbedBuilder(t, cfg, func(string) GCPConfig { return gcpCfg })

	if _, err := b.ContainerSpec(context.Background(), "/p1"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ContainerSpec(context.Background(), "/p1"); err != nil {
		t.Fatal(err)
	}

	b.mu.Lock()
	count := len(b.tokenTargets)
	b.mu.Unlock()

	if count != 1 {
		t.Errorf("expected 1 metadata server for same project, got %d", count)
	}
	if got := bound(); len(got) != 1 {
		t.Errorf("bound metadata sockets = %v, want exactly one listener for a repeated project", got)
	}
}

func TestSpecBuilder_refreshAllTokens_writesAllProjects(t *testing.T) {
	cfg := newTestConfig(t)
	var callCount atomic.Int32
	b, _ := newStubbedBuilder(t, cfg, func(string) GCPConfig {
		return GCPConfig{Account: "user@example.com", Active: "proj-x"}
	})
	b.gcpToken = func(_ context.Context, _, _ string) (string, error) {
		callCount.Add(1)
		return "fresh-token", nil
	}

	for _, proj := range []string{"/proj-a", "/proj-b"} {
		if _, err := b.ContainerSpec(context.Background(), proj); err != nil {
			t.Fatalf("ContainerSpec(%s): %v", proj, err)
		}
	}
	callCount.Store(0)

	if err := b.refreshAllTokens(context.Background()); err != nil {
		t.Fatalf("refreshAllTokens: %v", err)
	}
	if got := callCount.Load(); got != 2 {
		t.Errorf("expected 2 token writes (one per project), got %d", got)
	}
}

func TestSpecBuilder_refreshAllTokens_updatesFileContent(t *testing.T) {
	cfg := newTestConfig(t)
	b, _ := newStubbedBuilder(t, cfg, func(string) GCPConfig {
		return GCPConfig{Account: "user@example.com", Active: "proj-x"}
	})
	b.gcpToken = func(_ context.Context, _, _ string) (string, error) { return "stale-token", nil }

	if _, err := b.ContainerSpec(context.Background(), "/proj"); err != nil {
		t.Fatalf("ContainerSpec: %v", err)
	}

	b.gcpToken = func(_ context.Context, _, _ string) (string, error) { return "new-token", nil }
	if err := b.refreshAllTokens(context.Background()); err != nil {
		t.Fatalf("refreshAllTokens: %v", err)
	}

	b.mu.Lock()
	target := b.tokenTargets["/proj"]
	b.mu.Unlock()

	data, err := os.ReadFile(target.tokenFilePath)
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	if string(data) != "new-token" {
		t.Errorf("token file = %q, want %q", string(data), "new-token")
	}
}

func TestSpecBuilder_metadataServerIsolationByProject(t *testing.T) {
	cfg := newTestConfig(t)
	gcpCfg := GCPConfig{
		ServiceAccount: "sa@proj.iam.gserviceaccount.com",
		Account:        "user@example.com",
		Active:         "p",
		Projects:       []string{"p"},
	}
	b, bound := newStubbedBuilder(t, cfg, func(string) GCPConfig { return gcpCfg })

	if _, err := b.ContainerSpec(context.Background(), "/p1"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ContainerSpec(context.Background(), "/p2"); err != nil {
		t.Fatal(err)
	}

	b.mu.Lock()
	count := len(b.tokenTargets)
	b.mu.Unlock()

	if count != 2 {
		t.Errorf("expected 2 metadata servers for different projects, got %d", count)
	}
	// Isolation is per-project state and a per-project socket: two projects
	// sharing one socket would let either reach the other's metadata server.
	got := bound()
	want := []string{metaSockHostPath(cfg, "/p1"), metaSockHostPath(cfg, "/p2")}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("bound metadata sockets = %v, want %v", got, want)
	}
}

// TestSpecBuilder_metadataListenFailure_leavesProjectUnregistered pins the
// degraded path: a metadata server that cannot bind is logged and the container
// spec is still returned, but the project stays unregistered, so Materialize
// writes nothing for it rather than handing out a token file no metadata server
// backs.
func TestSpecBuilder_metadataListenFailure_leavesProjectUnregistered(t *testing.T) {
	cfg := newTestConfig(t)
	prev := metadataListen
	t.Cleanup(func() { metadataListen = prev })
	metadataListen = func(string) (net.Listener, error) {
		return nil, errors.New("listen refused")
	}

	b := NewSpecBuilder(context.Background(), cfg, func(string) GCPConfig {
		return GCPConfig{Account: "user@example.com", Active: "proj-x"}
	})
	b.gcpToken = func(_ context.Context, _, _ string) (string, error) {
		t.Error("token fetched for a project with no metadata server")
		return "", nil
	}

	spec, err := b.ContainerSpec(context.Background(), "/myproject")
	if err != nil {
		t.Fatalf("ContainerSpec: %v", err)
	}
	if len(spec.BridgeSpecs) != 1 {
		t.Errorf("expected the spec to be returned anyway, got bridges=%v", spec.BridgeSpecs)
	}

	b.mu.Lock()
	count := len(b.tokenTargets)
	b.mu.Unlock()
	if count != 0 {
		t.Errorf("registered %d token targets despite the listener failing, want 0", count)
	}
	if err := b.Materialize(context.Background(), "/myproject"); err != nil {
		t.Errorf("Materialize on an unregistered project = %v, want nil", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.RunBase, container.ProjectRunHash("/myproject"), "gcloud-token")); !os.IsNotExist(err) {
		t.Errorf("token file stat err = %v, want not-exist", err)
	}
}

// TestSpecBuilder_metadataServer_servesOverUnixSocket is the one assertion here
// that only a real socket can make: ContainerSpec must leave a serving Unix
// socket at the project's own run-dir path, since that is the file bridged into
// the container.
func TestSpecBuilder_metadataServer_servesOverUnixSocket(t *testing.T) {
	testenv.RequireUnixSocket(t)

	// sun_path caps a Unix socket at ~108 bytes and t.TempDir() spends much of
	// that on the test's name, so this one test builds a short run base instead:
	// a bind refused for path length would otherwise look like a broken server.
	runBase, err := os.MkdirTemp("", "gcpmeta")
	if err != nil {
		t.Fatalf("run base: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runBase) })
	cfg := Config{RunBase: runBase, ContainerRunDir: "/run/credproxy"}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	b := NewSpecBuilder(ctx, cfg, func(string) GCPConfig {
		return GCPConfig{Account: "user@example.com", Active: "proj-x"}
	})

	if _, err := b.ContainerSpec(context.Background(), "/myproject"); err != nil {
		t.Fatalf("ContainerSpec: %v", err)
	}

	sock := metaSockHostPath(cfg, "/myproject")
	info, err := os.Stat(sock)
	if err != nil {
		t.Fatalf("metadata socket not created: %v", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("%s mode = %v, want a socket", sock, info.Mode())
	}

	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", sock)
		},
	}}
	req, err := http.NewRequest(http.MethodGet, "http://metadata"+metadataProjectPath, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Metadata-Flavor", metadataFlavor)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET over metadata socket: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "proj-x" {
		t.Errorf("project-id over socket = %q, want %q", body, "proj-x")
	}
}
