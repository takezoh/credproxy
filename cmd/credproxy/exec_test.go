package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/takezoh/credproxy/credproxy"
)

// envProvider serves a fixed body_replace payload (or a fixed error).
type envProvider struct {
	body string
	err  error
}

func (p *envProvider) Get(_ context.Context, _ credproxy.Request) (*credproxy.Injection, error) {
	if p.err != nil {
		return nil, p.err
	}
	return &credproxy.Injection{BodyReplace: []byte(p.body)}, nil
}

func (p *envProvider) Refresh(ctx context.Context, req credproxy.Request) (*credproxy.Injection, error) {
	return p.Get(ctx, req)
}

// startUnixBroker runs a credproxy server with a single "/ctx-sync" route on a
// Unix socket in a temp dir.
func startUnixBroker(t *testing.T, provider credproxy.Provider, tokens []credproxy.TokenAuth) string {
	t.Helper()
	return startUnixBrokerRoutes(t, []credproxy.Route{{Path: "/ctx-sync", Provider: provider}}, tokens)
}

func TestFetchEnv_success(t *testing.T) {
	sock := startUnixBroker(t, &envProvider{body: `{"env":{"CTX_DATABASE_URL":"postgres://u:p@h/db"}}`}, nil)
	env, err := fetchEnv(context.Background(), sock, "ctx-sync", "", 5*time.Second)
	if err != nil {
		t.Fatalf("fetchEnv: %v", err)
	}
	if env["CTX_DATABASE_URL"] != "postgres://u:p@h/db" {
		t.Errorf("env = %v", env)
	}
}

func TestFetchEnv_reasonPropagated(t *testing.T) {
	sock := startUnixBroker(t, &envProvider{err: &credproxy.ReasonError{
		Reason: "op_rate_limited",
		Err:    fmt.Errorf("detail sensitive-blob"),
	}}, nil)
	_, err := fetchEnv(context.Background(), sock, "ctx-sync", "", 5*time.Second)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "op_rate_limited") {
		t.Errorf("error should carry the reason token: %v", err)
	}
	if strings.Contains(err.Error(), "sensitive-blob") {
		t.Errorf("error must not carry server-side detail: %v", err)
	}
}

func TestFetchEnv_bearerAuth(t *testing.T) {
	tokens := []credproxy.TokenAuth{{Token: "tok-1", ID: "client-a"}}
	sock := startUnixBroker(t, &envProvider{body: `{"env":{"A":"1"}}`}, tokens)

	if _, err := fetchEnv(context.Background(), sock, "ctx-sync", "", 5*time.Second); err == nil {
		t.Error("expected auth error without token")
	}
	env, err := fetchEnv(context.Background(), sock, "ctx-sync", "tok-1", 5*time.Second)
	if err != nil {
		t.Fatalf("fetchEnv with token: %v", err)
	}
	if env["A"] != "1" {
		t.Errorf("env = %v", env)
	}
}

func TestFetchEnv_missingEnvKey(t *testing.T) {
	sock := startUnixBroker(t, &envProvider{body: `{"other":true}`}, nil)
	if _, err := fetchEnv(context.Background(), sock, "ctx-sync", "", 5*time.Second); err == nil {
		t.Error("expected error for body without env key")
	}
}

func TestValidRouteName(t *testing.T) {
	valid := []string{"ctx-sync", "xai", "env_route1"}
	invalid := []string{"", "CTX", "a/b", "a b", "../etc", "a%2fb", "ロート"}
	for _, s := range valid {
		if !validRouteName(s) {
			t.Errorf("validRouteName(%q) = false, want true", s)
		}
	}
	for _, s := range invalid {
		if validRouteName(s) {
			t.Errorf("validRouteName(%q) = true, want false", s)
		}
	}
}
