// Package fakegcloud provides a PATH-injected fake gcloud binary for tests.
// It is the T0/T1 seam substitute for the real gcloud CLI: `gcloud auth
// print-access-token` and `gcloud auth application-default print-access-token`
// both resolve to a shell script that prints the token supplied at install time.
// The real gcloud invocation surface in providers/gcloudcli/metadata.go
// (gcpPrintAccessToken -> exec.CommandContext("gcloud", ...)) is the injection
// point: replacing PATH with a directory that contains this fake yields
// deterministic tokens without any host-side gcloud install or credentials.
//
// This fake ships alongside a contract test (Materialize invariants) and a
// //go:build e2e FakeVsReal fidelity test in the parent package, per the
// project's external-dependency testing rule.
package fakegcloud

import (
	"os"
	"path/filepath"
	"testing"
)

// Install writes a fake gcloud script to a fresh temp dir and prepends that
// dir to PATH for the duration of the test. Subsequent `gcloud auth
// print-access-token[...]` invocations return token verbatim. Cleanup is
// automatic via t.TempDir() and t.Setenv().
//
// The script ignores all arguments and prints token followed by \n.
// Both `gcloud auth print-access-token` and `gcloud auth application-default
// print-access-token` route to the same stub — the caller does not
// distinguish, they both consume stdout.
func Install(t *testing.T, token string) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "gcloud")
	content := "#!/bin/sh\necho " + token + "\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("fakegcloud: write stub: %v", err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}
