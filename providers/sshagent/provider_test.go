package sshagent

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

func newBuilder(t *testing.T, keys ...string) *SpecBuilder {
	t.Helper()
	cfg := Config{
		RunBase:         t.TempDir(),
		ContainerRunDir: "/run/credproxy",
	}
	return NewSpecBuilder(context.Background(), cfg, func(string) []string { return keys })
}

func TestSpecBuilder_emptyConfig_zeroSpec(t *testing.T) {
	spec, err := newBuilder(t).ContainerSpec(context.Background(), "/proj")
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Env) != 0 || len(spec.Mounts) != 0 {
		t.Errorf("expected zero spec, got %+v", spec)
	}
}

func TestSpecBuilder_keys_missing_file(t *testing.T) {
	if _, err := exec.LookPath("ssh-agent"); err != nil {
		t.Skip("ssh-agent not in PATH")
	}
	b := newBuilder(t, "/nonexistent/id_ed25519_missing")
	spec, err := b.ContainerSpec(context.Background(), "/proj")
	if err != nil {
		t.Skipf("ssh-agent spawn failed (sandboxed?): %v", err)
	}
	want := "/run/credproxy/agent.sock"
	if spec.Env["SSH_AUTH_SOCK"] != want {
		t.Errorf("SSH_AUTH_SOCK = %q, want %q", spec.Env["SSH_AUTH_SOCK"], want)
	}
}

func TestSpecBuilder_keys_passphrase_protected(t *testing.T) {
	if _, err := exec.LookPath("ssh-agent"); err != nil {
		t.Skip("ssh-agent not in PATH")
	}
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not in PATH")
	}

	keyDir := t.TempDir()
	keyPath := filepath.Join(keyDir, "id_ed25519_pp")
	cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-f", keyPath, "-N", "supersecret", "-q")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("ssh-keygen failed (sandboxed?): %v — %s", err, out)
	}

	b := newBuilder(t, keyPath)
	spec, err := b.ContainerSpec(context.Background(), "/proj-passphrase")
	if err != nil {
		t.Skipf("agent spawn failed (sandboxed?): %v", err)
	}
	want := "/run/credproxy/agent.sock"
	if spec.Env["SSH_AUTH_SOCK"] != want {
		t.Errorf("SSH_AUTH_SOCK = %q, want %q — agent should start even when key is passphrase-protected", spec.Env["SSH_AUTH_SOCK"], want)
	}
}
