package gcloudcli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteConfigDir_createsFilesPerProject(t *testing.T) {
	dir := t.TempDir()
	tokenPath := "/run/credproxy/gcloud-token"
	err := WriteConfigDir(dir, "user@example.com", "proj-a", []string{"proj-a", "proj-b"}, tokenPath)
	if err != nil {
		t.Fatalf("WriteConfigDir: %v", err)
	}

	for _, proj := range []string{"proj-a", "proj-b"} {
		path := filepath.Join(dir, "configurations", "config_"+proj)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		content := string(data)
		if !strings.Contains(content, "access_token_file") {
			t.Errorf("%s: missing access_token_file line", proj)
		}
		if !strings.Contains(content, tokenPath) {
			t.Errorf("%s: missing token path %q", proj, tokenPath)
		}
		if !strings.Contains(content, "user@example.com") {
			t.Errorf("%s: missing account line", proj)
		}
		if !strings.Contains(content, proj) {
			t.Errorf("%s: missing project line", proj)
		}
	}

	active, err := os.ReadFile(filepath.Join(dir, "active_config"))
	if err != nil {
		t.Fatalf("read active_config: %v", err)
	}
	if string(active) != "proj-a" {
		t.Errorf("active_config = %q, want %q", string(active), "proj-a")
	}
}

func TestWriteConfigDir_activeIndependentOfProjectsOrder(t *testing.T) {
	dir := t.TempDir()
	err := WriteConfigDir(dir, "user@example.com", "proj-b", []string{"proj-a", "proj-b"}, "")
	if err != nil {
		t.Fatalf("WriteConfigDir: %v", err)
	}
	active, err := os.ReadFile(filepath.Join(dir, "active_config"))
	if err != nil {
		t.Fatalf("read active_config: %v", err)
	}
	if string(active) != "proj-b" {
		t.Errorf("active_config = %q, want %q", string(active), "proj-b")
	}
}

func TestWriteConfigDir_rejectsInvalidProjectID(t *testing.T) {
	dir := t.TempDir()
	if err := WriteConfigDir(dir, "user@example.com", "ok-project", []string{"bad project!"}, ""); err == nil {
		t.Fatal("expected error for invalid project id")
	}
}

func TestWriteConfigDir_rejectsEmptyProjectID(t *testing.T) {
	dir := t.TempDir()
	if err := WriteConfigDir(dir, "user@example.com", "p", []string{""}, ""); err == nil {
		t.Fatal("expected error for empty project id")
	}
}

func TestContainerEnv_containsCloudsdkConfig(t *testing.T) {
	configPath := "/opt/run/gcloud-config"
	env := ContainerEnv(configPath)
	if env[ConfigDirEnv] != configPath {
		t.Errorf("ContainerEnv()[%q] = %q, want %q", ConfigDirEnv, env[ConfigDirEnv], configPath)
	}
	if env[MetadataHostEnv] != metadataListenAddr {
		t.Errorf("ContainerEnv()[%q] = %q, want %q", MetadataHostEnv, env[MetadataHostEnv], metadataListenAddr)
	}
	if env[MetadataIPEnv] != "127.0.0.1" {
		t.Errorf("ContainerEnv()[%q] = %q, want %q", MetadataIPEnv, env[MetadataIPEnv], "127.0.0.1")
	}
}
