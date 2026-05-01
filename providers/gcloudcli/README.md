# gcloudcli

gcloud CLI credential isolation for containers. Containers receive only short-lived SA-scoped access tokens (≤1h TTL); `~/.config/gcloud` is never bind-mounted.

## How it works

`SpecBuilder.ContainerSpec` materializes two items under the per-project run directory:

- **`gcloud-token`** — hard link to a shared token file refreshed on the host. Written in-place (not via atomic rename) to preserve the inode, so the bind-mounted file in the container always reflects the latest token.
- **`gcloud-config/`** — synthetic `CLOUDSDK_CONFIG` directory. Contains one `configurations/config_<projectId>` per listed project. Each configuration sets `[auth] access_token_file` to the container-side token path.

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

**Service account impersonation** (`service_account` set) additionally enforces project boundaries: the container can only exercise what the SA's IAM bindings permit. This is the recommended configuration for multi-project or shared environments.

**User-account proxy** (`allow_user_account = true`, `service_account` empty) provides the same refresh-token isolation but without project boundary enforcement — the access token carries the user's full scope. This is stronger than bind-mounting `~/.config/gcloud` (which leaks the refresh token) but weaker than SA impersonation. Use only when project boundary enforcement is not required.
