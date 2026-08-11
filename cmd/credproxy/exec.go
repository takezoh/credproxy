package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// maxEnvResponse bounds the broker response body read into memory.
const maxEnvResponse = 1 << 20 // 1 MiB

// maxErrorBody bounds the error-body read used for reason extraction.
const maxErrorBody = 4096

// execCmd implements "credproxy exec": fetch an env map for a broker-defined
// route over a Unix socket and exec the command with those variables injected.
//
// Unlike "run"/"resolve", the caller cannot name refs or env-files — the only
// degree of freedom is the route name, and the route → ref mapping lives on the
// daemon side. This is the client half of the fixed-wrapper pattern: agents get
// "which wrapper to call", never "which secret to resolve".
func execCmd(args []string) error {
	fs := flag.NewFlagSet("exec", flag.ContinueOnError)
	socket := fs.String("socket", "", "credproxyd Unix socket path (required)")
	route := fs.String("route", "", "broker route name to fetch env from (required)")
	tokenFile := fs.String("token-file", "", "file containing the bearer token (optional)")
	timeoutSec := fs.Int("timeout-sec", 10, "broker request timeout in seconds")

	flagArgs, cmdArgs := splitAtDashDash(args)
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if *socket == "" {
		return fmt.Errorf("--socket is required")
	}
	if !validRouteName(*route) {
		return fmt.Errorf("--route must match [a-z0-9_-]+")
	}
	if len(cmdArgs) == 0 {
		return fmt.Errorf("command is required after --")
	}

	var token string
	if *tokenFile != "" {
		data, err := os.ReadFile(*tokenFile)
		if err != nil {
			return fmt.Errorf("token file: %w", err)
		}
		token = strings.TrimSpace(string(data))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	env, err := fetchEnv(ctx, *socket, *route, token, time.Duration(*timeoutSec)*time.Second)
	if err != nil {
		return err
	}

	bin, err := exec.LookPath(cmdArgs[0])
	if err != nil {
		return fmt.Errorf("lookup %s: %w", cmdArgs[0], err)
	}
	return syscall.Exec(bin, cmdArgs, mergeEnv(os.Environ(), env))
}

// validRouteName restricts the only caller-controlled request component so it
// cannot smuggle path segments ("../", "%2f", …) into the broker URL.
func validRouteName(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

// brokerGet performs an authenticated GET against the broker's Unix socket and
// returns the response on 200, or a typed error otherwise. path is appended to
// the fixed host and must already be caller-validated.
func brokerGet(ctx context.Context, socketPath, path, token string, timeout time.Duration) (*http.Response, error) {
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			},
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://credproxyd/"+path, nil)
	if err != nil {
		return nil, fmt.Errorf("broker request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("broker %s: %w", socketPath, err)
	}
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		return nil, brokerError(resp)
	}
	return resp, nil
}

// fetchRouteEnv requests one route's body from credproxyd and returns its env
// map, using the same {"env":{...}} schema as "credproxy resolve". A body
// without an "env" key yields (nil, nil): that route serves some other
// credential shape (e.g. a synthetic AWS endpoint) and is not an env source.
func fetchRouteEnv(ctx context.Context, socketPath, route, token string, timeout time.Duration) (map[string]string, error) {
	resp, err := brokerGet(ctx, socketPath, route+"/", token, timeout)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var out struct {
		Env map[string]string `json:"env"`
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxEnvResponse))
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("broker response: %w", err)
	}
	return out.Env, nil
}

// fetchEnv is the strict variant used by "exec", where a route that serves no
// env is a misconfiguration rather than something to skip.
func fetchEnv(ctx context.Context, socketPath, route, token string, timeout time.Duration) (map[string]string, error) {
	env, err := fetchRouteEnv(ctx, socketPath, route, token, timeout)
	if err != nil {
		return nil, err
	}
	if env == nil {
		return nil, fmt.Errorf("broker response: missing env")
	}
	return env, nil
}

// brokerError extracts the machine-readable reason from a structured error
// body when present, so callers see "op_rate_limited" instead of a bare 502.
func brokerError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	var e struct {
		Error  string `json:"error"`
		Reason string `json:"reason"`
	}
	if json.Unmarshal(body, &e) == nil && e.Reason != "" {
		return fmt.Errorf("broker: %s (reason: %s)", e.Error, e.Reason)
	}
	return fmt.Errorf("broker: status %d", resp.StatusCode)
}
