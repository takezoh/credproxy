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

The `credproxy` package owns HTTP behavior only. `container/` describes per-launch mounts and environment, `providers/` implements reusable backends, `secretenv/` resolves opaque references, and command packages own user-facing processes.

## Invariants

- Credential caching belongs to Providers or hook wrappers.
- The core never learns Anthropic, AWS, gcloud, SSH, or caller-specific concepts.
- Long-lived refresh tokens and private keys remain on the host side of the boundary.

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
