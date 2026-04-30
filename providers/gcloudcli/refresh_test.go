package gcloudcli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func stubGcloud(t *testing.T, token string) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "gcloud")
	content := "#!/bin/sh\necho " + token + "\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write stub gcloud: %v", err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

func stubGcloudImpersonate(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "gcloud")
	content := `#!/bin/sh
for arg in "$@"; do
  case "$arg" in
    --impersonate-service-account=*) echo "sa-token"; exit 0 ;;
  esac
done
echo "user-token"
`
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write stub gcloud: %v", err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

func TestRefresher_Prime_writesToken(t *testing.T) {
	stubGcloud(t, "test-access-token-abc123")
	tokenPath := filepath.Join(t.TempDir(), "access-token")

	r := NewRefresher("user@example.com", "", tokenPath)
	if err := r.Prime(context.Background()); err != nil {
		t.Fatalf("Prime: %v", err)
	}

	data, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("read token: %v", err)
	}
	if string(data) != "test-access-token-abc123" {
		t.Errorf("token = %q, want %q", string(data), "test-access-token-abc123")
	}
}

func TestRefresher_Prime_preservesInode(t *testing.T) {
	stubGcloud(t, "fresh-token")
	tokenPath := filepath.Join(t.TempDir(), "access-token")

	if err := os.WriteFile(tokenPath, []byte("stale-token"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatal(err)
	}

	r := NewRefresher("user@example.com", "", tokenPath)
	if err := r.Prime(context.Background()); err != nil {
		t.Fatalf("Prime: %v", err)
	}

	data, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("read token: %v", err)
	}
	if string(data) != "fresh-token" {
		t.Errorf("token = %q, want %q", string(data), "fresh-token")
	}

	after, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Error("Prime replaced the file (new inode); Docker bind-mount will not see the update")
	}
}

func TestRefresher_Prime_passesImpersonateFlag(t *testing.T) {
	stubGcloudImpersonate(t)
	tokenPath := filepath.Join(t.TempDir(), "access-token")

	r := NewRefresher("user@example.com", "sa@proj.iam.gserviceaccount.com", tokenPath)
	if err := r.Prime(context.Background()); err != nil {
		t.Fatalf("Prime with SA: %v", err)
	}
	data, _ := os.ReadFile(tokenPath)
	if string(data) != "sa-token" {
		t.Errorf("with SA: token = %q, want %q", string(data), "sa-token")
	}

	tokenPathNoSA := filepath.Join(t.TempDir(), "access-token")
	r2 := NewRefresher("user@example.com", "", tokenPathNoSA)
	if err := r2.Prime(context.Background()); err != nil {
		t.Fatalf("Prime without SA: %v", err)
	}
	data2, _ := os.ReadFile(tokenPathNoSA)
	if string(data2) != "user-token" {
		t.Errorf("without SA: token = %q, want %q", string(data2), "user-token")
	}
}

func TestRefresher_Prime_failsWhenGcloudMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	tokenPath := filepath.Join(t.TempDir(), "access-token")

	r := NewRefresher("user@example.com", "", tokenPath)
	if err := r.Prime(context.Background()); err == nil {
		t.Fatal("expected error when gcloud is missing")
	}
}

func TestRefresher_Run_fsnotify_triggersRefresh(t *testing.T) {
	stubGcloud(t, "notified-token")
	credDir := t.TempDir()
	tokenPath := filepath.Join(t.TempDir(), "access-token")

	r := NewRefresher("user@example.com", "", tokenPath)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		r.runWithWatcher(ctx, credDir) //nolint:errcheck
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(credDir, "access_tokens.db"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(debounce + 500*time.Millisecond)
	for {
		select {
		case <-deadline:
			t.Fatal("token file not updated after fsnotify event")
		default:
			data, _ := os.ReadFile(tokenPath)
			if string(data) == "notified-token" {
				cancel()
				<-done
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

func TestRefresher_Run_fallbackTicker_exitOnCancel(t *testing.T) {
	stubGcloud(t, "polled-token")
	tokenPath := filepath.Join(t.TempDir(), "access-token")

	r := NewRefresher("user@example.com", "", tokenPath)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		cancel()
		r.runWithTicker(ctx)
		close(done)
	}()
	<-done
}
