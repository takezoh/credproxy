package secretenv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const defaultHookSafety = 30 * time.Second

// ScriptHook implements Hook by executing a command subprocess.
//
// Protocol (one call per ref):
//
//	stdin:  {"ref": "<opaque-reference-string>"}
//	stdout: {"value": "<resolved-secret>", "expires_in_sec": N}
//	exit 0: success; non-zero: error (stderr is forwarded)
//
// Results are cached per ref using the TTL from expires_in_sec minus a 30-second
// safety margin. Concurrent Resolve calls for the same ref are deduplicated via
// singleflight. ScriptHook is safe for concurrent use.
type ScriptHook struct {
	cmd     []string
	timeout time.Duration
	safety  time.Duration

	mu    sync.Mutex
	cache map[string]*hookCacheEntry
	sf    singleflight.Group
}

type hookCacheEntry struct {
	value   string
	expires time.Time
}

type hookReq struct {
	Ref string `json:"ref"`
}

type hookResp struct {
	Value        string `json:"value"`
	ExpiresInSec int    `json:"expires_in_sec"`
}

// NewScriptHook creates a ScriptHook that executes cmd[0] with cmd[1:].
// timeout bounds each subprocess execution; 0 uses no timeout.
func NewScriptHook(cmd []string, timeout time.Duration) *ScriptHook {
	return &ScriptHook{
		cmd:     cmd,
		timeout: timeout,
		safety:  defaultHookSafety,
		cache:   make(map[string]*hookCacheEntry),
	}
}

// Resolve returns the resolved secret for ref, using the cache when valid.
func (h *ScriptHook) Resolve(ctx context.Context, ref string) (string, error) {
	if v, ok := h.cacheGet(ref, time.Now()); ok {
		return v, nil
	}
	type sfResult struct {
		value   string
		expires time.Time
	}
	v, err, _ := h.sf.Do(ref, func() (any, error) {
		if cached, ok := h.cacheGet(ref, time.Now()); ok {
			return sfResult{value: cached}, nil
		}
		val, expires, err := h.run(ctx, ref)
		if err != nil {
			return nil, err
		}
		if !expires.IsZero() {
			h.cacheSet(ref, val, expires)
		}
		return sfResult{value: val, expires: expires}, nil
	})
	if err != nil {
		return "", err
	}
	return v.(sfResult).value, nil
}

func (h *ScriptHook) run(ctx context.Context, ref string) (string, time.Time, error) {
	if len(h.cmd) == 0 {
		return "", time.Time{}, fmt.Errorf("scripthook: hook command is not configured")
	}
	runCtx := ctx
	if h.timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, h.timeout)
		defer cancel()
	}

	stdin, err := json.Marshal(hookReq{Ref: ref})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("scripthook: encode request: %w", err)
	}

	var stdout, stderr bytes.Buffer
	c := exec.CommandContext(runCtx, h.cmd[0], h.cmd[1:]...)
	c.Stdin = bytes.NewReader(stdin)
	c.Stdout = &stdout
	c.Stderr = &stderr

	if err := c.Run(); err != nil {
		return "", time.Time{}, fmt.Errorf("scripthook %v: %w (stderr: %s)", h.cmd, err, stderr.String())
	}

	return h.parseResp(stdout.Bytes(), ref)
}

func (h *ScriptHook) parseResp(out []byte, ref string) (string, time.Time, error) {
	var resp hookResp
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&resp); err != nil {
		return "", time.Time{}, fmt.Errorf("scripthook: decode response for %q: %w", ref, err)
	}
	if resp.Value == "" {
		return "", time.Time{}, fmt.Errorf("scripthook: empty value for ref %q", ref)
	}
	var expires time.Time
	if ttl := time.Duration(resp.ExpiresInSec)*time.Second - h.safety; ttl > 0 {
		expires = time.Now().Add(ttl)
	}
	return resp.Value, expires, nil
}

func (h *ScriptHook) cacheGet(ref string, now time.Time) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e, ok := h.cache[ref]
	if !ok || !now.Before(e.expires) {
		return "", false
	}
	return e.value, true
}

func (h *ScriptHook) cacheSet(ref string, value string, expires time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cache[ref] = &hookCacheEntry{value: value, expires: expires}
}
