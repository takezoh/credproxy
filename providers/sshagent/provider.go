// Package sshagent implements a container.Provider that injects an ephemeral
// SSH agent into containers. An ssh-agent is spawned, loaded with only the
// listed keys, and its socket is injected. The container can sign but never
// sees private keys.
package sshagent

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/takezoh/credproxy/container"
	credproxylib "github.com/takezoh/credproxy/credproxy"
)

const (
	agentReadyTimeout = 5 * time.Second
	agentReadyPoll    = 50 * time.Millisecond
)

// Config holds path configuration for the sshagent spec builder.
type Config struct {
	// RunBase is the parent of per-project run directories on the host.
	RunBase string
	// ContainerRunDir is the mount target inside the container (e.g. /run/credproxy).
	ContainerRunDir string
}

type ephemeralAgent struct {
	sockPath string
	cmd      *exec.Cmd
}

// SpecBuilder implements container.Provider for SSH agent forwarding.
type SpecBuilder struct {
	ctx     context.Context
	cfg     Config
	keysFor func(projectPath string) []string

	mu     sync.Mutex
	agents map[string]*ephemeralAgent // projectPath → agent
}

// NewSpecBuilder creates a SpecBuilder.
// keysFor returns SSH key paths for a given project path; "~/" prefixes are
// expanded by ContainerSpec.
// ctx is used to kill ephemeral agents on shutdown.
func NewSpecBuilder(ctx context.Context, cfg Config, keysFor func(string) []string) *SpecBuilder {
	b := &SpecBuilder{
		ctx:     ctx,
		cfg:     cfg,
		keysFor: keysFor,
		agents:  map[string]*ephemeralAgent{},
	}
	go b.watchShutdown(ctx)
	return b
}

func (b *SpecBuilder) Name() string { return "sshagent" }

// Init creates RunBase.
func (b *SpecBuilder) Init() error {
	return os.MkdirAll(b.cfg.RunBase, 0o700)
}

// Routes returns nil; this provider uses sockets, not HTTP routes.
func (b *SpecBuilder) Routes() []credproxylib.Route { return nil }

// ContainerSpec implements container.Provider.
// Returns zero Spec when keysFor returns no keys for projectPath.
func (b *SpecBuilder) ContainerSpec(_ context.Context, projectPath string) (container.Spec, error) {
	keys := b.keysFor(projectPath)
	if len(keys) == 0 {
		return container.Spec{}, nil
	}
	expanded := make([]string, 0, len(keys))
	for _, k := range keys {
		expanded = append(expanded, expandHome(k))
	}
	return b.keysSpec(projectPath, expanded)
}

func (b *SpecBuilder) keysSpec(projectPath string, keys []string) (container.Spec, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.agents[projectPath]; !ok {
		a, err := b.spawnAgent(projectPath, keys)
		if err != nil {
			return container.Spec{}, fmt.Errorf("sshagent: spawn agent: %w", err)
		}
		b.agents[projectPath] = a
	}
	containerSocketPath := b.cfg.ContainerRunDir + "/agent.sock"
	return container.Spec{
		Env: map[string]string{"SSH_AUTH_SOCK": containerSocketPath},
	}, nil
}

func (b *SpecBuilder) spawnAgent(projectPath string, keys []string) (*ephemeralAgent, error) {
	projectRunDir := filepath.Join(b.cfg.RunBase, container.ProjectRunHash(projectPath))
	if err := os.MkdirAll(projectRunDir, 0o700); err != nil {
		return nil, fmt.Errorf("sshagent: mkdir run dir: %w", err)
	}
	sockPath := filepath.Join(projectRunDir, "agent.sock")
	_ = os.Remove(sockPath)

	cmd := exec.CommandContext(b.ctx, "ssh-agent", "-D", "-a", sockPath)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("ssh-agent: %w", err)
	}

	if err := waitForSocket(sockPath, agentReadyTimeout, agentReadyPoll); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("ssh-agent socket not ready: %w", err)
	}

	addKeys(sockPath, keys)

	return &ephemeralAgent{sockPath: sockPath, cmd: cmd}, nil
}

func waitForSocket(path string, timeout, poll time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("unix", path, poll)
		if err == nil {
			_ = c.Close()
			return nil
		}
		time.Sleep(poll)
	}
	return fmt.Errorf("timed out after %s", timeout)
}

func addKeys(sockPath string, keys []string) {
	for _, k := range keys {
		if _, err := os.Stat(k); err != nil {
			slog.Warn("sshagent: key not found, skipping", "path", k)
			continue
		}
		cmd := exec.Command("ssh-add", k)
		cmd.Env = append(os.Environ(), "SSH_AUTH_SOCK="+sockPath)
		cmd.Stdin = nil
		if out, err := cmd.CombinedOutput(); err != nil {
			slog.Warn("sshagent: ssh-add failed (passphrase-protected?), skipping", "path", k, "out", string(out))
		}
	}
}

func (b *SpecBuilder) watchShutdown(ctx context.Context) {
	<-ctx.Done()
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, a := range b.agents {
		if a.cmd.Process != nil {
			_ = a.cmd.Process.Kill()
		}
		_ = os.Remove(a.sockPath)
	}
}

// expandHome expands a leading "~/" to the user's home directory.
func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}
