---
id: adr-20260716-provider-isolation
kind: adr
title: Isolate provider-specific behavior
status: accepted
created: '2026-07-16'
decision_makers:
- repository owner
consequences:
  positive:
  - new credential backends can be added without changing the proxy core.
  negative:
  - providers must own caching, access control, and backend-specific tests.
  neutral:
  - container delivery may use routes, mounts, bridges, or combinations.
tags: []
owners: []
relations:
- {type: introduces, target: design-provider-boundaries}
source_paths: []
summary: Credential backend knowledge is excluded from the HTTP proxy core.
updated: '2026-07-16'
---

## Context

Credential systems differ in refresh, cache, access-control, and delivery behavior. Encoding those differences in the HTTP core would couple security-sensitive backend changes to routing and streaming behavior.

## Decision

Keep the `credproxy` package provider-agnostic. Backend packages implement Provider contracts and own acquisition, caching, allowlists, lifecycle, and provider-specific delivery.

## Consequences

- Positive: core behavior remains small and independently testable.
- Negative: provider implementations carry more explicit responsibility.
- Neutral: common behavior is shared through interfaces rather than backend switches.

## Alternatives

A provider enum and backend branches in the core were rejected because every new backend would modify the security-critical forwarding package.
