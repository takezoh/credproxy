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
  statement: RequireSamePeerUID admits only same-UID peers over a Unix socket, denies
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
- provider output to upstream request mutation
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
- secretenv/resolver.go
summary: Provider-agnostic authenticated credential injection, forwarding, and secret-resolution
  architecture.
updated: '2026-08-12'
---

## Purpose

Broker short-lived credentials to sandboxed processes without giving those processes long-lived host credential material.

## Responsibilities

The core authenticates a client, matches a route, asks a Provider for an Injection, and either returns a synthetic body or forwards a planned request. A configured refresh status triggers one Provider refresh and one retry.

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
