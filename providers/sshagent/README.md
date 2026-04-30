# sshagent

Per-project ephemeral SSH agent for containers. Containers receive a socket they can sign with, but never see private key material.

## How it works

`SpecBuilder.ContainerSpec` spawns an ephemeral `ssh-agent` process per project on first call:

```
ssh-agent -D -a <projectRunDir>/agent.sock
```

Then loads only the explicitly listed keys via `ssh-add`. The agent socket is bind-mounted into the container at `<ContainerRunDir>/agent.sock`.

Container env var injected:

| Env var | Value |
|---|---|
| `SSH_AUTH_SOCK` | Container path to the agent socket |

## Key loading behaviour

- Keys not found on disk are skipped with a warning.
- Passphrase-protected keys are skipped because `ssh-add` runs non-interactively.
- `~/` prefixes in key paths are expanded to the user's home directory.

## Design constraints

Direct forwarding of the host `$SSH_AUTH_SOCK` is intentionally not supported — it would expose all keys the host agent holds to container processes. This provider loads only the declared keys.

## Lifecycle

Agent processes are killed and their sockets removed when the parent context is cancelled (roost daemon shutdown).
