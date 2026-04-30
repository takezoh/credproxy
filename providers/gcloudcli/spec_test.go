package gcloudcli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/takezoh/credproxy/container"
)

func newTestConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		GCPDir:          t.TempDir(),
		RunBase:         t.TempDir(),
		ContainerRunDir: "/run/credproxy",
	}
}

func emptyGCPConfig(string) GCPConfig { return GCPConfig{} }

func TestSpecBuilder_emptyConfig_zeroSpec(t *testing.T) {
	cfg := newTestConfig(t)
	b := NewSpecBuilder(context.Background(), cfg, emptyGCPConfig)
	spec, err := b.ContainerSpec(context.Background(), "/proj")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(spec.Env) != 0 || len(spec.Mounts) != 0 {
		t.Errorf("expected zero spec, got env=%v mounts=%v", spec.Env, spec.Mounts)
	}
}

func TestSpecBuilder_accountOnly_returnsError(t *testing.T) {
	cfg := newTestConfig(t)
	b := NewSpecBuilder(context.Background(), cfg, func(string) GCPConfig {
		return GCPConfig{Account: "user@example.com", Projects: []string{"p"}}
	})
	_, err := b.ContainerSpec(context.Background(), "/proj")
	if err == nil {
		t.Fatal("expected error for account-only config without service_account, got nil")
	}
}

func TestSpecBuilder_missingServiceAccount_projectsOnly_returnsError(t *testing.T) {
	cfg := newTestConfig(t)
	b := NewSpecBuilder(context.Background(), cfg, func(string) GCPConfig {
		return GCPConfig{Projects: []string{"p"}}
	})
	_, err := b.ContainerSpec(context.Background(), "/proj")
	if err == nil {
		t.Fatal("expected error when service_account is missing")
	}
}

func TestSpecBuilder_withConfig_injectsEnvAndFiles(t *testing.T) {
	stubGcloudForSpec(t, "gcp-test-token")
	cfg := newTestConfig(t)
	b := NewSpecBuilder(context.Background(), cfg, func(string) GCPConfig {
		return GCPConfig{
			ServiceAccount: "sa@proj.iam.gserviceaccount.com",
			Account:        "user@example.com",
			Projects:       []string{"proj-a", "proj-b"},
		}
	})

	spec, err := b.ContainerSpec(context.Background(), "/myproject")
	if err != nil {
		t.Fatalf("ContainerSpec: %v", err)
	}

	wantConfigPath := cfg.ContainerRunDir + "/gcloud-config"
	if spec.Env[ConfigDirEnv] != wantConfigPath {
		t.Errorf("env[%s] = %q, want %q", ConfigDirEnv, spec.Env[ConfigDirEnv], wantConfigPath)
	}
	if len(spec.Mounts) != 0 {
		t.Errorf("expected 0 mounts, got %d: %v", len(spec.Mounts), spec.Mounts)
	}

	projectDir := filepath.Join(cfg.RunBase, container.ProjectRunHash("/myproject"))
	if _, err := os.Stat(filepath.Join(projectDir, "gcloud-config")); err != nil {
		t.Errorf("gcloud-config dir not created in run dir: %v", err)
	}
}

func TestSpecBuilder_refresherDeduplication(t *testing.T) {
	stubGcloudForSpec(t, "tok")
	cfg := newTestConfig(t)
	gcpCfg := GCPConfig{
		ServiceAccount: "sa@proj.iam.gserviceaccount.com",
		Account:        "user@example.com",
		Projects:       []string{"p"},
	}
	b := NewSpecBuilder(context.Background(), cfg, func(string) GCPConfig { return gcpCfg })

	if _, err := b.ContainerSpec(context.Background(), "/p1"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ContainerSpec(context.Background(), "/p2"); err != nil {
		t.Fatal(err)
	}

	b.mu.Lock()
	count := len(b.refreshers)
	b.mu.Unlock()

	if count != 1 {
		t.Errorf("expected 1 refresher for same SA, got %d", count)
	}
}

func TestSpecBuilder_refresherIsolationByServiceAccount(t *testing.T) {
	stubGcloudForSpec(t, "tok")
	cfg := newTestConfig(t)

	gcpCfg1 := GCPConfig{ServiceAccount: "sa-a@proj.iam.gserviceaccount.com", Account: "u@e.com", Projects: []string{"p"}}
	gcpCfg2 := GCPConfig{ServiceAccount: "sa-b@proj.iam.gserviceaccount.com", Account: "u@e.com", Projects: []string{"p"}}

	calls := 0
	b := NewSpecBuilder(context.Background(), cfg, func(p string) GCPConfig {
		calls++
		if p == "/p1" {
			return gcpCfg1
		}
		return gcpCfg2
	})

	if _, err := b.ContainerSpec(context.Background(), "/p1"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ContainerSpec(context.Background(), "/p2"); err != nil {
		t.Fatal(err)
	}

	b.mu.Lock()
	count := len(b.refreshers)
	b.mu.Unlock()

	if count != 2 {
		t.Errorf("expected 2 refreshers for different SAs, got %d", count)
	}
}

func stubGcloudForSpec(t *testing.T, token string) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "gcloud")
	content := "#!/bin/sh\necho " + token + "\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write stub gcloud: %v", err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}
