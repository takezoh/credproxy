//go:build e2e

// FakeVsReal fidelity test for the gcloud-cli dependency triple.
// Opt-in: set AG_E2E_GCLOUD_BIN=/path/to/real/gcloud (or leave empty to skip).
// The real gcloud requires host-side auth (either gcloud auth login for user
// mode, or gcloud auth application-default login / GOOGLE_APPLICATION_CREDENTIALS
// for ADC). The test does not perform auth setup — the operator does that
// out-of-band and points AG_E2E_GCLOUD_BIN at the resulting binary.
package gcloudcli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takezoh/credproxy/providers/gcloudcli/fakegcloud"
)

// realGcloudPath returns the real gcloud binary path or "" if the operator
// did not opt in.
func realGcloudPath(t *testing.T) string {
	p := os.Getenv("AG_E2E_GCLOUD_BIN")
	if p == "" {
		t.Skip("AG_E2E_GCLOUD_BIN not set; skipping real-gcloud fidelity test")
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("AG_E2E_GCLOUD_BIN=%q: stat: %v", p, err)
	}
	return p
}

// TestE2E_FakeVsRealMaterialize asserts that both the fake gcloud (installed
// via fakegcloud.Install) and the real gcloud produce the SAME observable
// outcome under Materialize: a non-empty token file at tokenFilePath. Token
// contents differ (fake returns the fixed marker; real returns a live
// OAuth token) so we only compare shape invariants, not payload.
func TestE2E_FakeVsRealMaterialize(t *testing.T) {
	realBin := realGcloudPath(t)

	// --- Fake half ---
	fakegcloud.Install(t, "FAKE-FIDELITY-1")
	fakeRunDir := t.TempDir()
	fakeToken := filepath.Join(fakeRunDir, "gcloud-token")
	fakeBuilder := &SpecBuilder{
		rootCtx:  context.Background(),
		gcpToken: gcpPrintAccessToken,
		tokenTargets: map[string]tokenTarget{
			"/p": {account: "u@example.com", tokenFilePath: fakeToken},
		},
	}
	if err := fakeBuilder.Materialize(context.Background(), "/p"); err != nil {
		t.Fatalf("fake Materialize err = %v", err)
	}
	fakeBytes, err := os.ReadFile(fakeToken)
	if err != nil {
		t.Fatalf("fake read: %v", err)
	}
	fakeToken1 := strings.TrimSpace(string(fakeBytes))

	// --- Real half ---
	// Point PATH at only the real binary's directory so exec.LookPath("gcloud")
	// resolves to it. Assumes the real gcloud's runtime dependencies live in
	// standard system paths.
	realDir := filepath.Dir(realBin)
	realLink := filepath.Join(t.TempDir(), "gcloud")
	if err := os.Symlink(realBin, realLink); err != nil {
		t.Fatalf("symlink real gcloud: %v", err)
	}
	t.Setenv("PATH", filepath.Dir(realLink)+":"+realDir+":"+os.Getenv("PATH"))

	// Preflight: skip if operator's real gcloud lacks working auth.
	if err := exec.CommandContext(context.Background(), "gcloud", "auth", "application-default", "print-access-token").Run(); err != nil {
		t.Skipf("real gcloud auth not usable (application-default print-access-token failed): %v; run `gcloud auth application-default login` and re-run", err)
	}

	realRunDir := t.TempDir()
	realTokenPath := filepath.Join(realRunDir, "gcloud-token")
	realBuilder := &SpecBuilder{
		rootCtx:  context.Background(),
		gcpToken: gcpPrintAccessToken,
		tokenTargets: map[string]tokenTarget{
			"/p": {account: "", sa: "", tokenFilePath: realTokenPath},
		},
	}
	if err := realBuilder.Materialize(context.Background(), "/p"); err != nil {
		t.Fatalf("real Materialize err = %v", err)
	}
	realBytes, err := os.ReadFile(realTokenPath)
	if err != nil {
		t.Fatalf("real read: %v", err)
	}
	realToken1 := strings.TrimSpace(string(realBytes))

	// Fidelity assertion: both produce non-empty tokens, both files at 0o600.
	// (Payload equality is impossible — fake is deterministic, real is dynamic.)
	if fakeToken1 == "" {
		t.Error("fake produced empty token")
	}
	if realToken1 == "" {
		t.Error("real produced empty token")
	}
	if info, err := os.Stat(fakeToken); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("fake token file mode = %v, want 0o600", info.Mode().Perm())
	}
	if info, err := os.Stat(realTokenPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("real token file mode = %v, want 0o600", info.Mode().Perm())
	}
}
