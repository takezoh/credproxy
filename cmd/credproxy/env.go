package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// maxRouteListResponse bounds the /_routes body read into memory.
const maxRouteListResponse = 1 << 16 // 64 KiB

// routeListPath is the broker's reserved route-discovery endpoint.
const routeListPath = "_routes"

// envCmd implements "credproxy env": fetch every broker route that serves an
// env map and print it as shell exports (or JSON). It exists so that adding a
// credential is a route definition on the daemon side and nothing else — the
// consumer stays the single fixed line `eval "$(credproxy env)"`, with no
// per-key snippet to write, and route discovery replaces the hardcoded name.
//
// It degrades quietly by design: a missing or unreachable broker prints nothing
// on stdout and exits 0, because this runs in shell startup and a broken
// eval would cost the user their shell, not just their credentials.
func envCmd(args []string) error {
	return runEnvCmd(args, os.Stdout, os.Stderr)
}

// routeList collects a repeatable --route flag.
type routeList []string

func (r *routeList) String() string { return strings.Join(*r, ",") }

func (r *routeList) Set(v string) error {
	*r = append(*r, v)
	return nil
}

func runEnvCmd(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("env", flag.ContinueOnError)
	fs.SetOutput(stderr)
	socket := fs.String("socket", "", "credproxyd Unix socket path (default: $XDG_RUNTIME_DIR/credproxyd/broker.sock)")
	tokenFile := fs.String("token-file", "", "file containing the bearer token (optional)")
	format := fs.String("format", "sh", "output format: sh or json")
	timeoutSec := fs.Int("timeout-sec", 10, "broker request timeout in seconds")
	var routes routeList
	fs.Var(&routes, "route", "broker route to fetch (repeatable; default: every env-serving route)")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *format != "sh" && *format != "json" {
		return fmt.Errorf("--format must be sh or json")
	}
	for _, r := range routes {
		if !validRouteName(r) {
			return fmt.Errorf("--route must match [a-z0-9_-]+")
		}
	}

	socketPath := *socket
	if socketPath == "" {
		socketPath = defaultBrokerSocket()
	}

	token, ok := loadBrokerToken(*tokenFile, stderr)
	if !ok {
		return nil
	}
	if _, err := os.Stat(socketPath); err != nil {
		fmt.Fprintf(stderr, "credproxy env: broker socket unavailable (%s); no variables exported\n", socketPath)
		return nil
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	timeout := time.Duration(*timeoutSec) * time.Second
	if len(routes) == 0 {
		discovered, err := fetchRouteNames(ctx, socketPath, token, timeout, stderr)
		if err != nil {
			fmt.Fprintf(stderr, "credproxy env: route discovery failed: %v\n", err)
			return nil
		}
		routes = discovered
	}

	env := collectRouteEnv(ctx, socketPath, token, timeout, routes, stderr)
	return writeEnv(stdout, stderr, *format, env)
}

// defaultBrokerSocket resolves the per-user broker socket the same way the
// daemon's unit file does, so the consumer snippet needs no path argument.
func defaultBrokerSocket() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	return filepath.Join(dir, "credproxyd", "broker.sock")
}

// loadBrokerToken reads the bearer token file. A file that does not exist is
// treated as "broker not installed here" (quiet, no output) rather than an
// error, for the same reason an absent socket is: this runs in shell startup.
// ok=false means the caller should exit 0 without exporting anything.
func loadBrokerToken(path string, stderr io.Writer) (token string, ok bool) {
	if path == "" {
		return "", true
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(stderr, "credproxy env: token file unavailable (%s); no variables exported\n", path)
		} else {
			fmt.Fprintf(stderr, "credproxy env: token file: %v\n", err)
		}
		return "", false
	}
	return strings.TrimSpace(string(data)), true
}

// fetchRouteNames asks the broker which routes it can answer locally. Names the
// broker reports that a client could not address are dropped here rather than
// sent back as a URL component.
func fetchRouteNames(ctx context.Context, socketPath, token string, timeout time.Duration, stderr io.Writer) ([]string, error) {
	resp, err := brokerGet(ctx, socketPath, routeListPath, token, timeout)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var out struct {
		Routes []string `json:"routes"`
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxRouteListResponse))
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("broker response: %w", err)
	}

	names := make([]string, 0, len(out.Routes))
	for _, n := range out.Routes {
		if !validRouteName(n) {
			fmt.Fprintf(stderr, "credproxy env: skipping unaddressable route name\n")
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

// collectRouteEnv resolves each route and merges the results. One route's
// failure must not cost the user every other credential, so a failing route is
// reported by name and reason only — never by value — and the rest continue.
func collectRouteEnv(ctx context.Context, socketPath, token string, timeout time.Duration, routes []string, stderr io.Writer) map[string]string {
	merged := make(map[string]string)
	origin := make(map[string]string)
	for _, route := range routes {
		env, err := fetchRouteEnv(ctx, socketPath, route, token, timeout)
		if err != nil {
			fmt.Fprintf(stderr, "credproxy env: route %s: %v\n", route, err)
			continue
		}
		// A route with no "env" key serves some other credential shape; it is
		// not an error, just not an env source.
		for name, value := range env {
			if prev, dup := origin[name]; dup {
				fmt.Fprintf(stderr, "credproxy env: %s from route %s overrides route %s\n", name, route, prev)
			}
			merged[name] = value
			origin[name] = route
		}
	}
	return merged
}

// writeEnv prints the merged map. Values reach stdout and nowhere else: not
// argv, not the log, not a diagnostic.
func writeEnv(stdout, stderr io.Writer, format string, env map[string]string) error {
	if format == "json" {
		return json.NewEncoder(stdout).Encode(struct {
			Env map[string]string `json:"env"`
		}{Env: env})
	}

	names := make([]string, 0, len(env))
	for name := range env {
		if !validEnvName(name) {
			fmt.Fprintf(stderr, "credproxy env: skipping variable with an unusable name\n")
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, err := fmt.Fprintf(stdout, "export %s=%s\n", name, shellQuote(env[name])); err != nil {
			return err
		}
	}
	return nil
}

// validEnvName restricts what may be emitted as a shell assignment. The broker
// side is trusted for values but the name becomes shell syntax, so anything
// that is not a plain identifier is dropped rather than quoted.
func validEnvName(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		alpha := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
		if alpha || (i > 0 && c >= '0' && c <= '9') {
			continue
		}
		return false
	}
	return true
}

// shellQuote renders v as a single-quoted shell word. Inside single quotes the
// shell interprets nothing, so newlines, spaces, $(...), backticks and
// backslashes survive eval verbatim. An embedded single quote cannot be escaped
// inside that region, so it is emitted by closing the quoted region, adding a
// backslash-escaped quote, and reopening the region.
func shellQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}
