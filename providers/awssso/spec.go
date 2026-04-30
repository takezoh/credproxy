package awssso

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/takezoh/credproxy/container"
	credproxylib "github.com/takezoh/credproxy/credproxy"
)

// Config holds path configuration for the AWS SSO spec builder.
type Config struct {
	// HostRunBase is the host-side parent of per-project run directories.
	HostRunBase string
	// HostSockPath is the host-side credproxy Unix socket path.
	HostSockPath string
	// ContainerRunDir is the mount target inside the container (e.g. /run/credproxy).
	ContainerRunDir string
	// ContainerSockPath is the credproxy sock path as seen inside the container.
	ContainerSockPath string
}

// SpecBuilder implements container.Provider for AWS SSO.
type SpecBuilder struct {
	cfg         Config
	profilesFor func(projectPath string) []string
	tokenFor    func(projectPath string) (string, error)
	provider    *Provider
}

// NewSpecBuilder creates a SpecBuilder.
// profilesFor returns the AWS profile names allowed for a given project path.
// tokenFor returns (or lazily creates) the bearer token for a given project path.
func NewSpecBuilder(cfg Config, profilesFor func(string) []string, tokenFor func(string) (string, error)) *SpecBuilder {
	return &SpecBuilder{
		cfg:         cfg,
		profilesFor: profilesFor,
		tokenFor:    tokenFor,
		provider:    New(),
	}
}

func (b *SpecBuilder) Name() string { return "awssso" }

// Init creates HostRunBase. Idempotent.
func (b *SpecBuilder) Init() error {
	if err := os.MkdirAll(b.cfg.HostRunBase, 0o700); err != nil {
		return fmt.Errorf("awssso: mkdir: %w", err)
	}
	return nil
}

// Routes returns the HTTP route that serves AWS credentials to containers.
func (b *SpecBuilder) Routes() []credproxylib.Route {
	return []credproxylib.Route{
		{Path: RoutePath, Provider: b.provider},
	}
}

// ContainerSpec implements container.Provider.
// Returns zero Spec when profilesFor returns no profiles for projectPath.
func (b *SpecBuilder) ContainerSpec(_ context.Context, projectPath string) (container.Spec, error) {
	profiles := b.profilesFor(projectPath)
	if len(profiles) == 0 {
		return container.Spec{}, nil
	}

	projectID := container.ProjectRunHash(projectPath)
	b.provider.SetAllowedProfiles(projectID, profiles)

	token, err := b.tokenFor(projectPath)
	if err != nil {
		return container.Spec{}, fmt.Errorf("awssso: get token for %s: %w", projectPath, err)
	}

	projectRunDir := filepath.Join(b.cfg.HostRunBase, projectID)
	if err := os.MkdirAll(projectRunDir, 0o700); err != nil {
		return container.Spec{}, fmt.Errorf("awssso: mkdir run dir: %w", err)
	}

	if err := WriteHelperScript(filepath.Join(projectRunDir, "aws-creds.sh")); err != nil {
		return container.Spec{}, fmt.Errorf("awssso: write helper: %w", err)
	}

	var buf bytes.Buffer
	scriptPath := b.cfg.ContainerRunDir + "/aws-creds.sh"
	if err := RenderConfig(&buf, profiles, scriptPath); err != nil {
		return container.Spec{}, fmt.Errorf("awssso: render config for %s: %w", projectPath, err)
	}
	if err := os.WriteFile(filepath.Join(projectRunDir, "aws-config"), buf.Bytes(), 0o644); err != nil {
		return container.Spec{}, fmt.Errorf("awssso: write config for %s: %w", projectPath, err)
	}

	env := ContainerEnv(token, b.cfg.ContainerSockPath)
	env["AWS_CONFIG_FILE"] = b.cfg.ContainerRunDir + "/aws-config"

	mount := fmt.Sprintf("type=bind,source=%s,target=%s", b.cfg.HostSockPath, b.cfg.ContainerSockPath)
	return container.Spec{Env: env, Mounts: []string{mount}}, nil
}
