// Package gcloudcli provides credential isolation for the gcloud CLI in Docker containers.
//
// Instead of bind-mounting ~/.config/gcloud, this provider writes a synthetic CLOUDSDK_CONFIG
// directory containing per-project gcloud configurations. Each configuration uses
// auth/access_token_file pointing to a host-refreshed token file.
// Containers receive only short-lived access tokens (≤1h TTL).
package gcloudcli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ConfigDirEnv is the gcloud SDK environment variable that overrides ~/.config/gcloud.
const ConfigDirEnv = "CLOUDSDK_CONFIG"

// OAuthAccessTokenEnv is the env var gcloud (all versions) checks for a raw access token.
const OAuthAccessTokenEnv = "GOOGLE_OAUTH_ACCESS_TOKEN"

const globalProperties = "[core]\ndisable_usage_reporting = true\n\n[component_manager]\ndisable_update_check = true\n"

// WriteConfigDir materializes a synthetic CLOUDSDK_CONFIG directory at dir.
// One gcloud configuration named after each project ID is written. active names
// the configuration that becomes the active default (written to active_config).
// Each configuration sets auth/access_token_file to tokenContainerPath.
func WriteConfigDir(dir, account, active string, projects []string, tokenContainerPath string) error {
	configsDir := filepath.Join(dir, "configurations")
	if err := os.MkdirAll(configsDir, 0o755); err != nil {
		return fmt.Errorf("gcloudcli: mkdir configurations: %w", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "properties"), []byte(globalProperties), 0o644); err != nil {
		return fmt.Errorf("gcloudcli: write properties: %w", err)
	}

	for _, proj := range projects {
		if err := validateProjectID(proj); err != nil {
			return fmt.Errorf("gcloudcli: invalid project id %q: %w", proj, err)
		}
		path := filepath.Join(configsDir, "config_"+proj)
		if err := writeConfigFile(path, account, proj, tokenContainerPath); err != nil {
			return err
		}
	}

	activeConfig := filepath.Join(dir, "active_config")
	if err := os.WriteFile(activeConfig, []byte(active), 0o644); err != nil {
		return fmt.Errorf("gcloudcli: write active_config: %w", err)
	}
	return nil
}

func writeConfigFile(path, account, project, tokenContainerPath string) error {
	var sb strings.Builder
	sb.WriteString("[auth]\n")
	sb.WriteString("access_token_file = ")
	sb.WriteString(tokenContainerPath)
	sb.WriteString("\n\n[core]\n")
	if account != "" {
		sb.WriteString("account = ")
		sb.WriteString(account)
		sb.WriteString("\n")
	}
	if project != "" {
		sb.WriteString("project = ")
		sb.WriteString(project)
		sb.WriteString("\n")
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

// RenderConfig writes the gcloud configuration ini file to w (for testing).
func RenderConfig(w io.Writer, account, project, tokenContainerPath string) error {
	_, err := fmt.Fprintf(w, "[auth]\naccess_token_file = %s\n\n[core]\naccount = %s\nproject = %s\n",
		tokenContainerPath, account, project)
	return err
}

// ContainerEnv returns the env vars to inject into the container.
// configContainerPath is the container-side CLOUDSDK_CONFIG directory path.
func ContainerEnv(configContainerPath string) map[string]string {
	return map[string]string{
		ConfigDirEnv: configContainerPath,
	}
}

func validateProjectID(id string) error {
	if id == "" {
		return fmt.Errorf("empty project id")
	}
	for _, c := range id {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '_' && c != ':' {
			return fmt.Errorf("invalid character %q (project ids contain lowercase letters, digits, hyphens, underscores)", c)
		}
	}
	return nil
}
