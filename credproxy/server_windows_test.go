//go:build windows

package credproxy_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/takezoh/credproxy/credproxy"
	"github.com/takezoh/credproxy/internal/nativeipc"
	"golang.org/x/sys/windows"
)

const (
	publicChildEnv = "CREDPROXY_PUBLIC_PIPE_CHILD"
	publicPathEnv  = "CREDPROXY_PUBLIC_PIPE_ENDPOINT"
	publicReadyEnv = "CREDPROXY_PUBLIC_PIPE_READY"
)

func TestServer_namedPipeKeepsSIDAndBearerAuthorization(t *testing.T) {
	if os.Getenv(publicChildEnv) == "1" {
		runPublicPipeChild(t)
		return
	}
	runPublicPipeClient(t)
}

func runPublicPipeChild(t *testing.T) {
	t.Helper()
	sid, err := nativeipc.CurrentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	srv, err := credproxy.New(credproxy.ServerConfig{
		ListenPipe: os.Getenv(publicPathEnv), RequirePeerSID: sid,
		AuthTokens: []credproxy.TokenAuth{{Token: "native-test-token", ID: "native-test"}},
		Routes: []credproxy.Route{{Path: "/api", Provider: &fakeProvider{
			inj: &credproxy.Injection{BodyReplace: []byte(`{"ok":true}`)},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv(publicReadyEnv), []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := srv.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func runPublicPipeClient(t *testing.T) {
	t.Helper()
	sid, err := nativeipc.CurrentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := nativeipc.PipePath("broker", sid)
	if err != nil {
		t.Fatal(err)
	}
	endpoint = fmt.Sprintf("%s-test-%d", endpoint, os.Getpid())
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	expected := protectedPublicTestImage(t, source)
	ready := filepath.Join(t.TempDir(), "ready")
	startPublicPipeChild(t, expected, endpoint, ready)
	waitPublicPipeReady(t, ready)
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return nativeipc.DialPipe(ctx, endpoint, expected)
		},
	}}
	if got := publicPipeStatus(t, client, endpoint, "native-test-token"); got != http.StatusOK {
		t.Fatalf("authenticated request status = %d, want 200", got)
	}
	if got := publicPipeStatus(t, client, endpoint, ""); got != http.StatusUnauthorized {
		t.Fatalf("missing bearer status = %d, want 401", got)
	}
}

func startPublicPipeChild(t *testing.T, executable, endpoint, ready string) {
	t.Helper()
	cmd := exec.Command(executable, "-test.run=^TestServer_namedPipeKeepsSIDAndBearerAuthorization$")
	cmd.Env = append(os.Environ(), publicChildEnv+"=1", publicPathEnv+"="+endpoint, publicReadyEnv+"="+ready)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
}

func protectedPublicTestImage(t *testing.T, source string) string {
	t.Helper()
	root := t.TempDir()
	parent, child := filepath.Join(root, "parent"), filepath.Join(root, "parent", "child")
	if err := os.MkdirAll(child, 0700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(child, "credproxyd-test.exe")
	if err := copyPublicTestImage(source, executable); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{parent, child, executable} {
		protectPublicTestPath(t, path)
	}
	return executable
}

func copyPublicTestImage(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(destination)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func protectPublicTestPath(t *testing.T, path string) {
	t.Helper()
	sid, err := nativeipc.CurrentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + sid + ")(A;;FA;;;SY)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	flags := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION)
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, flags, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
}

func waitPublicPipeReady(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("public pipe child did not become ready")
}

func publicPipeStatus(t *testing.T, client *http.Client, endpoint, token string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://credproxyd/api/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("named-pipe request %s: %v", endpoint, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}
