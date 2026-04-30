# awssso

AWS SSO credential injection for containers. Containers obtain short-lived credentials through the credproxy HTTP server; `~/.aws/sso/cache` is never bind-mounted.

## How it works

`SpecBuilder.ContainerSpec` materializes two files under the per-project run directory:

- **`aws-config`** — synthetic `~/.aws/config` with one `[profile <name>]` section per configured profile. Each section sets `credential_process` to call `aws-creds.sh <profile>`.
- **`aws-creds.sh`** — helper script that fetches credentials from the proxy via Unix socket:
  ```sh
  curl -fsSL --unix-socket "$CREDPROXY_SOCK" \
    -H "Authorization: Bearer $CREDPROXY_TOKEN" \
    "http://localhost/aws-credentials/$1"
  ```

Container env vars injected:

| Env var | Value |
|---|---|
| `AWS_CONFIG_FILE` | Container path to the synthetic `aws-config` |
| `CREDPROXY_TOKEN` | Ephemeral bearer token (never written to disk) |
| `CREDPROXY_SOCK` | In-container Unix socket path |

## Proxy route

`GET /aws-credentials/<profile>` returns `credential_process`-format JSON:

```json
{"Version":1,"AccessKeyId":"...","SecretAccessKey":"...","SessionToken":"...","Expiration":"..."}
```

`/aws-credentials/default` maps to the unnamed (default) profile.

## Credential fetch order

1. `aws configure export-credentials --format process [--profile <name>]`
2. On failure: scan `~/.aws/sso/cache/*.json` for a valid SSO session, then call `aws sso get-role-credentials`

## Per-project access control

Each project receives its own bearer token. `Provider` maintains a per-project allowlist of profile names (set via `SetAllowedProfiles`). A request whose token resolves to a project that has not registered the requested profile is rejected with an error; the proxy returns 502 to the container.

## Security

`~/.aws/sso/cache` is never bind-mounted. Containers receive only the `credential_process` JSON (short-lived `AccessKeyId` / `SecretAccessKey` / `SessionToken`); the host SSO refresh token stays on the host.
