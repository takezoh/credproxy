package gcloudcli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestSpecBuilder_accountOnly_noAllowFlag_returnsError(t *testing.T) {
	cfg := newTestConfig(t)
	b := NewSpecBuilder(context.Background(), cfg, func(string) GCPConfig {
		return GCPConfig{Account: "user@example.com", Projects: []string{"p"}}
	})
	_, err := b.ContainerSpec(context.Background(), "/proj")
	if err == nil {
		t.Fatal("expected error for account-only config without allow_user_account, got nil")
	}
}

func TestSpecBuilder_missingServiceAccount_projectsOnly_returnsError(t *testing.T) {
	cfg := newTestConfig(t)
	b := NewSpecBuilder(context.Background(), cfg, func(string) GCPConfig {
		return GCPConfig{Projects: []string{"p"}}
	})
	_, err := b.ContainerSpec(context.Background(), "/proj")
	if err == nil {
		t.Fatal("expected error when service_account is missing and allow_user_account is false")
	}
}

func TestSpecBuilder_userAccountProxy_injectsEnvAndFiles(t *testing.T) {
	stubGcloudForSpec(t, "user-token")
	cfg := newTestConfig(t)
	b := NewSpecBuilder(context.Background(), cfg, func(string) GCPConfig {
		return GCPConfig{
			Account:          "user@example.com",
			Projects:         []string{"proj-x"},
			EnableUserAccount: true,
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

	projectDir := filepath.Join(cfg.RunBase, container.ProjectRunHash("/myproject"))
	if _, err := os.Stat(filepath.Join(projectDir, "gcloud-config")); err != nil {
		t.Errorf("gcloud-config dir not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "gcloud-token")); err != nil {
		t.Errorf("gcloud-token not created: %v", err)
	}
}

func TestSpecBuilder_userAccountProxy_configContainsUserAccount(t *testing.T) {
	stubGcloudForSpec(t, "user-token")
	cfg := newTestConfig(t)
	b := NewSpecBuilder(context.Background(), cfg, func(string) GCPConfig {
		return GCPConfig{
			Account:          "user@example.com",
			Projects:         []string{"proj-x"},
			EnableUserAccount: true,
		}
	})

	if _, err := b.ContainerSpec(context.Background(), "/myproject"); err != nil {
		t.Fatalf("ContainerSpec: %v", err)
	}

	projectDir := filepath.Join(cfg.RunBase, container.ProjectRunHash("/myproject"))
	configFile := filepath.Join(projectDir, "gcloud-config", "configurations", "config_proj-x")
	data, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	if !strings.Contains(string(data), "user@example.com") {
		t.Errorf("config file does not contain user account; content:\n%s", data)
	}
}

func TestSpecBuilder_userAccountProxy_noProjects_returnsError(t *testing.T) {
	cfg := newTestConfig(t)
	b := NewSpecBuilder(context.Background(), cfg, func(string) GCPConfig {
		return GCPConfig{Account: "user@example.com", EnableUserAccount: true}
	})
	_, err := b.ContainerSpec(context.Background(), "/proj")
	if err == nil {
		t.Fatal("expected error when projects is empty, got nil")
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
