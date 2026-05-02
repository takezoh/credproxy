# gcloudcli

gcloud CLI credential isolation for containers. `~/.config/gcloud` (including the OAuth refresh token) is never bind-mounted. Instead the container reaches a GCE metadata server emulator running on the host, which calls `gcloud auth print-access-token` on demand and returns fresh short-lived tokens each time they are needed.

## Configuration

`GCPConfig` controls the mode and the synthetic config written into the container.

| Field | Required | Description |
|-------|----------|-------------|
| `Account` | yes | host gcloud principal whose credentials are used |
| `Active` | yes | project name written to `active_config` (the container's default project) |
| `ServiceAccount` | SA mode only | SA email to impersonate; presence selects SA mode |
| `Projects` | SA mode only | project IDs for which config files are written |

**Mode selection** is automatic: `ServiceAccount` non-empty → SA mode; empty → user-account mode.

In user-account mode `Projects` is ignored; the synthetic config contains a single configuration for `Active`.

## How it works

```
[container gcloud / Google SDK]
    │ HTTP to 127.0.0.1:8181  (GCE_METADATA_HOST)
    ↓
[sockbridge — in-container TCP↔unix forwarder]
    │ unix socket (bind-mounted from host run dir)
    ↓
[host: per-project GCE metadata server emulator (unix socket listener)]
    │ exec
    ↓
[host: gcloud auth print-access-token]   ← auto-refreshes via refresh token
```

`ContainerSpec` materializes two items under the per-project run directory:

- **`gcp-metadata.sock`** — unix socket the host-side metadata server listens on. Exposed to the container as `ContainerRunDir/gcp-metadata.sock` via the per-project bind-mount.
- **`gcloud-config/`** — synthetic `CLOUDSDK_CONFIG` directory. One `configurations/config_<project>` per entry in `Projects` (or just `Active` in user-account mode). `active_config` contains the value of `Active`. Each configuration sets `[core] account` and `[auth] access_token_file` (for the gcloud CLI) pointing to the container-side token path.
- **`gcloud-token`** — token file pre-populated at metadata server startup and updated on each `/token` request. Read by the gcloud CLI via `access_token_file`; kept current by the metadata server so gcloud CLI and Google SDKs see consistent credentials.

Container env vars injected:

| Env var | Value | Consumer |
|---|---|---|
| `CLOUDSDK_CONFIG` | Container path to the synthetic config directory | gcloud CLI |
| `GCE_METADATA_HOST` | `127.0.0.1:8181` | gcloud CLI, all Google SDKs |
| `GCE_METADATA_IP` | `127.0.0.1` | Python google-auth library |

The container also receives a `BridgeSpec` that launches `sockbridge` during `postCreateCommand`:

```sh
sockbridge -listen 127.0.0.1:8181 -socket /opt/roost/run/gcp-metadata.sock &
```

`sockbridge` is a provider-agnostic TCP↔unix forwarder built from `credproxy/bridge/` and installed alongside the roost binary.

## Token refresh

Tokens are fetched **on demand** when the metadata server's `/computeMetadata/v1/instance/service-accounts/default/token` endpoint is called:

```
gcloud auth print-access-token [--account=<account>] [--impersonate-service-account=<sa>]
```

`gcloud` auto-refreshes its own stored credentials via the host refresh token — no polling or expiry timer is needed. Token TTL in the metadata response is reported as 1800 seconds (a conservative value; clients use this for cache TTL).

The gcloud CLI reads `access_token_file` directly and therefore bypasses the metadata server. Both paths ultimately call `gcloud auth print-access-token` on the host, so they always return a valid (auto-refreshed) token.

## Metadata server endpoints

| Endpoint | Returns |
|---|---|
| `/computeMetadata/v1/instance/service-accounts/default/token` | JSON `{access_token, expires_in, token_type}` |
| `/computeMetadata/v1/instance/service-accounts/default/email` | SA email (or account if no SA) |
| `/computeMetadata/v1/instance/service-accounts/default/scopes` | `https://www.googleapis.com/auth/cloud-platform` |
| `/computeMetadata/v1/project/project-id` | Active project ID |

All endpoints require `Metadata-Flavor: Google` header (standard GCE metadata protocol).

## Security

`~/.config/gcloud` (refresh token, credentials.db) is never exposed to the container. `gcloud auth print-access-token` runs on the host; only the resulting short-lived access token reaches the container, either via the metadata server or via `access_token_file`. Containers have no means to obtain long-lived credentials.

**Service account impersonation** (`ServiceAccount` set) enforces project boundaries: the container can only exercise what the SA's IAM bindings permit. This is the recommended configuration for multi-project or shared environments.

**User-account proxy** (`ServiceAccount` empty) provides the same refresh-token isolation without project boundary enforcement. The access token carries the user's full scope. Use only when SA setup is not feasible.
