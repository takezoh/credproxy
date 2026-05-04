package gcloudcli

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/takezoh/credproxy/container"
	credproxylib "github.com/takezoh/credproxy/credproxy"
)

const metadataListenAddr = "127.0.0.1:8181"

// GCPConfig holds per-project GCP configuration.
type GCPConfig struct {
	Account        string
	ServiceAccount string
	Active         string   // required; written to active_config
	Projects       []string // SA mode only
}

// Config holds path configuration for the gcloudcli spec builder.
type Config struct {
	// RunBase is the parent of per-project run dirs bound into containers.
	RunBase string
	// ContainerRunDir is the mount target inside the container (e.g. /run/credproxy).
	ContainerRunDir string
}

// tokenTarget holds the credentials and file path needed to refresh a project's token file.
type tokenTarget struct {
	account       string
	sa            string
	tokenFilePath string
}

// SpecBuilder implements container.Provider for the gcloud CLI.
type SpecBuilder struct {
	rootCtx  context.Context
	cfg      Config
	gcpFor   func(projectPath string) GCPConfig
	gcpToken func(ctx context.Context, account, sa string) (string, error)

	mu           sync.Mutex
	tokenTargets map[string]tokenTarget // keyed by projectPath
}

// NewSpecBuilder creates a SpecBuilder.
// gcpFor returns the GCP configuration for a given project path.
// rootCtx controls the lifetime of per-project metadata server goroutines.
func NewSpecBuilder(rootCtx context.Context, cfg Config, gcpFor func(string) GCPConfig) *SpecBuilder {
	return &SpecBuilder{
		rootCtx:      rootCtx,
		cfg:          cfg,
		gcpFor:       gcpFor,
		gcpToken:     gcpPrintAccessToken,
		tokenTargets: make(map[string]tokenTarget),
	}
}

func (b *SpecBuilder) Name() string { return "gcloudcli" }

// Init creates RunBase.
func (b *SpecBuilder) Init() error {
	if err := os.MkdirAll(b.cfg.RunBase, 0o700); err != nil {
		return fmt.Errorf("gcloudcli: mkdir runBase: %w", err)
	}
	return nil
}

// RegisterPeriodic implements container.PeriodicRegistrar.
// It registers a job that refreshes all known token files every 25 minutes.
func (b *SpecBuilder) RegisterPeriodic(srv *credproxylib.Server) {
	srv.RegisterPeriodic(credproxylib.PeriodicJob{
		Name:  "gcloudcli/token-refresh",
		Every: 25 * time.Minute,
		Run:   b.refreshAllTokens,
	})
}

func (b *SpecBuilder) refreshAllTokens(ctx context.Context) error {
	b.mu.Lock()
	targets := make([]tokenTarget, 0, len(b.tokenTargets))
	for _, t := range b.tokenTargets {
		targets = append(targets, t)
	}
	b.mu.Unlock()

	for _, t := range targets {
		if err := b.writeTokenFile(ctx, t); err != nil {
			slog.Warn("gcloudcli: token refresh failed", "path", t.tokenFilePath, "err", err)
		}
	}
	return nil
}

func (b *SpecBuilder) writeTokenFile(ctx context.Context, t tokenTarget) error {
	token, err := b.gcpToken(ctx, t.account, t.sa)
	if err != nil {
		return err
	}
	return os.WriteFile(t.tokenFilePath, []byte(token), 0o600)
}

// Routes returns nil; gcloudcli uses a per-project unix socket, not a credproxy route.
func (b *SpecBuilder) Routes() []credproxylib.Route { return nil }

func (b *SpecBuilder) metaSockContainerPath() string {
	return b.cfg.ContainerRunDir + "/gcp-metadata.sock"
}

// ContainerSpec implements container.Provider.
// Returns zero Spec when gcpFor returns no configuration for projectPath.
func (b *SpecBuilder) ContainerSpec(_ context.Context, projectPath string) (container.Spec, error) {
	gcp := b.gcpFor(projectPath)
	account := gcp.Account
	sa := gcp.ServiceAccount
	active := gcp.Active

	if account == "" && active == "" && sa == "" && len(gcp.Projects) == 0 {
		return container.Spec{}, nil
	}
	if account == "" {
		return container.Spec{}, fmt.Errorf("gcloudcli: account is required")
	}
	if active == "" {
		return container.Spec{}, fmt.Errorf("gcloudcli: active is required")
	}

	var projects []string
	if sa != "" {
		projects = gcp.Projects
		if len(projects) == 0 {
			return container.Spec{}, fmt.Errorf("gcloudcli: projects must be set when service_account is configured")
		}
	} else {
		projects = []string{active}
	}

	projectRunDir := filepath.Join(b.cfg.RunBase, container.ProjectRunHash(projectPath))
	if err := os.MkdirAll(projectRunDir, 0o700); err != nil {
		return container.Spec{}, fmt.Errorf("gcloudcli: mkdir run dir: %w", err)
	}

	tokenHostPath := filepath.Join(projectRunDir, "gcloud-token")
	tokenContainerPath := b.cfg.ContainerRunDir + "/gcloud-token"

	metaSockHost := filepath.Join(projectRunDir, "gcp-metadata.sock")
	principal := cmp.Or(sa, account)
	if err := b.ensureMetadataServer(projectPath, principal, sa, active, metaSockHost, tokenHostPath); err != nil {
		slog.Warn("gcloudcli: metadata server start failed", "err", err)
	}

	configContainerPath := b.cfg.ContainerRunDir + "/gcloud-config"
	configDir := filepath.Join(projectRunDir, "gcloud-config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return container.Spec{}, fmt.Errorf("gcloudcli: mkdir config dir: %w", err)
	}
	if err := WriteConfigDir(configDir, cmp.Or(sa, account), active, projects, tokenContainerPath); err != nil {
		return container.Spec{}, fmt.Errorf("gcloudcli: write config dir: %w", err)
	}

	env := ContainerEnv(configContainerPath)
	return container.Spec{
		Env: env,
		BridgeSpecs: []container.BridgeSpec{{
			ListenAddr:          metadataListenAddr,
			ContainerSocketPath: b.metaSockContainerPath(),
		}},
	}, nil
}

func (b *SpecBuilder) ensureMetadataServer(projectPath, account, sa, project, sockPath, tokenFilePath string) error {
	b.mu.Lock()
	if _, running := b.tokenTargets[projectPath]; running {
		b.mu.Unlock()
		return nil
	}
	if err := removeExistingSocket(sockPath); err != nil {
		b.mu.Unlock()
		return err
	}
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		b.mu.Unlock()
		return fmt.Errorf("gcloudcli: listen metadata sock: %w", err)
	}
	b.tokenTargets[projectPath] = tokenTarget{account: account, sa: sa, tokenFilePath: tokenFilePath}
	srv := &http.Server{
		Handler:      metadataHandler(account, sa, project, tokenFilePath),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 35 * time.Second,
	}
	go func() {
		<-b.rootCtx.Done()
		ln.Close()
	}()
	go func() {
		if err := srv.Serve(ln); err != nil && b.rootCtx.Err() == nil {
			slog.Warn("gcloudcli: metadata server error", "project", projectPath, "err", err)
		}
	}()
	slog.Debug("gcloudcli: metadata server started", "project", projectPath, "sock", sockPath)
	b.mu.Unlock()

	// Pre-populate token file outside the lock; gcloud exec may take hundreds of ms.
	target := tokenTarget{account: account, sa: sa, tokenFilePath: tokenFilePath}
	if err := b.writeTokenFile(b.rootCtx, target); err != nil {
		slog.Warn("gcloudcli: initial token write failed", "err", err)
	}
	return nil
}

func removeExistingSocket(path string) error {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("gcloudcli: %s exists and is not a socket", path)
	}
	return os.Remove(path)
}
