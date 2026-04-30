package gcloudcli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/takezoh/credproxy/container"
	credproxylib "github.com/takezoh/credproxy/credproxy"
)

// GCPConfig holds per-project GCP configuration.
type GCPConfig struct {
	Account        string
	ServiceAccount string
	Projects       []string
}

// Config holds path configuration for the gcloudcli spec builder.
type Config struct {
	// GCPDir stores per-account token files refreshed by background goroutines.
	GCPDir string
	// RunBase is the parent of per-project run dirs bound into containers.
	RunBase string
	// ContainerRunDir is the mount target inside the container (e.g. /run/credproxy).
	ContainerRunDir string
}

// SpecBuilder implements container.Provider for the gcloud CLI.
type SpecBuilder struct {
	rootCtx context.Context
	cfg     Config
	gcpFor  func(projectPath string) GCPConfig

	mu         sync.Mutex
	refreshers map[string]*Refresher // keyed by principalKey(account, sa)
}

// NewSpecBuilder creates a SpecBuilder.
// gcpFor returns the GCP configuration for a given project path.
// rootCtx controls the lifetime of token refresh goroutines.
func NewSpecBuilder(rootCtx context.Context, cfg Config, gcpFor func(string) GCPConfig) *SpecBuilder {
	return &SpecBuilder{
		rootCtx:    rootCtx,
		cfg:        cfg,
		gcpFor:     gcpFor,
		refreshers: make(map[string]*Refresher),
	}
}

func (b *SpecBuilder) Name() string { return "gcloudcli" }

// Init creates GCPDir and RunBase.
func (b *SpecBuilder) Init() error {
	if err := os.MkdirAll(b.cfg.GCPDir, 0o755); err != nil {
		return fmt.Errorf("gcloudcli: mkdir: %w", err)
	}
	if err := os.MkdirAll(b.cfg.RunBase, 0o700); err != nil {
		return fmt.Errorf("gcloudcli: mkdir runBase: %w", err)
	}
	return nil
}

// Routes returns nil; gcloudcli uses bind-mounted files, not an HTTP route.
func (b *SpecBuilder) Routes() []credproxylib.Route { return nil }

// ContainerSpec implements container.Provider.
// Returns zero Spec when gcpFor returns no configuration for projectPath.
func (b *SpecBuilder) ContainerSpec(ctx context.Context, projectPath string) (container.Spec, error) {
	gcp := b.gcpFor(projectPath)
	account := gcp.Account
	sa := gcp.ServiceAccount
	projects := gcp.Projects

	if sa == "" && account == "" && len(projects) == 0 {
		return container.Spec{}, nil
	}
	if sa == "" || len(projects) == 0 {
		return container.Spec{}, fmt.Errorf("gcloudcli: service_account and projects must both be set; full-scope account tokens are not supported")
	}

	tokenSrc, err := b.ensureRefresher(ctx, account, sa)
	if err != nil {
		return container.Spec{}, err
	}

	projectRunDir := filepath.Join(b.cfg.RunBase, container.ProjectRunHash(projectPath))
	if err := os.MkdirAll(projectRunDir, 0o700); err != nil {
		return container.Spec{}, fmt.Errorf("gcloudcli: mkdir run dir: %w", err)
	}

	tokenContainerPath := b.cfg.ContainerRunDir + "/gcloud-token"
	configContainerPath := b.cfg.ContainerRunDir + "/gcloud-config"

	if err := linkToken(tokenSrc, filepath.Join(projectRunDir, "gcloud-token")); err != nil {
		slog.Warn("gcloudcli: token link failed, skipping", "err", err)
	}

	configDir := filepath.Join(projectRunDir, "gcloud-config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return container.Spec{}, fmt.Errorf("gcloudcli: mkdir config dir: %w", err)
	}
	if err := WriteConfigDir(configDir, sa, projects, tokenContainerPath); err != nil {
		return container.Spec{}, fmt.Errorf("gcloudcli: write config dir: %w", err)
	}

	return container.Spec{Env: ContainerEnv(configContainerPath)}, nil
}

func (b *SpecBuilder) ensureRefresher(ctx context.Context, account, sa string) (string, error) {
	key := principalKey(account, sa)
	principalDir := filepath.Join(b.cfg.GCPDir, principalHash(key))
	if err := os.MkdirAll(principalDir, 0o755); err != nil {
		return "", fmt.Errorf("gcloudcli: mkdir principal dir: %w", err)
	}
	tokenPath := filepath.Join(principalDir, "access-token")

	b.mu.Lock()
	defer b.mu.Unlock()

	if _, running := b.refreshers[key]; !running {
		ref := NewRefresher(account, sa, tokenPath)
		if err := ref.Prime(ctx); err != nil {
			slog.Warn("gcloudcli: initial token fetch failed", "account", account, "sa", sa, "err", err)
		}
		b.refreshers[key] = ref
		go ref.Run(b.rootCtx)
	}

	return tokenPath, nil
}

func linkToken(tokenSrc, dst string) error {
	if _, err := os.Stat(tokenSrc); err != nil {
		return nil
	}
	_ = os.Remove(dst)
	return os.Link(tokenSrc, dst)
}

func principalKey(account, sa string) string { return account + "|" + sa }

func principalHash(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:4])
}
