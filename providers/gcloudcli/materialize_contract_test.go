package gcloudcli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takezoh/credproxy/providers/gcloudcli/fakegcloud"
)

// TestMaterializeContract_WritesTokenFromFake pins the invariant that
// SpecBuilder.Materialize writes the token from the injected gcloud subprocess
// (here a fake) verbatim to the registered tokenFilePath. This is the T2
// contract test for the gcloud-cli dependency triple.
func TestMaterializeContract_WritesTokenFromFake(t *testing.T) {
	fakegcloud.Install(t, "TOKEN-CONTRACT-1")

	runDir := t.TempDir()
	tokenPath := filepath.Join(runDir, "gcloud-token")

	b := &SpecBuilder{
		rootCtx:  context.Background(),
		gcpToken: gcpPrintAccessToken,
		tokenTargets: map[string]tokenTarget{
			"/project-x": {account: "u@example.com", sa: "", tokenFilePath: tokenPath},
		},
	}

	if err := b.Materialize(context.Background(), "/project-x"); err != nil {
		t.Fatalf("Materialize err = %v, want nil", err)
	}

	got, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("read tokenFile: %v", err)
	}
	if strings.TrimSpace(string(got)) != "TOKEN-CONTRACT-1" {
		t.Errorf("token file content = %q, want TOKEN-CONTRACT-1", string(got))
	}
}

// TestMaterializeContract_UnregisteredProjectIsNoop pins the invariant that
// Materialize returns nil (not an error) when projectPath has no wiring —
// silence = healthy per adr-20260715-credproxy-runner-readonly-aggregation.
func TestMaterializeContract_UnregisteredProjectIsNoop(t *testing.T) {
	b := &SpecBuilder{
		rootCtx:      context.Background(),
		gcpToken:     gcpPrintAccessToken,
		tokenTargets: map[string]tokenTarget{},
	}
	if err := b.Materialize(context.Background(), "/unknown"); err != nil {
		t.Errorf("Materialize(/unknown) err = %v, want nil (opt-out=healthy)", err)
	}
}

// TestMaterializeContract_ErrorFromGcloudPropagates pins that a non-zero
// gcloud exit propagates as a non-nil Materialize error. The caller's retry
// envelope depends on being able to see this error.
func TestMaterializeContract_ErrorFromGcloudPropagates(t *testing.T) {
	dir := t.TempDir()
	// Fake gcloud that exits non-zero (simulates auth expiry / offline).
	script := filepath.Join(dir, "gcloud")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write failing stub: %v", err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	runDir := t.TempDir()
	tokenPath := filepath.Join(runDir, "gcloud-token")
	b := &SpecBuilder{
		rootCtx:  context.Background(),
		gcpToken: gcpPrintAccessToken,
		tokenTargets: map[string]tokenTarget{
			"/project-y": {account: "u@example.com", sa: "", tokenFilePath: tokenPath},
		},
	}
	if err := b.Materialize(context.Background(), "/project-y"); err == nil {
		t.Fatal("Materialize err = nil, want error from failing gcloud")
	}
	if _, err := os.Stat(tokenPath); !os.IsNotExist(err) {
		t.Errorf("tokenFile should not exist after failed materialize; stat err = %v", err)
	}
}

// TestMaterializeContract_NoInternalRetry pins that Materialize does NOT
// retry internally (retry ownership belongs to the caller — see
// adr-20260715-credproxy-retry-owner-caller-side). The observable is that
// a failing gcloud is invoked exactly once per Materialize call.
func TestMaterializeContract_NoInternalRetry(t *testing.T) {
	dir := t.TempDir()
	counter := filepath.Join(dir, "count")
	script := filepath.Join(dir, "gcloud")
	// Each invocation appends one 'x' to counter, then exits non-zero.
	content := "#!/bin/sh\necho x >> " + counter + "\nexit 1\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write counting stub: %v", err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	b := &SpecBuilder{
		rootCtx:  context.Background(),
		gcpToken: gcpPrintAccessToken,
		tokenTargets: map[string]tokenTarget{
			"/project-z": {account: "u@example.com", tokenFilePath: filepath.Join(t.TempDir(), "tok")},
		},
	}
	_ = b.Materialize(context.Background(), "/project-z")

	got, err := os.ReadFile(counter)
	if err != nil {
		t.Fatalf("read counter: %v", err)
	}
	// One newline-terminated 'x' means exactly one invocation.
	if strings.TrimSpace(string(got)) != "x" {
		t.Errorf("invocation count content = %q, want single 'x' (Materialize retried internally)", string(got))
	}
}
