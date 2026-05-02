package gcloudcli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takezoh/credproxy/container"
)

func newTestConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		RunBase:         t.TempDir(),
		ContainerRunDir: "/run/credproxy",
	}
}

func emptyGCPConfig(string) GCPConfig { return GCPConfig{} }

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
	b := NewSpecBuilder(context.Background(), cfg, func(string) GCPConfig {
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
}

func TestSpecBuilder_userAccountProxy_tokenPathInConfig(t *testing.T) {
	cfg := newTestConfig(t)
	b := NewSpecBuilder(context.Background(), cfg, func(string) GCPConfig {
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
	b := NewSpecBuilder(context.Background(), cfg, func(string) GCPConfig {
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
	b := NewSpecBuilder(context.Background(), cfg, func(string) GCPConfig {
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
	b := NewSpecBuilder(context.Background(), cfg, func(string) GCPConfig {
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
	b := NewSpecBuilder(context.Background(), cfg, func(string) GCPConfig { return gcpCfg })

	if _, err := b.ContainerSpec(context.Background(), "/p1"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ContainerSpec(context.Background(), "/p1"); err != nil {
		t.Fatal(err)
	}

	b.mu.Lock()
	count := len(b.metaServers)
	b.mu.Unlock()

	if count != 1 {
		t.Errorf("expected 1 metadata server for same project, got %d", count)
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
	b := NewSpecBuilder(context.Background(), cfg, func(string) GCPConfig { return gcpCfg })

	if _, err := b.ContainerSpec(context.Background(), "/p1"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ContainerSpec(context.Background(), "/p2"); err != nil {
		t.Fatal(err)
	}

	b.mu.Lock()
	count := len(b.metaServers)
	b.mu.Unlock()

	if count != 2 {
		t.Errorf("expected 2 metadata servers for different projects, got %d", count)
	}
}

