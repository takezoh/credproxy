---
id: design-credential-broker
kind: design
title: Credential broker architecture
status: active
created: '2026-07-16'
scope_type: system
responsibilities:
- id: RESP-001
  statement: Forward HTTP requests while injecting credentials supplied by a route
    Provider.
- id: RESP-002
  statement: Authenticate clients, select routes, retry once after provider refresh,
    and support synthetic credential responses.
- id: RESP-003
  statement: Expose reusable storage and secret-resolution seams without embedding
    backend knowledge.
invariants:
- id: INV-001
  statement: The credproxy core contains no provider-specific credential logic.
  enforcement: review
- id: INV-002
  statement: Response bodies stream unless a refresh-triggering response must be drained
    before the single retry.
  enforcement: test
- id: INV-003
  statement: A TCP listener without authentication requires explicit AllowUnauthenticated
    opt-in.
  enforcement: contract
- id: INV-004
  statement: A route restricted by AllowedClientIDs serves only the named authenticated
    clients; the restriction is rejected under AllowUnauthenticated (no identity to
    check).
  enforcement: test
- id: INV-005
  statement: RequireSamePeerUID is Unix-socket-only, admits only same-UID peers, denies
    when peer credentials are unavailable, and is refused on a TCP listener or an
    unsupported platform (fail-closed).
  enforcement: test
- id: INV-006
  statement: Only a hook's typed reason token reaches the client 502 body; hook stderr
    detail never crosses the client boundary.
  enforcement: test
- id: INV-007
  statement: Route discovery is side-effect free and caller-scoped — it lists only
    routes the server answers locally and the caller is allowed to use — and a configured
    route may not claim a server-reserved path.
  enforcement: test
- id: INV-008
  statement: Consumer command selection, executable admission, argv policy, and consumer
    process execution never enter credproxy APIs, configuration, or daemon runtime.
  enforcement: test
- id: INV-009
  statement: A Windows named-pipe client admits a server only when its process token
    SID matches the current user and its running image has the same file identity
    as the protected managed executable; image ownership is evaluated independently
    under the managed-image DACL policy.
  enforcement: test
- id: INV-010
  statement: Windows managed-image owners and write/control-capable trustees are limited
    to the current user, SYSTEM, Administrators, or TrustedInstaller; every other
    effective allow ACE is limited to read/execute. Paths reject reparse points and
    keep every opened ancestor held without write/delete sharing through identity
    admission.
  enforcement: test
- id: INV-011
  statement: A Windows 1Password token file and its parent/grandparent are owned by
    the current SID and grant access only to that SID and SYSTEM; reparse components
    are rejected and token bytes are read from the validated held handle.
  enforcement: test
- id: INV-012
  statement: Windows named-pipe listeners are local-only and first-instance, use an
    explicit owner/SYSTEM DACL, and admit only clients whose impersonated token SID
    matches RequirePeerSID (defaulting to the current SID); missing identity fails
    closed.
  enforcement: test
boundaries:
  provides:
  - authenticated HTTP proxy routes and synthetic credential endpoints
  - Provider, Store, container provider, and secret resolver contracts
  consumes:
  - caller-owned credential Providers and hooks
  - host networking and credential stores
  forbidden:
  - provider-specific authentication flows in the credproxy package
  - mounting long-lived host credential stores into containers
  - selecting, validating, or executing a credential consumer command
variability:
  fixed:
  - provider-agnostic request planning and one-refresh retry
  - authenticated-by-default TCP behavior
  free:
  - route prefixes, upstreams, refresh statuses, and Provider implementations
  - in-process or shared-daemon deployment
capabilities:
- id: cap:credential-broker
  uniqueness: per-boundary
- id: cap:secret-resolution
  uniqueness: multiple
failure_responsibilities:
- Provider and hook failures become observable proxy errors and are never treated
  as empty credentials.
- Partial secret resolution prevents command execution.
trust_boundaries:
- untrusted container client to authenticated host proxy
- untrusted local named-pipe client to authenticated Windows broker/admin listener
- local client to the Windows daemon process and managed executable identity
- provider output to upstream request mutation
- local owner to private daemon refresh socket
- opaque secret references to resolved process environment
compatibility_policies:
- Provider and Injection semantics remain stable across in-process and daemon modes.
tags: []
owners: []
relations: []
source_paths:
- credproxy/types.go
- credproxy/server.go
- cmd/credproxy/env.go
- internal/nativeipc/client_windows.go
- internal/winsecurefile/file_windows.go
- secretenv/resolver.go
summary: Provider-agnostic authenticated credential injection, forwarding, and secret-resolution
  architecture.
updated: '2026-08-12'
---

## Purpose

Broker short-lived credentials to sandboxed processes without giving those processes long-lived host credential material.

## Responsibilities

The core authenticates a client, matches a route, asks a Provider for an Injection, and either returns a synthetic body or forwards a planned request. A configured refresh status triggers one Provider refresh and one retry.

On Windows, `internal/nativeipc` owns named-pipe identity and handshake syscalls.
Before returning a pipe connection to the HTTP client, it checks that the server
process token belongs to the current user and that the running image matches a
protected managed executable. Image file ownership and DACL admission are a
separate integrity policy; they do not stand in for server process identity.

## Boundaries

The `credproxy` package owns HTTP behavior only. `container/` describes per-launch
mounts and environment, `providers/` implements reusable credential backends, and
`secretenv/` resolves opaque references. Provider helpers may execute a fixed
credential-acquisition program; this does not authorize credproxy to know or execute
the command that will consume the resulting credential.

`credproxy env`, `resolve`, and the generic caller-selected `exec` helper are
delivery surfaces. They do not contain an executable allowlist, argv grammar,
consumer identity, or operation semantics. A consumer that must not receive the
credential uses a protocol injection route (for example an HTTP Authorization
header); its own repository owns the operation behind that protocol.

## Invariants

- Credential caching belongs to Providers or hook wrappers.
- The core never learns Anthropic, AWS, gcloud, SSH, or caller-specific concepts.
- Long-lived refresh tokens and private keys remain on the host side of the boundary.
- A route may be restricted to named clients (`AllowedClientIDs`); token id is attribution, the restriction is what makes it authorization.
- A Unix-socket server may require same-UID peers (`RequireSamePeerUID`) as defense-in-depth over the 0600 socket, failing closed where peer credentials cannot be read.
- Windows named-pipe identity uses the explicit peer SID policy and authenticates the server process SID against the current user before HTTP bytes are sent. The expected executable may be owned by the current user, SYSTEM, Administrators, or TrustedInstaller; other principals may receive read/execute rights only. All path components remain held without write/delete sharing through file-identity comparison.
- Windows 1Password token admission uses a separate confidentiality policy: the token and its parent/grandparent are current-SID-owned, only the current SID and SYSTEM may have allow ACEs, reparse components fail closed, and reads use the validated handle.
- Hook failures may carry a machine-readable `reason` to the client; only the reason token crosses the boundary, so a hook cannot leak a secret through the error path.
- `/healthz` and `/_routes` are server-owned paths; a route that collides with one is a startup error, not a shadowed control.
- Discovery never triggers an upstream request and never advertises a route the caller could not use, so listing routes costs nothing and reveals nothing beyond what the caller may already fetch.
- No route or daemon configuration can name a consumer executable, subcommand, argv
  grammar, consumer environment, or process result.

## Collaboration

Routes bind Provider output to HTTP behavior. Container providers can contribute routes, mounts, environment, or bridge specifications independently.

## Failure Responsibility

Authentication failures stop at the proxy boundary. Provider failures return an error response. Secret resolution is all-or-nothing, so a subprocess never receives a partially resolved environment.

## Variability

Providers and routes are extensible. Authentication defaults, retry count, stream behavior, and host-secret isolation are fixed policy.

## Conformance

`go test ./...` includes a repository architecture test that rejects consumer
operation vocabulary and process execution in the proxy core and daemon wiring.
Provider helper subprocesses and the generic caller-owned CLI helper are explicit
exceptions because they do not define consumer admission policy.

## Related Decisions

- `adr-20260716-dual-library-daemon-modes`
- `adr-20260716-provider-isolation`
- `adr-20260716-local-authenticated-transport`
