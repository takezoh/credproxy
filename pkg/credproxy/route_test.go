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

	"github.com/takezoh/credproxy/pkg/credproxy"
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

func TestRouteHandler_openDefault_rejected(t *testing.T) {
	_, err := credproxy.New(credproxy.ServerConfig{
		ListenTCP: "127.0.0.1:0",
		Routes:    []credproxy.Route{},
		// No AuthTokens, no AllowUnauthenticated — must fail.
	})
	if err == nil {
		t.Error("expected error when AuthTokens empty and AllowUnauthenticated false on TCP listener")
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
