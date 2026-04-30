package script_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/takezoh/credproxy/cmd/credproxyd/providers/script"
	"github.com/takezoh/credproxy/credproxy"
)

func writeScript(t *testing.T, content string) []string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "hook.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+content), 0o755); err != nil {
		t.Fatal(err)
	}
	return []string{"sh", path}
}

func TestScriptProvider_Get_executes(t *testing.T) {
	cmd := writeScript(t, `echo '{"headers":{"Authorization":"Bearer tok"},"expires_in_sec":3600}'`)
	p := script.New("test", cmd, nil, 5*time.Second)
	inj, err := p.Get(context.Background(), credproxy.Request{Method: "GET"})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if inj.Headers["Authorization"] != "Bearer tok" {
		t.Errorf("Authorization = %q", inj.Headers["Authorization"])
	}
}

func TestScriptProvider_Get_cached(t *testing.T) {
	cmd := writeScript(t, `echo '{"headers":{"X-Ok":"1"},"expires_in_sec":3600}'`)
	p := script.New("test", cmd, nil, 5*time.Second)
	req := credproxy.Request{Method: "GET"}
	r1, _ := p.Get(context.Background(), req)
	r2, _ := p.Get(context.Background(), req)
	if r1.Headers["X-Ok"] != r2.Headers["X-Ok"] {
		t.Errorf("cached response differs: %v vs %v", r1, r2)
	}
}

func TestScriptProvider_Refresh_bypassesCache(t *testing.T) {
	getCmd := writeScript(t, `echo '{"headers":{"X-From":"get"},"expires_in_sec":3600}'`)
	refreshCmd := writeScript(t, `echo '{"headers":{"X-From":"refresh"},"expires_in_sec":3600}'`)
	p := script.New("test", getCmd, refreshCmd, 5*time.Second)
	req := credproxy.Request{Method: "GET"}

	// Prime the cache.
	_, _ = p.Get(context.Background(), req)

	// Refresh must bypass cache and use refreshCmd.
	inj, err := p.Refresh(context.Background(), req)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if inj.Headers["X-From"] != "refresh" {
		t.Errorf("Refresh returned wrong header: %v", inj.Headers)
	}
}

func TestScriptProvider_Get_exitError(t *testing.T) {
	cmd := writeScript(t, `exit 1`)
	p := script.New("test", cmd, nil, 5*time.Second)
	_, err := p.Get(context.Background(), credproxy.Request{})
	if err == nil {
		t.Error("expected error on non-zero exit")
	}
}

func TestScriptProvider_Get_emptyCmd(t *testing.T) {
	p := script.New("test", nil, nil, 5*time.Second)
	inj, err := p.Get(context.Background(), credproxy.Request{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(inj.Headers) != 0 {
		t.Errorf("expected empty injection, got %v", inj.Headers)
	}
}

func TestScriptProvider_bodyReplace(t *testing.T) {
	cmd := writeScript(t, `echo '{"body_replace":{"AccessKeyId":"AK"}}'`)
	p := script.New("test", cmd, nil, 5*time.Second)
	inj, err := p.Get(context.Background(), credproxy.Request{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(inj.BodyReplace) == 0 {
		t.Error("expected BodyReplace to be set")
	}
}
