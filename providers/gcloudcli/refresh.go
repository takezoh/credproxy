package gcloudcli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	fallbackPeriod = 5 * time.Minute
	debounce       = 2 * time.Second
)

// Refresher keeps a token file populated with a fresh GCP access token.
// It watches the host gcloud credential store with fsnotify and calls
// gcloud auth print-access-token immediately after each internal refresh,
// falling back to a 5-minute polling ticker when the watch is unavailable.
type Refresher struct {
	account        string
	serviceAccount string
	tokenPath      string
}

// NewRefresher creates a Refresher that keeps tokenPath populated.
// account is the host gcloud principal (may be empty to use gcloud's default).
// serviceAccount is the SA email to impersonate; must be non-empty for scope-limited tokens.
func NewRefresher(account, serviceAccount, tokenPath string) *Refresher {
	return &Refresher{account: account, serviceAccount: serviceAccount, tokenPath: tokenPath}
}

// Prime fetches the token once synchronously.
func (r *Refresher) Prime(ctx context.Context) error {
	return r.refresh(ctx)
}

// Run starts the refresh loop, preferring fsnotify over polling.
func (r *Refresher) Run(ctx context.Context) {
	credDir := gcloudCredentialDir()
	if credDir != "" {
		if err := r.runWithWatcher(ctx, credDir); err == nil {
			return
		}
	}
	r.runWithTicker(ctx)
}

func (r *Refresher) runWithWatcher(ctx context.Context, credDir string) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	if err := watcher.Add(credDir); err != nil {
		return fmt.Errorf("watch %s: %w", credDir, err)
	}
	slog.Debug("gcloudcli: watching credential dir", "dir", credDir)

	var debounceTimer *time.Timer
	defer func() {
		if debounceTimer != nil {
			debounceTimer.Stop()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-watcher.Events:
			if !ok {
				return fmt.Errorf("watcher closed")
			}
			if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) {
				continue
			}
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(debounce, func() {
				if err := r.refresh(ctx); err != nil {
					slog.Warn("gcloudcli: token refresh failed", "account", r.account, "err", err)
				}
			})
		case err, ok := <-watcher.Errors:
			if !ok {
				return fmt.Errorf("watcher error channel closed")
			}
			slog.Warn("gcloudcli: fsnotify error", "err", err)
		}
	}
}

func (r *Refresher) runWithTicker(ctx context.Context) {
	slog.Debug("gcloudcli: falling back to polling", "account", r.account, "period", fallbackPeriod)
	ticker := time.NewTicker(fallbackPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.refresh(ctx); err != nil {
				slog.Warn("gcloudcli: token refresh failed", "account", r.account, "err", err)
			}
		}
	}
}

func (r *Refresher) refresh(ctx context.Context) error {
	token, err := printAccessToken(ctx, r.account, r.serviceAccount)
	if err != nil {
		return fmt.Errorf("gcloudcli: token refresh (account=%s sa=%s): %w", r.account, r.serviceAccount, err)
	}
	if err := writeToken(r.tokenPath, []byte(token)); err != nil {
		return fmt.Errorf("write token: %w", err)
	}
	slog.Debug("gcloudcli: token refreshed", "account", r.account)
	return nil
}

func printAccessToken(ctx context.Context, account, serviceAccount string) (string, error) {
	args := []string{"auth", "print-access-token"}
	if account != "" {
		args = append(args, "--account="+account)
	}
	if serviceAccount != "" {
		args = append(args, "--impersonate-service-account="+serviceAccount)
	}
	out, err := exec.CommandContext(ctx, "gcloud", args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// writeToken writes data to path in-place, preserving the inode so that Docker
// bind-mount consumers see the updated content. Atomic rename would create a new
// inode, leaving the bind-mounted file pointing at the old one.
func writeToken(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}

func gcloudCredentialDir() string {
	if dir := os.Getenv("CLOUDSDK_CONFIG"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, ".config", "gcloud")
	if _, err := os.Stat(dir); err != nil {
		return ""
	}
	return dir
}
