package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeConfig writes a minimal config file with the given hook command.
func writeConfig(t *testing.T, dir string, hookCmd ...string) string {
	t.Helper()
	path := filepath.Join(dir, "config.toml")
	content := "hook = ["
	for i, c := range hookCmd {
		if i > 0 {
			content += ", "
		}
		content += `"` + c + `"`
	}
	content += "]\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// writeEnvFile writes an env-file and returns its path.
func writeEnvFile(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "test.env")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write env-file: %v", err)
	}
	return path
}

// writeEchoHook writes a shell script that echoes a fixed JSON response.
func writeEchoHook(t *testing.T, dir, response string) string {
	t.Helper()
	path := filepath.Join(dir, "hook.sh")
	script := "#!/bin/sh\necho '" + response + "'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write hook: %v", err)
	}
	return path
}

func TestResolveCmd_outputsEnvJSON(t *testing.T) {
	dir := t.TempDir()
	hook := writeEchoHook(t, dir, `{"value":"s3cr3t","expires_in_sec":3600}`)
	cfgPath := writeConfig(t, dir, hook)
	envFile := writeEnvFile(t, dir, "SECRET=op://vault/item/field\n")

	// Capture stdout.
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := resolveCmd([]string{"--config", cfgPath, "--env-file", envFile})

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("resolveCmd: %v", err)
	}

	var out struct {
		Env map[string]string `json:"env"`
	}
	if err := json.NewDecoder(r).Decode(&out); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if out.Env["SECRET"] != "s3cr3t" {
		t.Errorf("want SECRET=s3cr3t, got %q", out.Env["SECRET"])
	}
}

func TestResolveEnvFile_returnsOnlyEnvFileEntries(t *testing.T) {
	dir := t.TempDir()
	hook := writeEchoHook(t, dir, `{"value":"resolved","expires_in_sec":3600}`)
	cfgPath := writeConfig(t, dir, hook)
	envFile := writeEnvFile(t, dir, "MYKEY=some-ref\n")

	// Plant a host env var that must NOT appear in the output.
	t.Setenv("HOST_ONLY_VAR", "should-not-leak")

	resolved, err := resolveEnvFile(context.Background(), cfgPath, envFile)
	if err != nil {
		t.Fatalf("resolveEnvFile: %v", err)
	}
	if _, ok := resolved["HOST_ONLY_VAR"]; ok {
		t.Error("host env var HOST_ONLY_VAR must not appear in resolved output")
	}
	if resolved["MYKEY"] != "resolved" {
		t.Errorf("want MYKEY=resolved, got %q", resolved["MYKEY"])
	}
	if len(resolved) != 1 {
		t.Errorf("want exactly 1 entry, got %d: %v", len(resolved), resolved)
	}
}

func TestMergeEnv(t *testing.T) {
	base := []string{"PATH=/usr/bin", "FOO=old", "BAR=keep"}
	resolved := map[string]string{"FOO": "new", "SECRET": "val"}
	got := mergeEnv(base, resolved)

	m := make(map[string]string)
	for _, kv := range got {
		for i, c := range kv {
			if c == '=' {
				m[kv[:i]] = kv[i+1:]
				break
			}
		}
	}
	if m["FOO"] != "new" {
		t.Errorf("FOO: want new, got %q", m["FOO"])
	}
	if m["BAR"] != "keep" {
		t.Errorf("BAR: want keep, got %q", m["BAR"])
	}
	if m["SECRET"] != "val" {
		t.Errorf("SECRET: want val, got %q", m["SECRET"])
	}
	if m["PATH"] != "/usr/bin" {
		t.Errorf("PATH: want /usr/bin, got %q", m["PATH"])
	}
}
