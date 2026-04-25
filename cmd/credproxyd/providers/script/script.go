package script

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/takezoh/credproxy/pkg/credproxy"
)

const defaultSafety = 30 * time.Second

// hookRequest is the JSON structure sent to hook subprocesses on stdin.
type hookRequest struct {
	Action  string      `json:"action"`
	Route   string      `json:"route"`
	Request hookReqInfo `json:"request"`
	Context hookCtx     `json:"context"`
}

type hookReqInfo struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Host   string `json:"host"`
}

type hookCtx struct {
	Client      string `json:"client,omitempty"`
	ProjectPath string `json:"project_path,omitempty"`
}

// hookResponse is the JSON structure parsed from hook subprocess stdout.
type hookResponse struct {
	Headers      map[string]string `json:"headers"`
	Query        map[string]string `json:"query"`
	ExpiresInSec int               `json:"expires_in_sec"`
	BodyReplace  json.RawMessage   `json:"body_replace,omitempty"`
}

// Provider implements credproxy.Provider by executing hook scripts.
// It is safe for concurrent use.
type Provider struct {
	routeName  string
	getCmd     []string
	refreshCmd []string
	timeout    time.Duration
	safety     time.Duration
	cache      ttlCache
}

// New creates a ScriptProvider for routeName.
// getCmd is executed on Get(), refreshCmd on Refresh().
// timeout is applied per subprocess execution.
func New(routeName string, getCmd, refreshCmd []string, timeout time.Duration) *Provider {
	return &Provider{
		routeName:  routeName,
		getCmd:     getCmd,
		refreshCmd: refreshCmd,
		timeout:    timeout,
		safety:     defaultSafety,
	}
}

func (p *Provider) Get(ctx context.Context, req credproxy.Request) (*credproxy.Injection, error) {
	if inj, ok := p.cache.get(time.Now()); ok {
		return inj, nil
	}
	return p.cache.do(p.routeName+":get", func() (*credproxy.Injection, time.Time, error) {
		return p.run(ctx, "get", req, p.getCmd)
	})
}

func (p *Provider) Refresh(ctx context.Context, req credproxy.Request) (*credproxy.Injection, error) {
	p.cache.invalidate()
	cmd := p.refreshCmd
	if len(cmd) == 0 {
		cmd = p.getCmd
	}
	return p.cache.do(p.routeName+":refresh", func() (*credproxy.Injection, time.Time, error) {
		return p.run(ctx, "refresh", req, cmd)
	})
}

func (p *Provider) run(ctx context.Context, action string, req credproxy.Request, cmd []string) (*credproxy.Injection, time.Time, error) {
	if len(cmd) == 0 {
		return &credproxy.Injection{}, time.Time{}, nil
	}

	tCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	stdin, err := buildHookRequest(action, p.routeName, req)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("script: %w", err)
	}

	var stdout, stderr bytes.Buffer
	c := exec.CommandContext(tCtx, cmd[0], cmd[1:]...)
	c.Stdin = bytes.NewReader(stdin)
	c.Stdout = &stdout
	c.Stderr = &stderr

	if err := c.Run(); err != nil {
		return nil, time.Time{}, fmt.Errorf("script %v: %w (stderr: %s)", cmd, err, stderr.String())
	}

	inj, cacheUntil, err := parseHookResponse(stdout.Bytes(), time.Now(), p.safety)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("script %v: %w", cmd, err)
	}

	return inj, cacheUntil, nil
}
