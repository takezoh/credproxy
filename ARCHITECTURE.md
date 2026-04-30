# Architecture

## Purpose

credproxy is a Go library that provides a provider-agnostic HTTP forward proxy for credential injection. Container-resident agent processes (Claude Code, AWS CLI, etc.) reach it over HTTP; it injects credentials before forwarding to the real upstream.

The core invariant: **the library contains no provider knowledge**. Anthropic OAuth, AWS SSO, and any future provider are implemented by callers as `Provider` implementations. The library handles HTTP forward, bearer authentication, refresh retry, and routing only.

## Usage Modes

### In-process

```go
srv, err := credproxy.New(credproxy.ServerConfig{
    ListenTCP:  "127.0.0.1:0",   // ephemeral port
    AuthTokens: []credproxy.TokenAuth{{Token: token, ID: "client-1"}},
    Routes: []credproxy.Route{
        {Path: "/anthropic", Upstream: "https://api.anthropic.com",
         Provider: myAnthropicProvider, RefreshOnStatus: []int{401}, StripInboundAuth: true},
        {Path: "/aws-credentials", Provider: myAWSSSOProvider},
    },
})
// srv.Addr() → "127.0.0.1:PORT" (resolved after New returns)
go srv.Run(ctx)
```

### Shared daemon (credproxyd binary)

Uses `ScriptProvider` to delegate credential operations to external hook scripts via stdin/stdout JSON. Allows shell-script-based providers without recompilation.

```
systemd/launchd ── credproxyd (127.0.0.1:9787 + Unix socket)
                        │
                ScriptProvider → hook scripts (bash + curl + jq)
                        │
                api.anthropic.com / AWS SSO endpoint
```

## Design Principles

- **Provider-agnostic core**: the `credproxy` package contains no Anthropic, AWS, or git-specific logic
- **Provider interface**: callers implement `Get(ctx, Request) (*Injection, error)` and `Refresh(ctx, Request) (*Injection, error)`. Caching is the Provider's responsibility
- **Functional core / imperative shell**: credential injection decisions (`decideAction`, `planRequest`, `needsRefresh`) are pure functions; HTTP I/O and subprocess execution are thin shells over them
- **Streaming first**: `httputil.ReverseProxy.ModifyResponse` detects refresh-triggering status codes at header receipt time; response bodies stream transparently without buffering
- **Store interface**: generic `Load`/`Save` byte store; `FileStore` is provided as a ready-to-use implementation (atomic rename, mode 0600)
- **Ephemeral port support**: `New()` binds listeners immediately; `Addr()` returns the resolved address before `Run()` is called
- **Single static binary** (credproxyd): no runtime dependencies
- **Secure by default**: TCP listener + empty `AuthTokens` requires explicit `AllowUnauthenticated: true`; Unix socket is created with mode 0600 (overridable via `UnixMode`)

## Request Flow

```
container (agent)
  │  HTTP  Authorization: Bearer <proxy-token>
  ▼
credproxy.Server (TCP or Unix socket)
  │  bearer auth middleware  (constant-time token comparison)
  │  route prefix match (/anthropic → Route)
  │  Provider.Get(ctx, Request) → *Injection
  │    cache hit?  → Provider returns cached Injection
  │    cache miss? → singleflight-deduplicated subprocess / fetch
  │  decideAction(Route, Injection):
  │    BodyReplace non-nil → return synthetic body, done
  │    no Upstream        → 502
  │    else               → applyPlan → httputil.ReverseProxy
  │      ModifyResponse: status in RefreshOnStatus?
  │        no  → stream response body transparently (SSE safe)
  │        yes → drain, return errNeedsRefresh sentinel
  │      ErrorHandler (errNeedsRefresh):
  │        Provider.Refresh → new Injection → retry once
  ▼
upstream (api.anthropic.com, AWS SSO, ...)
```

For routes where `Injection.BodyReplace` is non-nil (e.g. AWS SSO credential endpoints), the proxy returns the synthetic body directly without forwarding to any upstream. This applies on both the initial and refresh paths.

## Provider Interface

```go
type Provider interface {
    Get(ctx context.Context, req Request) (*Injection, error)
    Refresh(ctx context.Context, req Request) (*Injection, error)
}

type Request struct {
    Method   string
    Path     string
    Host     string
    Metadata map[string]string // caller-supplied key/value; forwarded to hook scripts as "context"
}

type Injection struct {
    Headers     map[string]string
    Query       map[string]string
    BodyReplace []byte    // non-nil → return directly, skip upstream
    ExpiresAt   time.Time // informational; caching is Provider's responsibility
}
```

## ScriptProvider Hook Protocol (credproxyd)

stdin (one-line JSON):
```json
{
  "action": "get" | "refresh",
  "route": "<route name>",
  "request": {"method": "POST", "path": "/v1/messages", "host": "api.anthropic.com"},
  "context": {"client": "my-app", "project_path": "/workspace/foo"}
}
```

stdout (one-line JSON):
```json
{
  "headers": {"Authorization": "Bearer <access-token>"},
  "query": {},
  "expires_in_sec": 3600,
  "body_replace": {}
}
```

Rules:
- exit 0 → OK; non-zero → proxy returns 502 to client
- timeout (default 10 s) → 502
- `expires_in_sec > 30` → ScriptProvider caches result for `expires_in_sec - 30` seconds
- `body_replace` non-nil → proxy returns it as the response body, skips upstream forward

## Package Layout

```
credproxy/              HTTP proxy core — Provider/Store interfaces, Server, Route handler
  types.go              Provider, Request, Injection, Route, ServerConfig, Store interfaces
  server.go             Server — TCP + Unix socket listeners, lifecycle (effectful shell)
  auth.go               authMiddleware + pure extractBearer / matchToken
  inject.go             decideAction / planRequest / needsRefresh (pure core)
  route.go              routeHandler — ModifyResponse/ErrorHandler-based refresh retry
  store/file.go         FileStore — atomic file-backed Store implementation
container/              Container-injection abstraction
  spec.go               Spec{Env, Mounts} — per-launch contribution
  provider.go           Provider interface (Name/Init/Routes/ContainerSpec)
  runhash.go            ProjectRunHash — stable per-project run-dir name
providers/              Pre-built container.Provider implementations
  awssso/               AWS SSO via credential_process + HTTP route
  gcloudcli/            Synthetic CLOUDSDK_CONFIG + host-refreshed access token
  sshagent/             Per-project ephemeral ssh-agent
cmd/credproxyd/         Shared daemon binary (hook script users)
  main.go               flag parse, signal handling, credproxy.New + Run
  config/               Load / expand (pure) / validate (pure)
  providers/script/     Subprocess shell + ttlCache + singleflight
hooks/                  Reference hook scripts (bash + curl + jq + aws CLI)
packaging/              systemd unit, launchd plist
```

The `container` package and the `providers/` tree are independent of the HTTP proxy core: a provider may register HTTP `Routes()` (awssso) or use bind-mounts only (gcloudcli, sshagent).

## Client Integration

In-process:

```
ANTHROPIC_BASE_URL=http://host.docker.internal:<PORT>/anthropic
ANTHROPIC_AUTH_TOKEN=<ephemeral-bearer-token>
AWS_CONTAINER_CREDENTIALS_FULL_URI=http://host.docker.internal:<PORT>/aws-credentials
AWS_CONTAINER_AUTHORIZATION_TOKEN=<ephemeral-bearer-token>
```

On Linux Docker, `host.docker.internal` requires `--add-host=host.docker.internal:host-gateway`.

## Security Properties

| Property | Mechanism |
|---|---|
| Proxy bearer auth | `Authorization: Bearer <token>` validated; scheme enforced (case-insensitive); constant-time comparison |
| Open-default prevention | TCP listener + empty `AuthTokens` requires `AllowUnauthenticated: true` or `New()` returns an error |
| Unix socket permissions | Created with mode 0600 (default); overridable via `ServerConfig.UnixMode` |
| Unix socket path guard | `New()` refuses to remove a non-socket file at `ListenUnix` path |
| No credential files in container | `~/.claude/.credentials.json` is never bind-mounted; subdir mounts only |
| Ephemeral bearer token | In-process mode generates a fresh token per process; never written to disk |
| Refresh token isolation | Token stays on host; container receives only short-lived access tokens |
| Single refresh point | singleflight de-duplicates concurrent cache-miss fetches; at most one subprocess per route at a time |
| Hook script scope | ScriptProvider scripts run as the host user; they read host credential files directly |
