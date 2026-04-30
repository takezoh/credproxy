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
gcloud auth print-access-token [--account=<account>] --impersonate-service-account=<sa>
```

Refresh is triggered by fsnotify on the host `~/.config/gcloud` directory. When fsnotify is unavailable, the `Refresher` falls back to a 5-minute polling ticker. The first token is fetched synchronously at container start (`Prime`); on failure a warning is logged and gcloud calls in the container receive 401 until the host re-authenticates.

## Security

`service_account` must be non-empty; full-scope account tokens are not supported. Containers are limited to what the SA's IAM bindings permit and cannot act on projects outside those bindings.
