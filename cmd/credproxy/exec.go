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
	"time"
)

// maxEnvResponse bounds the broker response body read into memory.
const maxEnvResponse = 1 << 20 // 1 MiB

// maxErrorBody bounds the error-body read used for reason extraction.
const maxErrorBody = 4096

// execCmd implements "credproxy exec": fetch an env map for a broker-defined
// route over the local broker transport and exec the command with those variables injected.
//
// Unlike "run"/"resolve", the caller cannot name refs or env-files — the only
// degree of freedom is the route name, and the route → ref mapping lives on the
// daemon side. This is the client half of the fixed-wrapper pattern: agents get
// "which wrapper to call", never "which secret to resolve".
func execCmd(args []string) error {
	options, cmdArgs, err := parseExecOptions(args)
	if err != nil {
		return err
	}
	socketPath := options.socket
	if socketPath == "" {
		socketPath = defaultExecEndpoint()
	}
	if socketPath == "" {
		return fmt.Errorf("--socket is required")
	}
	token, err := readExecToken(options.tokenFile)
	if err != nil {
		return err
	}
	expectedExe, err := expectedServerExecutable(options.serverExecutable)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), commandSignals()...)
	defer stop()
	env, err := fetchEnv(ctx, socketPath, expectedExe, options.route, token, options.timeout)
	if err != nil {
		return err
	}
	bin, err := exec.LookPath(cmdArgs[0])
	if err != nil {
		return fmt.Errorf("lookup %s: %w", cmdArgs[0], err)
	}
	return execProcess(bin, cmdArgs, mergeEnv(os.Environ(), env))
}

type execOptions struct {
	socket           string
	serverExecutable string
	route            string
	tokenFile        string
	timeout          time.Duration
}

func parseExecOptions(args []string) (execOptions, []string, error) {
	fs := flag.NewFlagSet("exec", flag.ContinueOnError)
	socket := fs.String("socket", "", "credproxyd Unix socket or named-pipe endpoint")
	serverExecutable := fs.String("server-executable", "", "expected protected credproxyd executable on Windows")
	route := fs.String("route", "", "broker route name to fetch env from (required)")
	tokenFile := fs.String("token-file", "", "file containing the bearer token (optional)")
	timeoutSec := fs.Int("timeout-sec", 10, "broker request timeout in seconds")

	flagArgs, cmdArgs := splitAtDashDash(args)
	if err := fs.Parse(flagArgs); err != nil {
		return execOptions{}, nil, err
	}
	if !validRouteName(*route) {
		return execOptions{}, nil, fmt.Errorf("--route must match [a-z0-9_-]+")
	}
	if len(cmdArgs) == 0 {
		return execOptions{}, nil, fmt.Errorf("command is required after --")
	}
	return execOptions{
		socket: *socket, serverExecutable: *serverExecutable, route: *route,
		tokenFile: *tokenFile, timeout: time.Duration(*timeoutSec) * time.Second,
	}, cmdArgs, nil
}

func readExecToken(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("token file: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
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
func brokerGet(ctx context.Context, socketPath, expectedExe, path, token string, timeout time.Duration) (*http.Response, error) {
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return brokerDialer(ctx, socketPath, expectedExe)
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
func fetchRouteEnv(ctx context.Context, socketPath, expectedExe, route, token string, timeout time.Duration) (map[string]string, error) {
	resp, err := brokerGet(ctx, socketPath, expectedExe, route+"/", token, timeout)
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
func fetchEnv(ctx context.Context, socketPath, expectedExe, route, token string, timeout time.Duration) (map[string]string, error) {
	env, err := fetchRouteEnv(ctx, socketPath, expectedExe, route, token, timeout)
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
