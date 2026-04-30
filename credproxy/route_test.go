package credproxy_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/takezoh/credproxy/credproxy"
)

// refreshTrackingProvider counts Refresh calls and returns distinct injections per call.
type refreshTrackingProvider struct {
	getCalls     int
	refreshCalls int
	getInj       *credproxy.Injection
	refreshInj   *credproxy.Injection
}

func (p *refreshTrackingProvider) Get(_ context.Context, _ credproxy.Request) (*credproxy.Injection, error) {
	p.getCalls++
	return p.getInj, nil
}

func (p *refreshTrackingProvider) Refresh(_ context.Context, _ credproxy.Request) (*credproxy.Injection, error) {
	p.refreshCalls++
	return p.refreshInj, nil
}

func startRouteTestServer(t *testing.T, route credproxy.Route) string {
	t.Helper()
	return startTestServer(t, credproxy.ServerConfig{
		ListenTCP: "127.0.0.1:0",
		Routes:    []credproxy.Route{route},
	})
}

func TestRouteHandler_injectsHeaders(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
	}))
	defer upstream.Close()

	addr := startRouteTestServer(t, credproxy.Route{
		Path:     "/api",
		Upstream: upstream.URL,
		Provider: &fakeProvider{inj: &credproxy.Injection{
			Headers: map[string]string{"Authorization": "Bearer injected"},
		}},
	})

	resp, err := http.Post("http://"+addr+"/api/v1/test", "application/json", nil)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	_ = resp.Body.Close()
	if gotAuth != "Bearer injected" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer injected")
	}
}

func TestRouteHandler_stripInboundAuth(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
	}))
	defer upstream.Close()

	addr := startRouteTestServer(t, credproxy.Route{
		Path:             "/api",
		Upstream:         upstream.URL,
		StripInboundAuth: true,
		Provider: &fakeProvider{inj: &credproxy.Injection{
			Headers: map[string]string{"Authorization": "Bearer fresh"},
		}},
	})

	req, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/api/test", nil)
	req.Header.Set("Authorization", "Bearer client-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	_ = resp.Body.Close()
	if gotAuth != "Bearer fresh" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer fresh")
	}
}

func TestRouteHandler_refreshOnStatus(t *testing.T) {
	callCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	provider := &refreshTrackingProvider{
		getInj:     &credproxy.Injection{Headers: map[string]string{"X-Attempt": "1"}},
		refreshInj: &credproxy.Injection{Headers: map[string]string{"X-Attempt": "2"}},
	}

	addr := startRouteTestServer(t, credproxy.Route{
		Path:            "/api",
		Upstream:        upstream.URL,
		RefreshOnStatus: []int{401},
		Provider:        provider,
	})

	resp, err := http.Get("http://" + addr + "/api/test")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("final status = %d, want 200", resp.StatusCode)
	}
	if callCount != 2 {
		t.Errorf("upstream calls = %d, want 2", callCount)
	}
	if provider.refreshCalls != 1 {
		t.Errorf("Refresh calls = %d, want 1", provider.refreshCalls)
	}
}

func TestRouteHandler_bodyReplace(t *testing.T) {
	addr := startRouteTestServer(t, credproxy.Route{
		Path: "/creds",
		Provider: &fakeProvider{inj: &credproxy.Injection{
			BodyReplace: []byte(`{"AccessKeyId":"AK","SecretAccessKey":"SK","Token":"T"}`),
		}},
	})

	resp, err := http.Get("http://" + addr + "/creds/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestRouteHandler_pathStripping(t *testing.T) {
	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
	}))
	defer upstream.Close()

	addr := startRouteTestServer(t, credproxy.Route{
		Path:     "/anthropic",
		Upstream: upstream.URL,
		Provider: &fakeProvider{inj: &credproxy.Injection{}},
	})

	resp, err := http.Post("http://"+addr+"/anthropic/v1/messages", "application/json", nil)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	_ = resp.Body.Close()
	if gotPath != "/v1/messages" {
		t.Errorf("upstream path = %q, want /v1/messages", gotPath)
	}
}

func TestRouteHandler_SSEStreamingTransparent(t *testing.T) {
	// Upstream sends a Server-Sent Events stream in two chunks with a Flush between them.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("upstream ResponseWriter does not implement Flusher")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		for i := 0; i < 3; i++ {
			_, _ = fmt.Fprintf(w, "data: event%d\n\n", i)
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	addr := startRouteTestServer(t, credproxy.Route{
		Path:     "/sse",
		Upstream: upstream.URL,
		Provider: &fakeProvider{inj: &credproxy.Injection{
			Headers: map[string]string{"X-Auth": "injected"},
		}},
		// No RefreshOnStatus — streaming is always transparent without buffering.
	})

	resp, err := http.Get("http://" + addr + "/sse/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// Read all three SSE events.
	var events []string
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "data:") {
			events = append(events, line)
		}
	}
	if len(events) != 3 {
		t.Errorf("received %d SSE events, want 3: %v", len(events), events)
	}
}

func TestRouteHandler_refreshBodyReplace(t *testing.T) {
	// Even when refresh is triggered, if the refreshed Injection has BodyReplace,
	// it must be returned to the client (not forwarded to upstream).
	callCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer upstream.Close()

	provider := &refreshTrackingProvider{
		getInj: &credproxy.Injection{Headers: map[string]string{"X-A": "1"}},
		refreshInj: &credproxy.Injection{
			BodyReplace: []byte(`{"refreshed":true}`),
		},
	}

	addr := startRouteTestServer(t, credproxy.Route{
		Path:            "/api",
		Upstream:        upstream.URL,
		RefreshOnStatus: []int{401},
		Provider:        provider,
	})

	resp, err := http.Get("http://" + addr + "/api/test")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "refreshed") {
		t.Errorf("body = %q, want refreshed body_replace", body)
	}
	if provider.refreshCalls != 1 {
		t.Errorf("Refresh calls = %d, want 1", provider.refreshCalls)
	}
	// Upstream should have been called exactly once (the 401), then BodyReplace short-circuits.
	if callCount != 1 {
		t.Errorf("upstream calls = %d, want 1", callCount)
	}
}

// refreshErrProvider returns a valid injection from Get but errors on Refresh.
type refreshErrProvider struct {
	getInj *credproxy.Injection
}

func (p *refreshErrProvider) Get(_ context.Context, _ credproxy.Request) (*credproxy.Injection, error) {
	return p.getInj, nil
}

func (p *refreshErrProvider) Refresh(_ context.Context, _ credproxy.Request) (*credproxy.Injection, error) {
	return nil, fmt.Errorf("refresh deliberately failed")
}

// errReader is an io.Reader that always returns an error.
type errReader struct{ err error }

func (e *errReader) Read(_ []byte) (int, error) { return 0, e.err }

func TestRouteHandler_upstreamConnError_502(t *testing.T) {
	// Start and immediately close a server so its port gives ECONNREFUSED.
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	addr := startRouteTestServer(t, credproxy.Route{
		Path:     "/api",
		Upstream: deadURL,
		Provider: &fakeProvider{inj: &credproxy.Injection{}},
	})
	resp, err := http.Get("http://" + addr + "/api/test")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", resp.StatusCode)
	}
}

func TestRouteHandler_doubleRefreshBlocked_502(t *testing.T) {
	// Upstream always returns 401. After one refresh+retry the second 401
	// must NOT trigger another Refresh — instead the proxy returns 502.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer upstream.Close()

	provider := &refreshTrackingProvider{
		getInj:     &credproxy.Injection{},
		refreshInj: &credproxy.Injection{}, // no BodyReplace → proxy forwards again
	}

	addr := startRouteTestServer(t, credproxy.Route{
		Path:            "/api",
		Upstream:        upstream.URL,
		RefreshOnStatus: []int{401},
		Provider:        provider,
	})

	resp, err := http.Get("http://" + addr + "/api/test")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", resp.StatusCode)
	}
	if provider.refreshCalls != 1 {
		t.Errorf("Refresh calls = %d, want exactly 1", provider.refreshCalls)
	}
}

func TestRouteHandler_refreshError_502(t *testing.T) {
	// Upstream returns 401; Provider.Refresh returns an error → 502.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer upstream.Close()

	addr := startRouteTestServer(t, credproxy.Route{
		Path:            "/api",
		Upstream:        upstream.URL,
		RefreshOnStatus: []int{401},
		Provider:        &refreshErrProvider{getInj: &credproxy.Injection{}},
	})

	resp, err := http.Get("http://" + addr + "/api/test")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", resp.StatusCode)
	}
}

func TestRouteHandler_bodyReadError_400(t *testing.T) {
	// When RefreshOnStatus is set, ServeHTTP buffers the request body.
	// If reading the body errors, it must return 400 Bad Request.
	srv, err := credproxy.New(credproxy.ServerConfig{
		ListenTCP:            "127.0.0.1:0",
		AllowUnauthenticated: true,
		Routes: []credproxy.Route{{
			Path:            "/api",
			Upstream:        "http://127.0.0.1:1",
			RefreshOnStatus: []int{401},
			Provider:        &fakeProvider{inj: &credproxy.Injection{}},
		}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/test", &errReader{err: io.ErrUnexpectedEOF})
	srv.Handler().ServeHTTP(rr, r)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestRouteHandler_queryInjection(t *testing.T) {
	// Injection.Query values must be appended to the upstream request URL.
	var gotKey string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.URL.Query().Get("api_key")
	}))
	defer upstream.Close()

	addr := startRouteTestServer(t, credproxy.Route{
		Path:     "/api",
		Upstream: upstream.URL,
		Provider: &fakeProvider{inj: &credproxy.Injection{
			Query: map[string]string{"api_key": "secret123"},
		}},
	})

	resp, err := http.Get("http://" + addr + "/api/test")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if gotKey != "secret123" {
		t.Errorf("api_key query param = %q, want secret123", gotKey)
	}
}

// TestServer_noTokens_openByDefault verifies that a server with no registered tokens
// does not require authentication (auth is enforced only once AddAuthToken is called).
func TestServer_noTokens_openByDefault(t *testing.T) {
	addr := startTestServer(t, credproxy.ServerConfig{
		ListenTCP: "127.0.0.1:0",
		Routes: []credproxy.Route{{
			Path:     "/api",
			Provider: &fakeProvider{inj: &credproxy.Injection{BodyReplace: []byte(`{"ok":true}`)}},
		}},
	})
	resp, err := http.Get(fmt.Sprintf("http://%s/api/", addr))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 (no tokens registered → auth skipped)", resp.StatusCode)
	}
}

func TestServer_Addr_ephemeralPort(t *testing.T) {
	srv, err := credproxy.New(credproxy.ServerConfig{
		ListenTCP:            "127.0.0.1:0",
		AllowUnauthenticated: true,
		Routes:               []credproxy.Route{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	addr := srv.Addr()
	if addr == "" || strings.HasSuffix(addr, ":0") {
		t.Errorf("Addr() = %q, want resolved port", addr)
	}
}
