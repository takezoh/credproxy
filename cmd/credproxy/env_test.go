package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takezoh/credproxy/credproxy"
	"github.com/takezoh/credproxy/internal/testenv"
)

// brokerConfig builds a server config with the given routes, unauthenticated
// unless the test supplied tokens.
func brokerConfig(routes []credproxy.Route, tokens []credproxy.TokenAuth) credproxy.ServerConfig {
	cfg := credproxy.ServerConfig{AuthTokens: tokens, Routes: routes}
	if len(tokens) == 0 {
		cfg.AllowUnauthenticated = true
	}
	return cfg
}

// startBrokerRoutes runs a credproxy server with arbitrary routes and points the
// client's transport seam at it, returning the socket path the client should be
// invoked with.
//
// The server listens on TCP loopback and the returned path is a placeholder
// file, because what these tests check — route discovery, merging, shell
// quoting, error handling, output format — is client logic that a Unix socket
// only carries. Running them over loopback keeps them meaningful in
// environments that deny AF_UNIX; the socket transport itself is covered
// separately by TestEnvCmd_overRealUnixSocket.
//
// The placeholder must exist on disk: "credproxy env" checks for the socket
// before making a request and stays quiet when it is absent.
func startBrokerRoutes(t *testing.T, routes []credproxy.Route, tokens []credproxy.TokenAuth) string {
	t.Helper()
	cfg := brokerConfig(routes, tokens)
	cfg.ListenTCP = "127.0.0.1:0"
	srv, err := credproxy.New(cfg)
	if err != nil {
		t.Fatalf("credproxy.New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Run(ctx) }()

	sock := filepath.Join(t.TempDir(), "broker.sock")
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatalf("placeholder socket: %v", err)
	}

	addr := srv.Addr()
	prev := brokerDialer
	t.Cleanup(func() { brokerDialer = prev })
	brokerDialer = func(ctx context.Context, socketPath string) (net.Conn, error) {
		// The client must still route by the path it was given; a seam that
		// ignored it would hide a wiring mistake in every test below.
		if socketPath != sock {
			return nil, fmt.Errorf("dialed %q, want %q", socketPath, sock)
		}
		var d net.Dialer
		return d.DialContext(ctx, "tcp", addr)
	}
	return sock
}

// startUnixBrokerRoutes runs a credproxy server on a real Unix socket in a temp
// dir and returns its path. Callers must gate on testenv.RequireUnixSocket.
func startUnixBrokerRoutes(t *testing.T, routes []credproxy.Route, tokens []credproxy.TokenAuth) string {
	t.Helper()
	cfg := brokerConfig(routes, tokens)
	cfg.ListenUnix = filepath.Join(t.TempDir(), "broker.sock")
	srv, err := credproxy.New(cfg)
	if err != nil {
		t.Fatalf("credproxy.New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Run(ctx) }()
	return cfg.ListenUnix
}

// runEnv executes "credproxy env" against sock and returns stdout/stderr.
func runEnv(t *testing.T, sock string, extra ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errBuf bytes.Buffer
	args := append([]string{"--socket", sock}, extra...)
	err = runEnvCmd(args, &out, &errBuf)
	return out.String(), errBuf.String(), err
}

func TestEnvCmd_mergesDiscoveredRoutes(t *testing.T) {
	sock := startBrokerRoutes(t, []credproxy.Route{
		{Path: "/ctx-sync", Provider: &envProvider{body: `{"env":{"CTX_TOKEN":"a"}}`}},
		{Path: "/grok-x-search", Provider: &envProvider{body: `{"env":{"XAI_API_KEY":"b"}}`}},
	}, nil)

	stdout, stderr, err := runEnv(t, sock)
	if err != nil {
		t.Fatalf("runEnvCmd: %v (stderr: %s)", err, stderr)
	}
	want := "export CTX_TOKEN='a'\nexport XAI_API_KEY='b'\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestEnvCmd_skipsRouteWithoutEnv(t *testing.T) {
	sock := startBrokerRoutes(t, []credproxy.Route{
		{Path: "/aws-credentials", Provider: &envProvider{body: `{"AccessKeyId":"AKIA","SecretAccessKey":"s"}`}},
		{Path: "/ctx-sync", Provider: &envProvider{body: `{"env":{"CTX_TOKEN":"a"}}`}},
	}, nil)

	stdout, stderr, err := runEnv(t, sock)
	if err != nil {
		t.Fatalf("runEnvCmd: %v (stderr: %s)", err, stderr)
	}
	if stdout != "export CTX_TOKEN='a'\n" {
		t.Errorf("stdout = %q", stdout)
	}
	if strings.Contains(stdout, "AKIA") || strings.Contains(stderr, "AKIA") {
		t.Errorf("non-env route body must not surface: stdout=%q stderr=%q", stdout, stderr)
	}
	// A route that simply serves another credential shape is not a failure.
	if strings.Contains(stderr, "aws-credentials") {
		t.Errorf("skip should be silent, stderr = %q", stderr)
	}
}

// TestEnvCmd_shellQuotingSurvivesEval is the load-bearing test for the "sh"
// format: the output only has to be correct once it has been through eval.
func TestEnvCmd_shellQuotingSurvivesEval(t *testing.T) {
	hostile := "it's \"quoted\" $(touch /nonexistent-marker) `id` a\\b\n\tmulti line   spaced $HOME"
	body, err := json.Marshal(map[string]map[string]string{"env": {"WEIRD": hostile}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	sock := startBrokerRoutes(t, []credproxy.Route{
		{Path: "/ctx-sync", Provider: &envProvider{body: string(body)}},
	}, nil)

	stdout, stderr, err := runEnv(t, sock)
	if err != nil {
		t.Fatalf("runEnvCmd: %v (stderr: %s)", err, stderr)
	}

	cmd := exec.Command("sh", "-c", `eval "$1"; printf '%s' "$WEIRD"`, "sh", stdout)
	got, err := cmd.Output()
	if err != nil {
		t.Fatalf("eval: %v (exports: %q)", err, stdout)
	}
	if string(got) != hostile {
		t.Errorf("round trip through eval = %q, want %q", got, hostile)
	}
}

func TestEnvCmd_brokerAbsentIsQuietSuccess(t *testing.T) {
	stdout, _, err := runEnv(t, filepath.Join(t.TempDir(), "missing.sock"))
	if err != nil {
		t.Fatalf("missing broker must not fail the shell: %v", err)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

func TestEnvCmd_typedErrorSkipsOnlyThatRoute(t *testing.T) {
	sock := startBrokerRoutes(t, []credproxy.Route{
		{Path: "/broken", Provider: &envProvider{err: &credproxy.ReasonError{
			Reason: "op_rate_limited",
			Err:    fmt.Errorf("detail sensitive-blob"),
		}}},
		{Path: "/ctx-sync", Provider: &envProvider{body: `{"env":{"CTX_TOKEN":"a"}}`}},
	}, nil)

	stdout, stderr, err := runEnv(t, sock)
	if err != nil {
		t.Fatalf("one failing route must not fail the shell: %v", err)
	}
	if stdout != "export CTX_TOKEN='a'\n" {
		t.Errorf("stdout = %q", stdout)
	}
	if !strings.Contains(stderr, "broken") || !strings.Contains(stderr, "op_rate_limited") {
		t.Errorf("stderr should name the route and reason: %q", stderr)
	}
	if strings.Contains(stderr, "sensitive-blob") {
		t.Errorf("stderr must not carry server-side detail: %q", stderr)
	}
}

func TestEnvCmd_explicitRoutesOnly(t *testing.T) {
	sock := startBrokerRoutes(t, []credproxy.Route{
		{Path: "/ctx-sync", Provider: &envProvider{body: `{"env":{"CTX_TOKEN":"a"}}`}},
		{Path: "/grok-x-search", Provider: &envProvider{body: `{"env":{"XAI_API_KEY":"b"}}`}},
	}, nil)

	stdout, stderr, err := runEnv(t, sock, "--route", "grok-x-search")
	if err != nil {
		t.Fatalf("runEnvCmd: %v (stderr: %s)", err, stderr)
	}
	if stdout != "export XAI_API_KEY='b'\n" {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestEnvCmd_jsonFormat(t *testing.T) {
	sock := startBrokerRoutes(t, []credproxy.Route{
		{Path: "/ctx-sync", Provider: &envProvider{body: `{"env":{"CTX_TOKEN":"a"}}`}},
	}, nil)

	stdout, stderr, err := runEnv(t, sock, "--format", "json")
	if err != nil {
		t.Fatalf("runEnvCmd: %v (stderr: %s)", err, stderr)
	}
	var got struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON (%q): %v", stdout, err)
	}
	if got.Env["CTX_TOKEN"] != "a" || len(got.Env) != 1 {
		t.Errorf("env = %v", got.Env)
	}
}

func TestEnvCmd_bearerAuth(t *testing.T) {
	tokens := []credproxy.TokenAuth{{Token: "tok-1", ID: "client-a"}}
	sock := startBrokerRoutes(t, []credproxy.Route{
		{Path: "/ctx-sync", Provider: &envProvider{body: `{"env":{"CTX_TOKEN":"a"}}`}},
	}, tokens)

	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("tok-1\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	stdout, stderr, err := runEnv(t, sock, "--token-file", tokenFile)
	if err != nil {
		t.Fatalf("runEnvCmd: %v (stderr: %s)", err, stderr)
	}
	if stdout != "export CTX_TOKEN='a'\n" {
		t.Errorf("stdout = %q", stdout)
	}

	// Without the token, discovery fails and nothing is exported — but the
	// shell still starts.
	stdout, _, err = runEnv(t, sock)
	if err != nil {
		t.Fatalf("unauthenticated call must not fail the shell: %v", err)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

func TestEnvCmd_missingTokenFileIsQuietSuccess(t *testing.T) {
	sock := startBrokerRoutes(t, []credproxy.Route{
		{Path: "/ctx-sync", Provider: &envProvider{body: `{"env":{"CTX_TOKEN":"a"}}`}},
	}, nil)

	stdout, _, err := runEnv(t, sock, "--token-file", filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("missing token file must not fail the shell: %v", err)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

// TestEnvCmd_overRealUnixSocket is the one test that exercises the transport the
// other "env" tests stub out: a real Unix socket, dialed by the production
// dialer. Everything else about "credproxy env" is checked over loopback, so
// this is the only place a sandbox that denies AF_UNIX costs coverage.
func TestEnvCmd_overRealUnixSocket(t *testing.T) {
	testenv.RequireUnixSocket(t)

	sock := startUnixBrokerRoutes(t, []credproxy.Route{
		{Path: "/ctx-sync", Provider: &envProvider{body: `{"env":{"CTX_TOKEN":"a"}}`}},
	}, nil)

	stdout, stderr, err := runEnv(t, sock)
	if err != nil {
		t.Fatalf("runEnvCmd: %v (stderr: %s)", err, stderr)
	}
	if stdout != "export CTX_TOKEN='a'\n" {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestEnvCmd_rejectsBadFlags(t *testing.T) {
	var out, errBuf bytes.Buffer
	if err := runEnvCmd([]string{"--format", "yaml"}, &out, &errBuf); err == nil {
		t.Error("expected error for unknown format")
	}
	if err := runEnvCmd([]string{"--route", "../etc"}, &out, &errBuf); err == nil {
		t.Error("expected error for path-smuggling route name")
	}
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"":             "''",
		"plain":        "'plain'",
		"a b":          "'a b'",
		"it's":         `'it'\''s'`,
		"$(id)":        "'$(id)'",
		"line1\nline2": "'line1\nline2'",
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidEnvName(t *testing.T) {
	valid := []string{"A", "_x", "CTX_TOKEN", "a1"}
	invalid := []string{"", "1A", "A-B", "A B", "A;rm -rf /", "A=1", "ロート"}
	for _, s := range valid {
		if !validEnvName(s) {
			t.Errorf("validEnvName(%q) = false, want true", s)
		}
	}
	for _, s := range invalid {
		if validEnvName(s) {
			t.Errorf("validEnvName(%q) = true, want false", s)
		}
	}
}
