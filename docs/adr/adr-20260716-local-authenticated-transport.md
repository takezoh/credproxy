---
id: adr-20260716-local-authenticated-transport
kind: adr
title: Use local authenticated daemon transport
status: accepted
created: '2026-07-16'
decision_makers:
- repository owner
consequences:
  positive:
  - local clients can reach the broker without receiving long-lived host credentials.
  negative:
  - clients must receive and protect an ephemeral bearer token or constrained socket
    path.
  neutral:
  - TCP and Unix socket transports remain available for different runtimes.
tags: []
owners: []
relations:
- {type: references, target: design-credential-broker}
- {type: references, target: design-daemon-hook-runtime}
source_paths: []
summary: Local TCP and Unix transports enforce explicit authentication and safe socket
  defaults.
updated: '2026-07-16'
---

## Context

Sandboxed processes need a local credential service, but an accidentally open listener or broadly shared host socket would expand credential access.

## Decision

Require bearer authentication for TCP unless unauthenticated operation is explicitly enabled. Default Unix sockets to owner-only permissions, reject destructive replacement of non-socket paths, and issue per-process or per-project credentials.

## Consequences

- Positive: transport access is constrained independently of backend credentials.
- Negative: token and socket distribution become explicit integration work.
- Neutral: containers still receive short-lived outputs through standard HTTP or constrained sockets.

## Alternatives

Loopback-only unauthenticated TCP was rejected because local process and container boundaries still require explicit authorization.
