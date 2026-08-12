package config_test

import (
	"strings"
	"testing"

	"github.com/takezoh/credproxy/cmd/credproxyd/config"
)

const validOperationConfig = `
listen_unix = "/tmp/credproxyd.sock"
daemon_revision = "daemon/test-1"

[[operation]]
name = "ctx-sync"
binding_revision = "ctx-sync/2"
executable_paths = ["/opt/credproxy/bin/ctx"]
subcommand = "sync"
credential_command = ["/opt/credproxy/op-resolve", "ctx-sync"]
hook_timeout_sec = 10
max_runtime_sec = 300
pass_env = ["LANG", "LC_ALL", "TZ"]
fixed_env = { PATH = "/usr/bin:/bin", CTX_CONFIG = "/etc/context-fabric/config.toml" }
env = { CTX_DATABASE_URL = "CTX_DATABASE_URL" }

[[operation.argument]]
flag = "-timeout"
type = "duration"
min = "1s"
max = "5m"

[[operation.argument]]
flag = "-min-interval"
type = "duration"
min = "0s"
max = "24h"
`

func TestLoad_closedOperation(t *testing.T) {
	cfg, err := config.Load(writeConfig(t, validOperationConfig))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Operations) != 1 || cfg.Operations[0].BindingRevision != "ctx-sync/2" || len(cfg.Operations[0].Arguments) != 2 {
		t.Fatalf("operation=%+v", cfg.Operations)
	}
}

func TestLoad_rejectsOpenOperationConfig(t *testing.T) {
	mutations := []string{
		strings.Replace(validOperationConfig, `daemon_revision = "daemon/test-1"`, `daemon_revision = ""`, 1),
		strings.Replace(validOperationConfig, `/opt/credproxy/bin/ctx`, `ctx`, 1),
		strings.Replace(validOperationConfig, `flag = "-min-interval"`, `flag = "-timeout"`, 1),
		strings.Replace(validOperationConfig, `type = "duration"`, `type = "string"`, 1),
	}
	for _, body := range mutations {
		if _, err := config.Load(writeConfig(t, body)); err == nil {
			t.Errorf("expected rejection for config mutation")
		}
	}
}

func TestLoad_rejectsDirectRouteUsingClosedCredentialCommand(t *testing.T) {
	body := validOperationConfig + `
[[route]]
path = "/credential-bypass"
credential_command = ["/opt/credproxy/op-resolve", "ctx-sync"]
`
	if _, err := config.Load(writeConfig(t, body)); err == nil {
		t.Fatal("direct HTTP route could return the closed operation credential")
	}
}
