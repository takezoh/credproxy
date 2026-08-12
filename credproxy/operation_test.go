package credproxy

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const operationCanary = "canary-credential-must-not-cross-response"

type operationProvider struct {
	body string
	err  error
}

func (p operationProvider) Get(context.Context, Request) (*Injection, error) {
	return &Injection{BodyReplace: []byte(p.body)}, p.err
}

func (p operationProvider) Refresh(context.Context, Request) (*Injection, error) {
	return nil, errors.New("unused")
}

type recordingRunner struct {
	executable string
	args       []string
	env        []string
	err        error
	started    int
	wait       bool
	startedCh  chan struct{}
}

func (r *recordingRunner) Run(ctx context.Context, executable string, args, env []string) error {
	r.started++
	if r.startedCh != nil {
		close(r.startedCh)
	}
	r.executable = executable
	r.args = append([]string(nil), args...)
	r.env = append([]string(nil), env...)
	if r.wait {
		<-ctx.Done()
		return ctx.Err()
	}
	return r.err
}

func operationFixture(t *testing.T, provider Provider, runner OperationRunner) (*Server, string) {
	return operationFixtureLogger(t, provider, runner, slog.Default())
}

func operationFixtureLogger(t *testing.T, provider Provider, runner OperationRunner, logger *slog.Logger) (*Server, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ctx")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	srv, err := New(ServerConfig{
		ListenUnix: filepath.Join(t.TempDir(), "broker.sock"), AllowUnauthenticated: true,
		Logger:         logger,
		DaemonRevision: "daemon/test-1",
		Operations: []Operation{{
			Name: "ctx-sync", BindingRevision: "ctx-sync/2",
			ExecutablePaths: []string{path}, Subcommand: "sync",
			Arguments: []OperationArgument{
				{Flag: "-timeout", ValueType: "duration", Min: time.Second, Max: time.Minute},
				{Flag: "-min-interval", ValueType: "duration", Min: 0, Max: 24 * time.Hour},
			},
			Environment: map[string]string{"CTX_DATABASE_URL": "CTX_DATABASE_URL"},
			FixedEnv:    map[string]string{"PATH": "/usr/bin:/bin"},
			PassEnv:     []string{"LANG"}, Provider: provider, Runner: runner,
		}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return srv, path
}

func operationRequestRecorder(t *testing.T, srv *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/operations/ctx-sync", strings.NewReader(body))
	srv.Handler().ServeHTTP(rr, req)
	return rr
}

func validOperationBody(args string) string {
	return `{"protocol":"credroute/v1","binding_revision":"ctx-sync/2","daemon_revision":"daemon/test-1","arguments":` + args + `}`
}

func TestOperation_executesFixedChildWithoutReturningCredential(t *testing.T) {
	runner := &recordingRunner{}
	var logs strings.Builder
	provider := operationProvider{body: `{"env":{"CTX_DATABASE_URL":"` + operationCanary + `"}}`}
	srv, executable := operationFixtureLogger(t, provider, runner, slog.New(slog.NewTextHandler(&logs, nil)))

	rr := operationRequestRecorder(t, srv, validOperationBody(`["-timeout","10s","-min-interval","1h"]`))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if runner.executable != executable || strings.Join(runner.args, " ") != "sync -timeout 10s -min-interval 1h" {
		t.Fatalf("fixed command mismatch: %q %v", runner.executable, runner.args)
	}
	if !containsEnv(runner.env, "CTX_DATABASE_URL="+operationCanary) {
		t.Fatalf("credential was not delivered to fixed child: %v", envNames(runner.env))
	}
	if strings.Contains(rr.Body.String(), operationCanary) || strings.Contains(logs.String(), operationCanary) {
		t.Fatal("credential crossed response or log boundary")
	}
	var out operationResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil || out.Outcome != "success" {
		t.Fatalf("response=%s err=%v", rr.Body.String(), err)
	}
}

func TestOperation_sameUIDSocketDoesNotRequireBearerCredential(t *testing.T) {
	runner := &recordingRunner{}
	srv, _ := operationFixture(t, operationProvider{body: `{"env":{"CTX_DATABASE_URL":"x"}}`}, runner)
	// Route bearer tokens may coexist for generic routes. Closed operations use
	// only the exclusive 0600 Unix socket because no bearer may be delegated to
	// an arbitrary same-UID caller.
	srv.AddAuthToken("route-only-secret", "generic-client")
	rr := operationRequestRecorder(t, srv, validOperationBody(`[]`))
	if rr.Code != http.StatusOK || runner.started != 1 {
		t.Fatalf("status=%d child starts=%d body=%q", rr.Code, runner.started, rr.Body.String())
	}
}

func TestOperation_isNotEnvServingOrDiscoverable(t *testing.T) {
	srv, _ := operationFixture(t, operationProvider{body: `{"env":{"CTX_DATABASE_URL":"` + operationCanary + `"}}`}, &recordingRunner{})
	for _, path := range []string{"/_routes", "/ctx-sync/", "/v1/operations/ctx-sync"} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		srv.Handler().ServeHTTP(rr, req)
		if strings.Contains(rr.Body.String(), operationCanary) {
			t.Fatalf("GET %s returned credential", path)
		}
		if path == "/_routes" && strings.Contains(rr.Body.String(), "ctx-sync") {
			t.Fatal("closed operation was advertised as env-serving route")
		}
	}
}

func TestOperation_rejectsEnvRouteWithSameName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ctx")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := New(ServerConfig{
		ListenUnix: filepath.Join(t.TempDir(), "broker.sock"), AllowUnauthenticated: true,
		DaemonRevision: "daemon/test-1",
		Routes:         []Route{{Path: "/ctx-sync", Provider: operationProvider{body: `{"env":{"CTX_DATABASE_URL":"x"}}`}}},
		Operations: []Operation{{
			Name: "ctx-sync", BindingRevision: "ctx-sync/2", ExecutablePaths: []string{path},
			Subcommand: "sync", Environment: map[string]string{"CTX_DATABASE_URL": "CTX_DATABASE_URL"},
			Provider: operationProvider{body: `{"env":{"CTX_DATABASE_URL":"x"}}`}, Runner: &recordingRunner{},
		}},
	})
	if err == nil {
		t.Fatal("same-name env route would make the closed operation secret retrievable")
	}
}

func TestOperation_requiresExclusiveUnixListener(t *testing.T) {
	provider := operationProvider{body: `{"env":{"CTX_DATABASE_URL":"x"}}`}
	path := filepath.Join(t.TempDir(), "ctx")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	op := Operation{
		Name: "ctx-sync", BindingRevision: "ctx-sync/2", ExecutablePaths: []string{path},
		Subcommand: "sync", Environment: map[string]string{"CTX_DATABASE_URL": "CTX_DATABASE_URL"},
		Provider: provider, Runner: &recordingRunner{},
	}
	for _, cfg := range []ServerConfig{
		{ListenTCP: "127.0.0.1:0", AllowUnauthenticated: true, DaemonRevision: "daemon/test", Operations: []Operation{op}},
		{ListenTCP: "127.0.0.1:0", ListenUnix: filepath.Join(t.TempDir(), "broker.sock"), AllowUnauthenticated: true, DaemonRevision: "daemon/test", Operations: []Operation{op}},
	} {
		if _, err := New(cfg); err == nil {
			t.Fatal("closed operation accepted a TCP-reachable server")
		}
	}
}

func TestOperation_rejectsMaliciousRequestsBeforeProvider(t *testing.T) {
	tests := map[string]string{
		"unknown flag":     validOperationBody(`["--config","/tmp/evil"]`),
		"duplicate flag":   validOperationBody(`["-timeout","1s","-timeout","2s"]`),
		"positional":       validOperationBody(`["brief","1s"]`),
		"bad duration":     validOperationBody(`["-timeout","forever"]`),
		"path override":    `{"protocol":"credroute/v1","binding_revision":"ctx-sync/2","daemon_revision":"daemon/test-1","arguments":[],"executable":"/tmp/evil"}`,
		"env override":     `{"protocol":"credroute/v1","binding_revision":"ctx-sync/2","daemon_revision":"daemon/test-1","arguments":[],"env":{"LD_PRELOAD":"/tmp/evil"}}`,
		"binding mismatch": `{"protocol":"credroute/v1","binding_revision":"ctx-sync/1","daemon_revision":"daemon/test-1","arguments":[]}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			runner := &recordingRunner{}
			srv, _ := operationFixture(t, operationProvider{body: `{"env":{"CTX_DATABASE_URL":"x"}}`}, runner)
			rr := operationRequestRecorder(t, srv, body)
			if rr.Code < 400 || runner.started != 0 {
				t.Fatalf("status=%d child starts=%d", rr.Code, runner.started)
			}
		})
	}
}

func TestOperation_rejectsMalformedProviderEnvBeforeExec(t *testing.T) {
	bodies := []string{
		`{}`, `{"env":{}}`, `{"env":{"OTHER":"x"}}`,
		`{"env":{"CTX_DATABASE_URL":"x","PATH":"/tmp"}}`,
		`{"env":{"CTX_DATABASE_URL":"x","CTX_DATABASE_URL":"y"}}`,
		`{"env":{"CTX_DATABASE_URL":"x"},"extra":true}`,
	}
	for _, body := range bodies {
		runner := &recordingRunner{}
		srv, _ := operationFixture(t, operationProvider{body: body}, runner)
		rr := operationRequestRecorder(t, srv, validOperationBody(`[]`))
		if rr.Code != http.StatusBadGateway || runner.started != 0 || strings.Contains(rr.Body.String(), body) {
			t.Errorf("body=%q status=%d starts=%d response=%q", body, rr.Code, runner.started, rr.Body.String())
		}
	}
}

func TestOperation_childAndProviderFailuresAreTypedAndSecretSafe(t *testing.T) {
	for name, tc := range map[string]struct {
		provider Provider
		runner   *recordingRunner
		want     string
	}{
		"provider": {operationProvider{err: errors.New(operationCanary)}, &recordingRunner{}, "credential_unavailable"},
		"child":    {operationProvider{body: `{"env":{"CTX_DATABASE_URL":"` + operationCanary + `"}}`}, &recordingRunner{err: errors.New(operationCanary)}, "operation_failed"},
	} {
		t.Run(name, func(t *testing.T) {
			var logs strings.Builder
			srv, _ := operationFixtureLogger(t, tc.provider, tc.runner, slog.New(slog.NewTextHandler(&logs, nil)))
			rr := operationRequestRecorder(t, srv, validOperationBody(`[]`))
			if !strings.Contains(rr.Body.String(), tc.want) || strings.Contains(rr.Body.String(), operationCanary) || strings.Contains(logs.String(), operationCanary) {
				t.Fatalf("response=%q logs=%q", rr.Body.String(), logs.String())
			}
		})
	}
}

func TestOperation_cancellationReachesChild(t *testing.T) {
	started := make(chan struct{})
	runner := &recordingRunner{wait: true, startedCh: started}
	srv, _ := operationFixture(t, operationProvider{body: `{"env":{"CTX_DATABASE_URL":"x"}}`}, runner)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/v1/operations/ctx-sync", strings.NewReader(validOperationBody(`[]`))).WithContext(ctx)
	rr := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { srv.Handler().ServeHTTP(rr, req); close(done) }()
	<-started
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancel did not stop child")
	}
	if !strings.Contains(rr.Body.String(), "operation_cancelled") {
		t.Fatalf("response=%q", rr.Body.String())
	}
}

func containsEnv(env []string, want string) bool {
	for _, entry := range env {
		if entry == want {
			return true
		}
	}
	return false
}

func envNames(env []string) []string {
	names := make([]string, 0, len(env))
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		names = append(names, name)
	}
	return names
}
