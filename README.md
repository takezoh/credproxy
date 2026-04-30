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
| `providers/awssso` | HTTP route serving `credential_process` JSON via the proxy |
| `providers/gcloudcli` | Synthetic `CLOUDSDK_CONFIG` directory + bind-mounted access token |
| `providers/sshagent` | Per-project ephemeral `ssh-agent` with bind-mounted socket |

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
  "expires_in_sec": 3600
}
```

- `expires_in_sec > 30` → ScriptProvider caches the response; the hook is not re-executed until TTL expires
- `body_replace` → returned as-is to the client, upstream not contacted
- Non-zero exit → 502 to client

### Reference Hooks

| Script | Provider | Requires |
|---|---|---|
| `hooks/anthropic-get.sh` | Anthropic OAuth | `jq` |
| `hooks/anthropic-refresh.sh` | Anthropic OAuth refresh | `curl`, `jq` |
| `hooks/aws-sso-get.sh` | AWS SSO temporary credentials | `aws` CLI, `jq` |

## Architecture

See [ARCHITECTURE.md](ARCHITECTURE.md).
