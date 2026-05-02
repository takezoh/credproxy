// Package gcloudcli provides credential isolation for the gcloud CLI in Docker containers.
//
// Instead of bind-mounting ~/.config/gcloud, this provider writes a synthetic CLOUDSDK_CONFIG
// directory containing per-project gcloud configurations. A per-project GCE metadata server
// emulator is started on the host and exposed to the container via a unix socket + TCP bridge,
// allowing the container's gcloud / Google SDKs to obtain fresh tokens on demand without
// any token file management or expiry timers.
package gcloudcli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ConfigDirEnv is the gcloud SDK environment variable that overrides ~/.config/gcloud.
const ConfigDirEnv = "CLOUDSDK_CONFIG"

// MetadataHostEnv is the env var that redirects GCE metadata requests to a custom host:port.
const MetadataHostEnv = "GCE_METADATA_HOST"

// MetadataIPEnv aligns the Python google-auth library's metadata IP with GCE_METADATA_HOST.
const MetadataIPEnv = "GCE_METADATA_IP"

const globalProperties = "[core]\ndisable_usage_reporting = true\n\n[component_manager]\ndisable_update_check = true\n"

// WriteConfigDir materializes a synthetic CLOUDSDK_CONFIG directory at dir.
// One gcloud configuration named after each project ID is written. active names
// the configuration that becomes the active default (written to active_config).
// tokenContainerPath is the container-side path of the access token file (used by gcloud CLI).
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
	if tokenContainerPath != "" {
		sb.WriteString("[auth]\naccess_token_file = ")
		sb.WriteString(tokenContainerPath)
		sb.WriteString("\n\n")
	}
	sb.WriteString("[core]\n")
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

// ContainerEnv returns the env vars to inject into the container.
// configContainerPath is the container-side CLOUDSDK_CONFIG directory path.
func ContainerEnv(configContainerPath string) map[string]string {
	return map[string]string{
		ConfigDirEnv:    configContainerPath,
		MetadataHostEnv: metadataListenAddr,
		MetadataIPEnv:   "127.0.0.1",
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
