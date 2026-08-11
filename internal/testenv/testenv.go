// Package testenv reports what the surrounding environment lets a test do.
//
// Some sandboxes (notably the seccomp profile LLM agent tooling runs under)
// deny the socket(2) syscall for AF_UNIX outright. A test that needs a real
// Unix socket cannot run there at all, and letting it fail would report an
// environment restriction as a defect in the code under test. Tests use these
// probes to skip instead — always naming the error they observed, so a skip is
// never silent and can never be mistaken for coverage.
//
// Probes bind for real rather than inspect the platform: the restriction comes
// from the sandbox policy, not from the operating system.
package testenv

import (
	"net"
	"path/filepath"
	"testing"
)

// RequireUnixSocket skips t when this environment refuses AF_UNIX sockets.
func RequireUnixSocket(t *testing.T) {
	t.Helper()
	ln, err := net.Listen("unix", filepath.Join(t.TempDir(), "probe.sock"))
	if err != nil {
		t.Skipf("AF_UNIX unavailable in this environment: %v", err)
	}
	_ = ln.Close()
}
