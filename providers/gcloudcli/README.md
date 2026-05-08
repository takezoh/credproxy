# gcloudcli

gcloud CLI credential isolation for containers. `~/.config/gcloud` (including the OAuth refresh token) is never bind-mounted. Instead the container reaches a GCE metadata server emulator running on the host, which calls `gcloud auth application-default print-access-token` (user-account mode) or `gcloud auth print-access-token --impersonate-service-account` (SA mode) on demand and returns fresh short-lived tokens each time they are needed.

## Configuration

`GCPConfig` controls the mode and the synthetic config written into the container.

| Field | Required | Description |
|-------|----------|-------------|
| `Account` | yes | host gcloud principal whose credentials are used |
| `Active` | yes | GCP project ID written to `active_config` (the container's default project) |
| `ServiceAccount` | SA mode only | SA email to impersonate; presence selects SA mode |
| `Projects` | SA mode only | project IDs for which config files are written |

**Mode selection** is automatic: `ServiceAccount` non-empty → SA mode; empty → user-account mode.

In user-account mode `Projects` is ignored; the synthetic config contains a single configuration for `Active`.

**Host prerequisite (user-account mode):** the host must have Application Default Credentials set up:

```sh
gcloud auth application-default login
```

`gcloud auth login` alone is not sufficient — it does not create ADC credentials. ADC credentials are required because user-account mode calls `gcloud auth application-default print-access-token`, which produces a token accepted by all Google APIs including Cloud Run.

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
- **`gcloud-token`** — token file pre-populated at metadata server startup, refreshed by the periodic scheduler every 25 minutes, and updated on each `/token` request. Read by the gcloud CLI via `access_token_file`; Google SDKs use the metadata server directly.

Container env vars injected:

| Env var | Value | Consumer |
|---|---|---|
| `CLOUDSDK_CONFIG` | Container path to the synthetic config directory | gcloud CLI |
| `GCE_METADATA_HOST` | `127.0.0.1:8181` | gcloud CLI, all Google SDKs |
| `GCE_METADATA_IP` | `127.0.0.1:8181` | Python google-auth (uses this var for GCE detection ping) |

The container also receives a `BridgeSpec` that launches `sockbridge` during `postCreateCommand`:

```sh
sockbridge -listen 127.0.0.1:8181 -socket /opt/roost/run/gcp-metadata.sock &
```

`sockbridge` is a provider-agnostic TCP↔unix forwarder built from `credproxy/bridge/` and installed alongside the roost binary.

## Token refresh

Two delivery paths exist; both ultimately call the same host-side gcloud command:

- **Metadata server** (`/computeMetadata/v1/.../token`) — called by Google SDKs on demand. The response also rewrites `gcloud-token` so the file stays current.
- **`gcloud-token` file** — read directly by the gcloud CLI via `access_token_file`. Refreshed proactively every 25 minutes by the credproxy periodic scheduler (all registered projects in parallel) so it is always valid regardless of CLI invocation frequency.

Token source by mode:

- **User-account:** `gcloud auth application-default print-access-token` — ADC credentials on the host; token accepted by all Google APIs including Cloud Run RunJob.
- **SA mode:** `gcloud auth print-access-token [--account=<account>] --impersonate-service-account=<sa>` — host account generates an impersonation token for the SA.

`gcloud` auto-refreshes its own stored credentials on the host via the host refresh token. Token TTL in the metadata response is reported as 1800 seconds (a conservative value; clients use this for cache TTL).

## Metadata server endpoints

| Endpoint | Returns |
|---|---|
| `/` | `0.1/\ncomputeMetadata/\n` (GCE detection ping target) |
| `/computeMetadata/` | `v1/\n` |
| `/computeMetadata/v1/` | `instance/\nproject/\n` |
| `/computeMetadata/v1/instance/service-accounts/<seg>/` | JSON `{aliases, email, scopes}` (recursive info) |
| `/computeMetadata/v1/instance/service-accounts/<seg>/token` | JSON `{access_token, expires_in, token_type}` |
| `/computeMetadata/v1/instance/service-accounts/<seg>/email` | SA email (or account if no SA) |
| `/computeMetadata/v1/instance/service-accounts/<seg>/scopes` | `https://www.googleapis.com/auth/cloud-platform` |
| `/computeMetadata/v1/project/project-id` | Active project ID |

`<seg>` accepts `default` or the active principal email (SA email in SA mode; account email in user-account mode). Any other segment returns 404.

All endpoints require `Metadata-Flavor: Google` request header and return `Metadata-Flavor: Google` response header (standard GCE metadata protocol).

## Security

`~/.config/gcloud` (refresh token, credentials.db) is never exposed to the container. `gcloud auth print-access-token` runs on the host; only the resulting short-lived access token reaches the container, either via the metadata server or via `access_token_file`. Containers have no means to obtain long-lived credentials.

**Service account impersonation** (`ServiceAccount` set) enforces project boundaries: the container can only exercise what the SA's IAM bindings permit. This is the recommended configuration for multi-project or shared environments.

**User-account proxy** (`ServiceAccount` empty) provides the same refresh-token isolation without project boundary enforcement. The access token carries the user's full scope. Use only when SA setup is not feasible.
