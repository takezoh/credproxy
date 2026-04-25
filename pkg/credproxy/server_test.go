package credproxy_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/takezoh/credproxy/pkg/credproxy"
)

// fakeProvider returns a fixed Injection for every Get/Refresh call.
type fakeProvider struct {
	inj *credproxy.Injection
	err error
}

func (p *fakeProvider) Get(_ context.Context, _ credproxy.Request) (*credproxy.Injection, error) {
	return p.inj, p.err
}

func (p *fakeProvider) Refresh(_ context.Context, _ credproxy.Request) (*credproxy.Injection, error) {
	return p.inj, p.err
}

func startTestServer(t *testing.T, cfg credproxy.ServerConfig) string {
	t.Helper()
	// Tests that don't set AuthTokens are explicitly unauthenticated (loopback only).
	if len(cfg.AuthTokens) == 0 {
		cfg.AllowUnauthenticated = true
	}
	srv, err := credproxy.New(cfg)
	if err != nil {
		t.Fatalf("credproxy.New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Run(ctx) }()

	addr := srv.Addr()
	for i := 0; i < 50; i++ {
		conn, err := net.Dial("tcp", addr)
		if err == nil {
			_ = conn.Close()
			return addr
		}
	}
	t.Fatal("server did not start in time")
	return ""
}

func TestServer_healthz(t *testing.T) {
	addr := startTestServer(t, credproxy.ServerConfig{
		ListenTCP: "127.0.0.1:0",
		Routes:    []credproxy.Route{},
	})
	resp, err := http.Get(fmt.Sprintf("http://%s/healthz", addr))
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestServer_bearerAuth_rejects(t *testing.T) {
	addr := startTestServer(t, credproxy.ServerConfig{
		ListenTCP:  "127.0.0.1:0",
		AuthTokens: []string{"valid-token"},
		Routes: []credproxy.Route{{
			Path:     "/api",
			Upstream: "http://localhost:1",
			Provider: &fakeProvider{inj: &credproxy.Injection{}},
		}},
	})
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/api/test", addr), nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestServer_bearerAuth_accepts(t *testing.T) {
	addr := startTestServer(t, credproxy.ServerConfig{
		ListenTCP:  "127.0.0.1:0",
		AuthTokens: []string{"secret"},
		Routes: []credproxy.Route{{
			Path: "/api",
			Provider: &fakeProvider{inj: &credproxy.Injection{
				BodyReplace: []byte(`{"ok":true}`),
			}},
		}},
	})
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/api/", addr), nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, body = %q", resp.StatusCode, body)
	}
}

func TestServer_unixSocket_permission(t *testing.T) {
	dir := t.TempDir()
	sockPath := dir + "/test.sock"

	srv, err := credproxy.New(credproxy.ServerConfig{
		ListenUnix:           sockPath,
		AllowUnauthenticated: true,
		Routes:               []credproxy.Route{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Run(ctx) }()

	info, err := os.Stat(sockPath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("socket mode = %04o, want 0600", perm)
	}
}

func TestServer_unixSocket_rejectNonSocket(t *testing.T) {
	dir := t.TempDir()
	regularFile := dir + "/notasocket"
	if err := os.WriteFile(regularFile, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := credproxy.New(credproxy.ServerConfig{
		ListenUnix:           regularFile,
		AllowUnauthenticated: true,
		Routes:               []credproxy.Route{},
	})
	if err == nil {
		t.Error("expected error when Unix socket path points to a regular file")
	}
}

func TestServer_noAuth_open(t *testing.T) {
	addr := startTestServer(t, credproxy.ServerConfig{
		ListenTCP:            "127.0.0.1:0",
		AllowUnauthenticated: true,
		Routes: []credproxy.Route{{
			Path: "/open",
			Provider: &fakeProvider{inj: &credproxy.Injection{
				BodyReplace: []byte(`{"ok":true}`),
			}},
		}},
	})
	resp, err := http.Get(fmt.Sprintf("http://%s/open/", addr))
	if err != nil {
		t.Fatalf("GET /open/: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, body = %q", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "ok") {
		t.Errorf("body = %q, want JSON with ok", body)
	}
}
