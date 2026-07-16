---
id: adr-20260716-dual-library-daemon-modes
kind: adr
title: Support library and shared daemon modes
status: accepted
created: '2026-07-16'
decision_makers:
- repository owner
consequences:
  positive:
  - callers can choose process-local lifecycle or a reusable shared service.
  negative:
  - two deployment surfaces and their integration tests must remain consistent.
  neutral:
  - both modes share the same Server and Provider semantics.
tags: []
owners: []
relations:
- {type: introduces, target: design-credential-broker}
- {type: introduces, target: design-daemon-hook-runtime}
source_paths: []
summary: The broker supports embedded library and shared daemon deployment modes.
updated: '2026-07-16'
---

## Context

Embedded callers need ephemeral listeners and direct Provider implementations, while multiple clients benefit from a stable host daemon and script-configured backends.

## Decision

Support both an in-process Go library and the `credproxyd` shared daemon. Reuse the same Server and Provider contract; adapt external scripts through ScriptProvider.

## Consequences

- Positive: both embedded and shared-service deployments are first-class.
- Negative: lifecycle, configuration, and documentation exist on two surfaces.
- Neutral: provider behavior remains outside the HTTP core in both modes.

## Alternatives

A daemon-only API would make embedded lifecycle and ephemeral-port use unnecessarily indirect. A library-only API would duplicate host services in every client.
