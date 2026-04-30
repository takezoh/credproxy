package awssso

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// EnvKeyToken is the container env var for the bearer token presented to the proxy.
const EnvKeyToken = "CREDPROXY_TOKEN"

// EnvKeySock is the container env var for the path of the credproxy Unix socket.
const EnvKeySock = "CREDPROXY_SOCK"

// helperScript is the container-side credential_process helper.
// It receives the profile name as $1 and calls back to the proxy via Unix socket.
const helperScript = `#!/bin/sh
exec curl -fsSL --unix-socket "$CREDPROXY_SOCK" \
  -H "Authorization: Bearer $CREDPROXY_TOKEN" \
  "http://localhost/aws-credentials/$1"
`

// WriteHelperScript materializes the helper script at hostPath with mode 0o755.
func WriteHelperScript(hostPath string) error {
	return os.WriteFile(hostPath, []byte(helperScript), 0o755)
}

// ContainerEnv returns the env vars to inject into the container.
// sockPath is the in-container path to the credproxy Unix socket.
func ContainerEnv(token, sockPath string) map[string]string {
	return map[string]string{
		EnvKeyToken: token,
		EnvKeySock:  sockPath,
	}
}

// RenderConfig writes a synthetic ~/.aws/config to w.
// Each name in profiles becomes a [profile <name>] section with credential_process
// pointing to scriptPath. If "default" is listed, a [default] section is emitted.
func RenderConfig(w io.Writer, profiles []string, scriptPath string) error {
	for _, name := range profiles {
		if err := validateProfileName(name); err != nil {
			return err
		}
		var section string
		if name == "default" {
			section = "[default]"
		} else {
			section = "[profile " + name + "]"
		}
		if _, err := fmt.Fprintf(w, "%s\ncredential_process = %s %s\n\n", section, scriptPath, name); err != nil {
			return err
		}
	}
	return nil
}

func validateProfileName(name string) error {
	if name == "" {
		return fmt.Errorf("awssso: profile name must not be empty")
	}
	for _, ch := range name {
		if ch == ']' || ch == '\n' || ch == '\r' || ch == '\x00' {
			return fmt.Errorf("awssso: profile name %q contains invalid character", name)
		}
	}
	if strings.ContainsAny(name, " \t\\'\"") || strings.ContainsRune(name, '`') {
		return fmt.Errorf("awssso: profile name %q contains shell-unsafe character", name)
	}
	return nil
}
