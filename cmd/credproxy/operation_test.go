package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takezoh/credproxy/credproxy"
)

type clientOperationProvider struct{}

func (clientOperationProvider) Get(context.Context, credproxy.Request) (*credproxy.Injection, error) {
	return &credproxy.Injection{BodyReplace: []byte(`{"env":{"CTX_DATABASE_URL":"client-canary"}}`)}, nil
}
func (clientOperationProvider) Refresh(context.Context, credproxy.Request) (*credproxy.Injection, error) {
	return nil, errors.New("unused")
}

type clientRunner struct{}

func (clientRunner) Run(context.Context, string, []string, []string) error { return nil }

func startOperationBroker(t *testing.T) string {
	t.Helper()
	executable := filepath.Join(t.TempDir(), "ctx")
	socket := filepath.Join(t.TempDir(), "broker.sock")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	srv, err := credproxy.New(credproxy.ServerConfig{
		ListenUnix: socket, AllowUnauthenticated: true,
		DaemonRevision: "daemon/test-1",
		Operations: []credproxy.Operation{{
			Name: "ctx-sync", BindingRevision: ctxBindingRevision,
			ExecutablePaths: []string{executable}, Subcommand: "sync",
			Environment: map[string]string{"CTX_DATABASE_URL": "CTX_DATABASE_URL"},
			Provider:    clientOperationProvider{}, Runner: clientRunner{},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Run(ctx) }()
	return socket
}

func TestOperationCmd_returnsOnlyBoundedOutcome(t *testing.T) {
	sock := startOperationBroker(t)
	var stdout, stderr bytes.Buffer
	err := runOperationCmd([]string{
		"--socket", sock, "--route", "ctx-sync",
		"--binding-revision", ctxBindingRevision,
		"--daemon-revision", "daemon/test-1", "--",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("operation: %v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"outcome":"success"`) || strings.Contains(stdout.String(), "client-canary") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestCredrouteVersion(t *testing.T) {
	got := credrouteVersion()
	for _, component := range []string{credrouteProtocol, ctxBindingRevision, clientRevision} {
		if !strings.Contains(got, component) {
			t.Errorf("version %q missing %q", got, component)
		}
	}
}

func TestOperationCmd_failureReturnsNonzeroWithoutSecret(t *testing.T) {
	// A binding mismatch is rejected before credentials are resolved and must
	// remain a typed, bounded response plus a non-nil CLI error.
	sock := startOperationBroker(t)
	var stdout, stderr bytes.Buffer
	err := runOperationCmd([]string{
		"--socket", sock, "--route", "ctx-sync",
		"--binding-revision", "ctx-sync/1", "--daemon-revision", "daemon/test-1", "--",
	}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "binding mismatch") || stdout.Len() != 0 || strings.Contains(stdout.String(), "client-canary") {
		t.Fatalf("err=%v stdout=%q", err, stdout.String())
	}
}
