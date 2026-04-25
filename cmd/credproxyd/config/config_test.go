package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takezoh/credproxy/cmd/credproxyd/config"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad_minimal(t *testing.T) {
	path := writeConfig(t, `
listen_tcp = "127.0.0.1:9787"

[[route]]
path = "/test"
upstream = "https://example.com"
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ListenTCP != "127.0.0.1:9787" {
		t.Errorf("ListenTCP = %q", cfg.ListenTCP)
	}
	if len(cfg.Routes) != 1 {
		t.Fatalf("Routes len = %d", len(cfg.Routes))
	}
	if cfg.Routes[0].HookTimeoutSec != 10 {
		t.Errorf("default HookTimeoutSec = %d, want 10", cfg.Routes[0].HookTimeoutSec)
	}
}

func TestLoad_noListeners(t *testing.T) {
	path := writeConfig(t, `
[[route]]
path = "/test"
upstream = "https://example.com"
`)
	if _, err := config.Load(path); err == nil {
		t.Error("expected error for no listeners")
	}
}

func TestLoad_missingPath(t *testing.T) {
	path := writeConfig(t, `
listen_tcp = "127.0.0.1:9787"

[[route]]
upstream = "https://example.com"
`)
	if _, err := config.Load(path); err == nil {
		t.Error("expected error for missing route path")
	}
}

func TestLoadTokens(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "tokens")
	if err := os.WriteFile(tokenFile, []byte("tok1\ntok2\n  tok3  \n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tokens, err := config.LoadTokens(tokenFile)
	if err != nil {
		t.Fatalf("LoadTokens: %v", err)
	}
	if len(tokens) != 3 {
		t.Errorf("got %d tokens, want 3: %v", len(tokens), tokens)
	}
	if tokens[2] != "tok3" {
		t.Errorf("token[2] = %q, want trimmed", tokens[2])
	}
}

func TestLoad_envExpansion(t *testing.T) {
	t.Setenv("TEST_PORT", "9999")
	path := writeConfig(t, `
listen_tcp = "127.0.0.1:${TEST_PORT}"

[[route]]
path = "/test"
upstream = "https://example.com"
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.HasSuffix(cfg.ListenTCP, "9999") {
		t.Errorf("env not expanded: %q", cfg.ListenTCP)
	}
}
