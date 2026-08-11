package credproxy

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAuthorizePeer(t *testing.T) {
	self := uint32(os.Getuid())
	cases := []struct {
		name    string
		pc      peerCred
		require bool
		want    bool
	}{
		{"not required passes", peerCred{}, false, true},
		{"required same uid", peerCred{UID: self, available: true}, true, true},
		{"required different uid", peerCred{UID: self + 1, available: true}, true, false},
		{"required but unavailable denies", peerCred{available: false}, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := authorizePeer(tc.pc, tc.require, self); got != tc.want {
				t.Errorf("authorizePeer(%+v, %v) = %v, want %v", tc.pc, tc.require, got, tc.want)
			}
		})
	}
}

func TestNew_requireSamePeerUID_rejectsTCP(t *testing.T) {
	if !peerCredSupported {
		t.Skip("peer credentials unsupported on this platform")
	}
	_, err := New(ServerConfig{
		ListenTCP:          "127.0.0.1:0",
		ListenUnix:         filepath.Join(t.TempDir(), "s.sock"),
		RequireSamePeerUID: true,
	})
	if err == nil {
		t.Fatal("expected New to reject RequireSamePeerUID with a TCP listener")
	}
}

// TestServer_requireSamePeerUID_capturesRealPeer exercises the ConnContext →
// SO_PEERCRED path over an actual Unix socket. Same-UID (this test process)
// must be admitted; the pure denial path is covered by TestAuthorizePeer.
func TestServer_requireSamePeerUID_capturesRealPeer(t *testing.T) {
	if !peerCredSupported {
		t.Skip("peer credentials unsupported on this platform")
	}
	sock := filepath.Join(t.TempDir(), "broker.sock")
	srv, err := New(ServerConfig{
		ListenUnix:           sock,
		AllowUnauthenticated: true,
		RequireSamePeerUID:   true,
		Routes: []Route{{
			Path:     "/api",
			Provider: &peerEchoProvider{},
		}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Run(ctx) }()

	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", sock)
		},
	}}

	// Retry briefly until the listener is serving.
	var resp *http.Response
	for i := 0; i < 50; i++ {
		resp, err = client.Get("http://broker/api/")
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 (same-uid peer must be admitted)", resp.StatusCode)
	}
}

// peerEchoProvider confirms the peer_uid metadata reached the provider.
type peerEchoProvider struct{}

func (p *peerEchoProvider) Get(_ context.Context, req Request) (*Injection, error) {
	if req.Metadata["peer_uid"] == "" {
		return &Injection{BodyReplace: []byte(`{"peer":"missing"}`)}, nil
	}
	return &Injection{BodyReplace: []byte(`{"peer":"present"}`)}, nil
}

func (p *peerEchoProvider) Refresh(ctx context.Context, req Request) (*Injection, error) {
	return p.Get(ctx, req)
}
