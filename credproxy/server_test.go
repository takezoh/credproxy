package credproxy_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/takezoh/credproxy/credproxy"
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
		AuthTokens: []credproxy.TokenAuth{{Token: "valid-token", ID: "test"}},
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
		AuthTokens: []credproxy.TokenAuth{{Token: "secret", ID: "test"}},
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

func TestServer_Handler(t *testing.T) {
	srv, err := credproxy.New(credproxy.ServerConfig{
		ListenTCP:            "127.0.0.1:0",
		AllowUnauthenticated: true,
		Routes: []credproxy.Route{{
			Path: "/api",
			Provider: &fakeProvider{inj: &credproxy.Injection{
				BodyReplace: []byte(`{"ok":true}`),
			}},
		}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	h := srv.Handler()
	if h == nil {
		t.Fatal("Handler() returned nil")
	}

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/", nil)
	h.ServeHTTP(rr, r)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "ok") {
		t.Errorf("body = %q, want JSON with ok", rr.Body.String())
	}
}

// recordingProvider captures the Request passed to Get so tests can inspect Metadata.
type recordingProvider struct {
	lastReq credproxy.Request
	inj     *credproxy.Injection
}

func (p *recordingProvider) Get(_ context.Context, req credproxy.Request) (*credproxy.Injection, error) {
	p.lastReq = req
	return p.inj, nil
}

func (p *recordingProvider) Refresh(_ context.Context, req credproxy.Request) (*credproxy.Injection, error) {
	p.lastReq = req
	return p.inj, nil
}

// TestServer_tokenID_propagated verifies that the matched token's ID reaches the provider
// via Request.Metadata["token_id"].
func TestServer_tokenID_propagated(t *testing.T) {
	provider := &recordingProvider{inj: &credproxy.Injection{BodyReplace: []byte(`{}`)}}
	addr := startTestServer(t, credproxy.ServerConfig{
		ListenTCP:  "127.0.0.1:0",
		AuthTokens: []credproxy.TokenAuth{{Token: "tok-abc", ID: "proj-A"}},
		Routes: []credproxy.Route{{
			Path:     "/creds",
			Provider: provider,
		}},
	})

	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/creds/", addr), nil)
	req.Header.Set("Authorization", "Bearer tok-abc")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if got := provider.lastReq.Metadata["token_id"]; got != "proj-A" {
		t.Errorf("Metadata[token_id] = %q, want %q", got, "proj-A")
	}
}

// TestServer_AddAuthToken_dynamic verifies that a token registered after New() is accepted.
func TestServer_AddAuthToken_dynamic(t *testing.T) {
	provider := &fakeProvider{inj: &credproxy.Injection{BodyReplace: []byte(`{"ok":true}`)}}
	srv, err := credproxy.New(credproxy.ServerConfig{
		ListenTCP: "127.0.0.1:0",
		// Empty AuthTokens: auth skipped until a token is added.
		Routes: []credproxy.Route{{Path: "/api", Provider: provider}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Run(ctx) }()
	addr := srv.Addr()
	for i := 0; i < 50; i++ {
		conn, err2 := net.Dial("tcp", addr)
		if err2 == nil {
			_ = conn.Close()
			break
		}
	}

	srv.AddAuthToken("dynamic-secret", "dynamic-proj")

	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/api/", addr), nil)
	req.Header.Set("Authorization", "Bearer dynamic-secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

// TestServer_RegisterPeriodic verifies that a periodic job fires repeatedly and serially.
func TestServer_RegisterPeriodic(t *testing.T) {
	srv, err := credproxy.New(credproxy.ServerConfig{
		ListenTCP:            "127.0.0.1:0",
		AllowUnauthenticated: true,
		Routes:               nil,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var calls atomic.Int32
	srv.RegisterPeriodic(credproxy.PeriodicJob{
		Name:  "test-job",
		Every: 20 * time.Millisecond,
		Run: func(ctx context.Context) error {
			calls.Add(1)
			return nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Run(ctx) }()

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		if calls.Load() >= 3 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := calls.Load(); got < 3 {
		t.Errorf("expected ≥3 job invocations, got %d", got)
	}
}

// TestServer_AddAuthToken_idempotent verifies replacing a token with the same ID works.
func TestServer_AddAuthToken_idempotent(t *testing.T) {
	provider := &recordingProvider{inj: &credproxy.Injection{BodyReplace: []byte(`{}`)}}
	srv, err := credproxy.New(credproxy.ServerConfig{
		ListenTCP:  "127.0.0.1:0",
		AuthTokens: []credproxy.TokenAuth{{Token: "old-token", ID: "proj-X"}},
		Routes:     []credproxy.Route{{Path: "/api", Provider: provider}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Run(ctx) }()
	addr := srv.Addr()
	for i := 0; i < 50; i++ {
		conn, err2 := net.Dial("tcp", addr)
		if err2 == nil {
			_ = conn.Close()
			break
		}
	}

	srv.AddAuthToken("new-token", "proj-X")

	// Old token must be rejected.
	reqOld, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/api/", addr), nil)
	reqOld.Header.Set("Authorization", "Bearer old-token")
	respOld, err := http.DefaultClient.Do(reqOld)
	if err != nil {
		t.Fatalf("old-token request: %v", err)
	}
	defer func() { _ = respOld.Body.Close() }()
	if respOld.StatusCode != http.StatusUnauthorized {
		t.Errorf("old token: status = %d, want 401", respOld.StatusCode)
	}

	// New token must be accepted and carry the same ID.
	reqNew, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/api/", addr), nil)
	reqNew.Header.Set("Authorization", "Bearer new-token")
	respNew, err := http.DefaultClient.Do(reqNew)
	if err != nil {
		t.Fatalf("new-token request: %v", err)
	}
	defer func() { _ = respNew.Body.Close() }()
	if respNew.StatusCode != http.StatusOK {
		t.Errorf("new token: status = %d, want 200", respNew.StatusCode)
	}
	if got := provider.lastReq.Metadata["token_id"]; got != "proj-X" {
		t.Errorf("Metadata[token_id] = %q, want %q", got, "proj-X")
	}
}
