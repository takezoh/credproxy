package awssso

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/takezoh/credproxy/container"
)

func newTestConfig(t *testing.T) (Config, string) {
	t.Helper()
	runBase := t.TempDir()
	sockPath := filepath.Join(runBase, "credproxy.sock")
	cfg := Config{
		HostRunBase:       runBase,
		HostSockPath:      sockPath,
		ContainerRunDir:   "/run/credproxy",
		ContainerSockPath: "/run/credproxy/credproxy.sock",
	}
	return cfg, sockPath
}

func TestSpecBuilder_emptyProfiles_zeroSpec(t *testing.T) {
	cfg, _ := newTestConfig(t)
	b := NewSpecBuilder(cfg,
		func(string) []string { return nil },
		func(string) (string, error) { return "tok", nil },
	)
	spec, err := b.ContainerSpec(context.Background(), "/proj")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(spec.Env) != 0 || len(spec.Mounts) != 0 {
		t.Errorf("expected zero spec, got env=%v mounts=%v", spec.Env, spec.Mounts)
	}
}

func TestSpecBuilder_withProfiles_returnsEnvAndFiles(t *testing.T) {
	withFakeAWS(t)
	cfg, sockPath := newTestConfig(t)
	profiles := []string{"default", "prod"}
	b := NewSpecBuilder(cfg,
		func(string) []string { return profiles },
		func(string) (string, error) { return "mytoken", nil },
	)

	spec, err := b.ContainerSpec(context.Background(), "/myproject")
	if err != nil {
		t.Fatalf("ContainerSpec: %v", err)
	}

	if spec.Env[EnvKeyToken] != "mytoken" {
		t.Errorf("%s = %q, want %q", EnvKeyToken, spec.Env[EnvKeyToken], "mytoken")
	}
	if spec.Env[EnvKeySock] != cfg.ContainerSockPath {
		t.Errorf("%s = %q, want %q", EnvKeySock, spec.Env[EnvKeySock], cfg.ContainerSockPath)
	}
	if spec.Env["AWS_CONFIG_FILE"] != "/run/credproxy/aws-config" {
		t.Errorf("AWS_CONFIG_FILE = %q, want /run/credproxy/aws-config", spec.Env["AWS_CONFIG_FILE"])
	}
	wantMount := "type=bind,source=" + sockPath + ",target=" + cfg.ContainerSockPath
	if len(spec.Mounts) != 1 || spec.Mounts[0] != wantMount {
		t.Errorf("Mounts = %v, want [%q]", spec.Mounts, wantMount)
	}

	projectDir := filepath.Join(cfg.HostRunBase, container.ProjectRunHash("/myproject"))
	if _, err := os.Stat(filepath.Join(projectDir, "aws-config")); err != nil {
		t.Errorf("aws-config not created in run dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "aws-creds.sh")); err != nil {
		t.Errorf("aws-creds.sh not created in run dir: %v", err)
	}

	projectID := container.ProjectRunHash("/myproject")
	provider := b.Routes()[0].Provider.(*Provider)
	_, err = provider.Get(context.Background(), req(projectID, "/prod"))
	if err != nil {
		t.Errorf("provider.Get(/prod): unexpected error: %v", err)
	}
	_, err = provider.Get(context.Background(), req(projectID, "/"))
	if err != nil {
		t.Errorf("provider.Get(/): unexpected error (default should be allowed): %v", err)
	}
	_, err = provider.Get(context.Background(), req(projectID, "/other"))
	if err == nil {
		t.Errorf("provider.Get(/other): expected error for unlisted profile, got nil")
	}
}

func TestSpecBuilder_NoProfiles_provider_remains_strict(t *testing.T) {
	cfg, _ := newTestConfig(t)
	b := NewSpecBuilder(cfg,
		func(string) []string { return nil },
		func(string) (string, error) { return "tok", nil },
	)

	provider := b.Routes()[0].Provider.(*Provider)
	_, err := provider.Get(context.Background(), req("unknown-project", "/master"))
	if err == nil {
		t.Errorf("expected rejection for unlisted profile on empty allowlist, got nil")
	}
}
