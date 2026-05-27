package secretenv

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeHookScript(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("unix hook scripts not supported on windows")
	}
	p := filepath.Join(t.TempDir(), "hook.sh")
	require.NoError(t, os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o700))
	return p
}

func TestScriptHook_resolve(t *testing.T) {
	p := writeHookScript(t, `echo '{"value":"my-secret","expires_in_sec":0}'`)
	h := NewScriptHook([]string{p}, 0)
	got, err := h.Resolve(context.Background(), "any://ref")
	require.NoError(t, err)
	assert.Equal(t, "my-secret", got)
}

func TestScriptHook_exitError(t *testing.T) {
	p := writeHookScript(t, `echo "not found" >&2; exit 1`)
	h := NewScriptHook([]string{p}, 0)
	_, err := h.Resolve(context.Background(), "bad://ref")
	assert.ErrorContains(t, err, "scripthook")
	assert.ErrorContains(t, err, "not found")
}

func TestScriptHook_unknownField(t *testing.T) {
	p := writeHookScript(t, `echo '{"value":"x","expires_in_sec":0,"extra":"bad"}'`)
	h := NewScriptHook([]string{p}, 0)
	_, err := h.Resolve(context.Background(), "ref")
	assert.ErrorContains(t, err, "decode response")
}

func TestScriptHook_emptyValue(t *testing.T) {
	p := writeHookScript(t, `echo '{"value":"","expires_in_sec":0}'`)
	h := NewScriptHook([]string{p}, 0)
	_, err := h.Resolve(context.Background(), "ref")
	assert.ErrorContains(t, err, "empty value")
}

func TestScriptHook_cacheHit(t *testing.T) {
	callCount := 0
	p := writeHookScript(t, `echo '{"value":"cached","expires_in_sec":3600}'`)

	// Wrap to count invocations via the hook script counting on filesystem.
	countFile := filepath.Join(t.TempDir(), "count")
	script := `count=$(cat ` + countFile + ` 2>/dev/null || echo 0); echo $((count+1)) > ` + countFile + `
echo '{"value":"cached","expires_in_sec":3600}'`
	p = writeHookScript(t, script)
	_ = callCount

	h := NewScriptHook([]string{p}, 0)
	v1, err := h.Resolve(context.Background(), "ref://x")
	require.NoError(t, err)
	v2, err := h.Resolve(context.Background(), "ref://x")
	require.NoError(t, err)
	assert.Equal(t, v1, v2)

	// Count file should show the script ran exactly once.
	countBytes, err := os.ReadFile(countFile)
	require.NoError(t, err)
	assert.Equal(t, "1\n", string(countBytes))
}
