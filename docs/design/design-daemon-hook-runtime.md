---
id: design-daemon-hook-runtime
kind: design
title: Daemon and hook runtime
status: active
created: '2026-07-16'
scope_type: component
responsibilities:
- id: RESP-001
  statement: Run a shared local proxy whose route providers are external hook commands.
- id: RESP-002
  statement: Validate configuration, constrain hook execution, cache eligible results,
    and manage listeners and shutdown.
invariants:
- id: INV-001
  statement: Hook stdin and stdout use one JSON object per invocation and non-zero
    exit is a provider failure.
  enforcement: contract
- id: INV-002
  statement: Unix sockets default to mode 0600 and a non-socket path is never removed.
  enforcement: test
boundaries:
  provides:
  - credproxyd TCP and Unix-socket service
  - ScriptProvider hook protocol
  consumes:
  - user-owned hook commands and local configuration
  forbidden:
  - interpreting provider-specific hook payload fields in the daemon core
  - silently continuing after hook timeout or malformed output
variability:
  fixed:
  - hook action/request/context envelope and timeout/error semantics
  - local authenticated listener defaults
  free:
  - configured routes, commands, TTLs, and upstreams
capabilities:
- id: cap:credential-daemon
  uniqueness: per-boundary
- id: cap:script-provider
  uniqueness: multiple
failure_responsibilities:
- Invalid config fails startup; hook timeout, exit failure, or malformed JSON fails
  the request.
trust_boundaries:
- authenticated local client to daemon
- daemon process to user-owned hook subprocess
compatibility_policies:
- Hook protocol additions must preserve existing fields and one-line JSON framing.
tags: []
owners: []
relations: []
source_paths:
- cmd/credproxyd/main.go
- cmd/credproxyd/config
- cmd/credproxyd/providers/script
summary: Shared local daemon and bounded JSON hook execution over the common Provider
  contract.
---

## Purpose

Provide credential routes as a reusable host service and permit backend changes through scripts without recompiling the daemon.

## Responsibilities

`credproxyd` owns configuration, signals, listeners, and ScriptProvider construction. ScriptProvider owns command execution, timeout, TTL cache, and singleflight deduplication.

## Boundaries

Hooks own backend login and refresh logic. The daemon only maps their generic injection output onto the same Provider contract used in-process.

## Invariants

TCP is authenticated by default, Unix sockets are owner-only by default, and hook failures are observable as request failures.

## Collaboration

Configuration binds a route to credential and refresh commands. The proxy core performs forwarding; ScriptProvider supplies Injection values.

## Failure Responsibility

Startup rejects unsafe listener configuration. Runtime hook errors are bounded by timeout and returned without credential fallback.

## Variability

Users may add hook-backed providers and routes through config while the protocol and security defaults remain fixed.

## Conformance

Daemon config, ScriptProvider, listener, and end-to-end hook tests verify this contract.

## Related Decisions

- `adr-20260716-dual-library-daemon-modes`
- `adr-20260716-local-authenticated-transport`
