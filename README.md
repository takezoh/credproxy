# credproxy

Brokers credentials to sandboxed agent processes (Docker containers, VMs) so they never hold secrets directly. Routes either inject auth headers into proxied requests, or serve a synthetic body that emulates a credential endpoint (e.g. AWS ECS).

Also ships as `credproxyd` — a standalone shared daemon for hook-script-based providers.

## Library Usage

```go
import "github.com/takezoh/credproxy/credproxy"

srv, err := credproxy.New(credproxy.ServerConfig{
    ListenTCP:  "127.0.0.1:0",          // ephemeral port
    AuthTokens: []credproxy.TokenAuth{{Token: myBearerToken, ID: "my-client"}},
    Routes: []credproxy.Route{
        {
            Path:             "/anthropic",
            Upstream:         "https://api.anthropic.com",
            Provider:         myAnthropicProvider,
            RefreshOnStatus:  []int{401},
            StripInboundAuth: true,
        },
        {
            Path:     "/aws-credentials",
            Provider: myAWSSSOProvider, // uses BodyReplace, no upstream
        },
    },
})
addr := srv.Addr() // "127.0.0.1:PORT" — resolved immediately after New()
go srv.Run(ctx)    // blocks until ctx is cancelled
```

## Provider Interface

```go
type Provider interface {
    Get(ctx context.Context, req Request) (*Injection, error)
    Refresh(ctx context.Context, req Request) (*Injection, error)
}
```

`Get` is called for every request (cache internally). `Refresh` is called when the upstream returns a status in `RefreshOnStatus`; the request is then retried once.

`Injection.BodyReplace`, when non-nil, is returned directly to the client without upstream forwarding (useful for credential endpoint emulation, e.g. AWS ECS credential provider).

## Store Interface

```go
type Store interface {
    Load(ctx context.Context, key string) ([]byte, error)
    Save(ctx context.Context, key string, data []byte) error
}
```

`FileStore` is provided:

```go
import "github.com/takezoh/credproxy/credproxy/store"

s := store.NewFileStore("~/.mytool/credentials", 0) // mode 0600 enforced
```

## Container Providers

For agents running in containers, `container.Provider` abstracts the per-launch contribution of env vars and bind-mounts a credential backend needs. Pre-built implementations live under `providers/`:

```go
import (
    "github.com/takezoh/credproxy/container"
    "github.com/takezoh/credproxy/providers/awssso"
    "github.com/takezoh/credproxy/providers/gcloudcli"
    "github.com/takezoh/credproxy/providers/sshagent"
)

type Provider interface {
    Name() string
    Init() error                       // create host-side directories etc.
    Routes() []credproxy.Route         // HTTP routes to register on the proxy (may be nil)
    ContainerSpec(ctx, projectPath) (container.Spec, error)
}
```

Each provider is constructed from a typed `Config` plus caller-supplied callbacks (project allowlists, token providers). Providers contain no caller-specific concepts and can be used independently of the proxy server.

| Provider | Mechanism |
|---|---|
| [`providers/awssso`](providers/awssso/README.md) | HTTP route serving `credential_process` JSON via the proxy |
| [`providers/gcloudcli`](providers/gcloudcli/README.md) | Synthetic `CLOUDSDK_CONFIG` directory + bind-mounted access token |
| [`providers/sshagent`](providers/sshagent/README.md) | Per-project ephemeral `ssh-agent` with bind-mounted socket |

---

## credproxyd — Shared Daemon

`credproxyd` is a standalone daemon that uses `ScriptProvider` to delegate credential operations to external hook scripts. Providers are configured via `~/.config/credproxyd/config.toml` — no recompilation required.

### Quick Start

```sh
make build
sudo make install   # installs to /usr/local/bin/credproxyd
```

Configure:

```sh
mkdir -p ~/.config/credproxyd/hooks
cp hooks/*.sh ~/.config/credproxyd/hooks/
chmod +x ~/.config/credproxyd/hooks/*.sh

openssl rand -hex 32 > ~/.config/credproxyd/token
chmod 600 ~/.config/credproxyd/token
```

Create `~/.config/credproxyd/config.toml`:

```toml
listen_tcp      = "127.0.0.1:9787"
auth_tokens_file = "~/.config/credproxyd/token"

[[route]]
path                = "/anthropic"
upstream            = "https://api.anthropic.com"
credential_command  = ["bash", "-c", "exec ${HOME}/.config/credproxyd/hooks/anthropic-get.sh"]
refresh_command     = ["bash", "-c", "exec ${HOME}/.config/credproxyd/hooks/anthropic-refresh.sh"]
refresh_on_status   = [401]
hook_timeout_sec    = 10
strip_inbound_auth  = true

[[route]]
path               = "/aws-credentials"
credential_command = ["bash", "-c", "exec ${HOME}/.config/credproxyd/hooks/aws-sso-get.sh"]
hook_timeout_sec   = 10
```

#### Per-route client restriction

The tokens file may name each client as `<id>=<token>` (one per line; bare
lines stay unnamed and get positional ids `token-0`, `token-1`, …). A route can
then be restricted to specific clients with `allowed_client_ids`; other
authenticated clients get 403. Every referenced id must exist as a named token
or the daemon refuses to start.

```toml
[[route]]
path               = "/anthropic"
upstream           = "https://api.anthropic.com"
credential_command = ["bash", "-c", "exec ${HOME}/.config/credproxyd/hooks/anthropic-get.sh"]
allowed_client_ids = ["ci-runner"]
```

Token id is attribution; `allowed_client_ids` is what turns it into
authorization.

Start:

```sh
# Direct
credproxyd --config ~/.config/credproxyd/config.toml

# systemd
sudo systemctl enable --now credproxyd
```

Test:

```sh
TOKEN=$(cat ~/.config/credproxyd/token)
curl -s http://localhost:9787/healthz           # → ok
curl -H "Authorization: Bearer ${TOKEN}" http://localhost:9787/anthropic/v1/models
```

### Hook Protocol

Hooks receive a JSON object on stdin and must write a JSON object to stdout:

**stdin:**
```json
{
  "action": "get",
  "route": "anthropic",
  "request": {"method": "POST", "path": "/v1/messages", "host": "api.anthropic.com"},
  "context": {"client": "my-app", "project_path": "/workspace/foo"}
}
```

**stdout:**
```json
{
  "headers": {"Authorization": "Bearer <access-token>"},
  "append_headers": {"anthropic-beta": "oauth-2025-04-20"},
  "expires_in_sec": 3600
}
```

- `headers` → set on the upstream request, replacing any existing value
- `append_headers` → merged into the existing comma-separated header value (e.g. `anthropic-beta`) instead of overwriting it; useful when the client already sends values for the same header. Tokens already present are not duplicated
- `expires_in_sec > 30` → ScriptProvider caches the response; the hook is not re-executed until TTL expires
- `body_replace` → returned as-is to the client, upstream not contacted
- Non-zero exit → 502 to client

#### Typed failure reasons

A failing hook may classify the failure so the client can distinguish failure
classes (rate limit vs invalid credential vs unreachable) without parsing logs.
Print `reason:<token>` as the **first line of stderr** before exiting non-zero;
the token must match `[a-z0-9_]{1,64}`:

```sh
echo "reason:op_rate_limited" >&2
exit 1
```

The proxy then returns a structured 502 body — `{"error":"credential_unavailable","route":"<route>","reason":"op_rate_limited"}` —
instead of the opaque default. Only the reason token crosses to the client; the
rest of stderr stays in server logs, so a hook cannot leak a secret through this
channel even if it accidentally prints one. stderr without a valid `reason:` line
keeps the opaque 502.

### Reference Hooks

| Script | Provider | Requires |
|---|---|---|
| `hooks/anthropic-get.sh` | Anthropic OAuth | `jq` |
| `hooks/anthropic-refresh.sh` | Anthropic OAuth refresh | `curl`, `jq` |
| `hooks/aws-sso-get.sh` | AWS SSO temporary credentials | `aws` CLI, `jq` |

---

## credproxy run — ad-hoc secret injection

`credproxy run` resolves opaque references in an env-file and injects the real values into a **single subprocess** environment. Follows the `op run --env-file` model.

```sh
credproxy run --env-file .secrets.env -- terraform apply
```

The env-file uses `NAME=ref` format. Lines with comments (`#`) or without `=` are skipped.

```ini
TF_VAR_db_password=op://infra/db/password
TF_VAR_api_key=op://infra/api/key
```

Configure hook in `~/.config/credproxy/config.toml`:

```toml
hook = ["/usr/local/bin/resolve-secret"]
hook_timeout_sec = 15   # default: 10
```

The hook receives one reference per call: `stdin: {"ref":"<ref>"}` → `stdout: {"value":"<secret>","expires_in_sec":N}`.

Resolved values are injected into the subprocess environment only. If resolution fails for any entry, the command is not executed. The subprocess runs via `syscall.Exec` — it replaces the credproxy process entirely, so signals are received directly.

**Note:** `credproxy run` is for bare-host use only (no gate). When running inside a roost devcontainer, the `credproxy` binary on PATH is the roost-provided shim, which routes through the host-side broker that enforces an env-file path allowlist.

## credproxy resolve — resolve env-file refs and print JSON env

`credproxy resolve` resolves the same env-file format as `run` but prints the result as JSON instead of executing a command. Useful for scripts that need the resolved values as structured data.

```sh
credproxy resolve --env-file .secrets.env
# stdout: {"env":{"TF_VAR_db_password":"actual-value","TF_VAR_api_key":"key-value"}}
```

Uses the same `~/.config/credproxy/config.toml` hook configuration as `run`.

**Security:** `resolve` outputs only the entries declared in the env-file. Host environment variables are never included in the output — only the resolved secret values cross the process boundary to the caller. This is the interface used by the roost container broker to resolve secrets on behalf of container-side processes.

## credproxy exec — run a command with a broker route's env injected

`credproxy exec` fetches an env map from a running `credproxyd` over a Unix
socket and execs a command with those variables injected. Unlike `run`/`resolve`,
the caller cannot name refs or env-files — the only degree of freedom is the
route name, and the route → ref mapping lives on the daemon side.

```sh
credproxy exec --socket "$XDG_RUNTIME_DIR/credproxyd/broker.sock" \
  --route ctx-sync --token-file ~/.config/credproxyd/token -- ctx sync
```

The daemon serves the route via a `body_replace` hook returning
`{"env":{"NAME":"value", ...}}` (the same schema as `resolve`). The values are
injected into the child environment only, then `credproxy exec` replaces itself
via `syscall.Exec`. The route name is charset-restricted (`[a-z0-9_-]+`) so it
cannot smuggle path segments into the broker URL; a broker error surfaces the
typed `reason` (see Typed failure reasons) rather than a bare status.

This is the client half of the fixed-wrapper pattern: a sandboxed agent gets to
pick *which wrapper to call*, never *which secret to resolve*. The resolved
secret still enters the child's environment, so `exec` narrows the attack
surface (no arbitrary ref/env-file, no `resolve`-style stdout dump) but does not
hide the value from the child process itself.

## secretenv — resolver library

`secretenv/` is a roost-agnostic library for env-file parsing and secret resolution. It is used both by `cmd/credproxy` (bare-host) and by the roost `platform/secretenv` broker (container-side).

```go
import "github.com/takezoh/credproxy/secretenv"

hook := secretenv.NewScriptHook([]string{"/usr/local/bin/resolve"}, 10*time.Second)
resolver := secretenv.NewResolver(hook)
env, err := resolver.ResolveFile(ctx, ".secrets.env")
// env: map[string]string{"SECRET": "actual-value", ...}
```

`ScriptHook` caches resolved values per-ref using the TTL from `expires_in_sec` minus a 30-second safety margin. Concurrent `Resolve` calls for the same ref are deduplicated via singleflight.

## Architecture

Governing architecture is maintained in [`docs/design/`](docs/design/), with decision history in [`docs/adr/`](docs/adr/).
