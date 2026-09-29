package admin

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRefresher struct {
	names []string
	all   bool
	err   error
	calls int
}

func (f *fakeRefresher) Refresh(_ context.Context, names []string, all bool) error {
	f.names, f.all, f.calls = names, all, f.calls+1
	return f.err
}

func TestRefreshDispatch(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		calls      int
		all        bool
		credential string
	}{
		{"one", `{"credential":"alpha"}`, 204, 1, false, "alpha"},
		{"all", `{"all_preloaded":true}`, 204, 1, true, ""},
		{"missing", `{}`, 400, 0, false, ""},
		{"both", `{"credential":"alpha","all_preloaded":true}`, 400, 0, false, ""},
		{"unknown", `{"credential":"alpha","secret":"x"}`, 400, 0, false, ""},
		{"trailing", `{"credential":"alpha"}{}`, 400, 0, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeRefresher{}
			req := httptest.NewRequest(http.MethodPost, "/refresh", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			Handler(f).ServeHTTP(rec, req)
			if rec.Code != tc.status || f.calls != tc.calls || f.all != tc.all {
				t.Fatalf("status=%d calls=%d all=%t", rec.Code, f.calls, f.all)
			}
			if tc.credential != "" && (len(f.names) != 1 || f.names[0] != tc.credential) {
				t.Fatalf("names=%q", f.names)
			}
			if strings.Contains(rec.Body.String(), "alpha") {
				t.Fatal("response contains credential name")
			}
		})
	}
}

func TestRefreshErrorDoesNotExposeCause(t *testing.T) {
	f := &fakeRefresher{err: errors.New("source rate limit exceeded")}
	req := httptest.NewRequest(http.MethodPost, "/refresh", strings.NewReader(`{"credential":"alpha"}`))
	rec := httptest.NewRecorder()
	Handler(f).ServeHTTP(rec, req)
	if rec.Code != 502 || strings.Contains(rec.Body.String(), "rate limit exceeded") {
		t.Fatalf("%d: %q", rec.Code, rec.Body.String())
	}
}

func TestStartRejectsOccupiedSocketPathBeforeReturning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admin.sock")
	if err := os.WriteFile(path, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Start(context.Background(), path, &fakeRefresher{}); err == nil {
		t.Fatal("admin startup succeeded with an occupied non-socket path")
	}
}

func TestStartDoesNotReplaceLiveSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admin.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if _, err := Start(context.Background(), path, &fakeRefresher{}); err == nil {
		t.Fatal("admin startup replaced a live listener")
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("original listener lost socket: %v", err)
	}
	conn.Close()
}
