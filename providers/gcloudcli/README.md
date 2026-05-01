# gcloudcli

gcloud CLI credential isolation for containers. Containers receive only short-lived access tokens (≤1h TTL); `~/.config/gcloud` is never bind-mounted.

## Configuration

`GCPConfig` controls the mode and the synthetic config written into the container.

| Field | Required | Description |
|-------|----------|-------------|
| `Account` | yes | host gcloud principal whose credentials are used |
| `Active` | yes | project name written to `active_config` (the container's default project) |
| `ServiceAccount` | SA mode only | SA email to impersonate; presence selects SA mode |
| `Projects` | SA mode only | project IDs for which config files are written |

**Mode selection** is automatic: `ServiceAccount` non-empty → SA mode; empty → user-account mode. There is no flag.

In user-account mode `Projects` is ignored; the synthetic config contains a single configuration for `Active`.

## How it works

`SpecBuilder.ContainerSpec` materializes two items under the per-project run directory:

- **`gcloud-token`** — hard link to a shared token file refreshed on the host. Written in-place (not via atomic rename) to preserve the inode, so the bind-mounted file in the container always reflects the latest token.
- **`gcloud-config/`** — synthetic `CLOUDSDK_CONFIG` directory. One `configurations/config_<project>` per entry in `Projects` (or just `Active` in user-account mode). `active_config` contains the value of `Active`. Each configuration sets `[auth] access_token_file` to the container-side token path.

Container env vars injected:

| Env var | Value |
|---|---|
| `CLOUDSDK_CONFIG` | Container path to the synthetic config directory |

## Token refresh

One `Refresher` goroutine is shared per `(account, service_account)` pair. It calls:

```
gcloud auth print-access-token [--account=<account>] [--impersonate-service-account=<sa>]
```

Refresh is triggered by fsnotify on the host `~/.config/gcloud` directory. When fsnotify is unavailable, the `Refresher` falls back to a 5-minute polling ticker. The first token is fetched synchronously at container start (`Prime`); on failure a warning is logged and gcloud calls in the container receive 401 until the host re-authenticates.

## Security

The core guarantee is that long-lived credentials (OAuth refresh tokens, `credentials.db`) are never exposed to containers. `gcloud auth print-access-token` runs on the host; only the resulting short-lived access token (≤1h TTL) is written to the bind-mounted file. Containers have no means to refresh the token themselves.

**Service account impersonation** (`ServiceAccount` set) additionally enforces project boundaries: the container can only exercise what the SA's IAM bindings permit. This is the recommended configuration for multi-project or shared environments.

**User-account proxy** (`ServiceAccount` empty) provides the same refresh-token isolation but without project boundary enforcement — the access token carries the user's full scope. This is stronger than bind-mounting `~/.config/gcloud` (which leaks the refresh token) but weaker than SA impersonation. Use only when project boundary enforcement is not required.
