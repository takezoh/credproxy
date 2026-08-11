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
boundaries:
  provides:
  - authenticated HTTP proxy routes and synthetic credential endpoints
  - Provider, Store, container provider, and secret resolver contracts
  consumes:
  - caller-owned credential Providers and hooks
  - host networking, subprocesses, and credential stores
  forbidden:
  - provider-specific authentication flows in the credproxy package
  - mounting long-lived host credential stores into containers
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
- secretenv/resolver.go
summary: Provider-agnostic authenticated credential injection, forwarding, and secret-resolution
  architecture.
updated: '2026-07-16'
---

## Purpose

Broker short-lived credentials to sandboxed processes without giving those processes long-lived host credential material.

## Responsibilities

The core authenticates a client, matches a route, asks a Provider for an Injection, and either returns a synthetic body or forwards a planned request. A configured refresh status triggers one Provider refresh and one retry.

## Boundaries

The `credproxy` package owns HTTP behavior only. `container/` describes per-launch mounts and environment, `providers/` implements reusable backends, `secretenv/` resolves opaque references, and command packages own user-facing processes. `credproxy exec` fetches a daemon-defined route's env map over the Unix socket and injects it into a child process — the caller chooses only the route name, never the refs behind it.

## Invariants

- Credential caching belongs to Providers or hook wrappers.
- The core never learns Anthropic, AWS, gcloud, SSH, or caller-specific concepts.
- Long-lived refresh tokens and private keys remain on the host side of the boundary.
- A route may be restricted to named clients (`AllowedClientIDs`); token id is attribution, the restriction is what makes it authorization.
- A Unix-socket server may require same-UID peers (`RequireSamePeerUID`) as defense-in-depth over the 0600 socket, failing closed where peer credentials cannot be read.
- Hook failures may carry a machine-readable `reason` to the client; only the reason token crosses the boundary, so a hook cannot leak a secret through the error path.

## Collaboration

Routes bind Provider output to HTTP behavior. Container providers can contribute routes, mounts, environment, or bridge specifications independently.

## Failure Responsibility

Authentication failures stop at the proxy boundary. Provider failures return an error response. Secret resolution is all-or-nothing, so a subprocess never receives a partially resolved environment.

## Variability

Providers and routes are extensible. Authentication defaults, retry count, stream behavior, and host-secret isolation are fixed policy.

## Conformance

`go test ./...`, provider-specific tests, `go vet ./...`, and security-focused route/auth tests verify the design.

## Related Decisions

- `adr-20260716-dual-library-daemon-modes`
- `adr-20260716-provider-isolation`
- `adr-20260716-local-authenticated-transport`
