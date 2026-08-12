---
change: change-20260812-transport-only-boundary
role: implementation
---

<!-- lifecycle is owned by change.md -->

# Implementation

## Changes

1. Revert the closed-operation feature.
2. Adopt the transport-only ADR and promote the consumer-command invariant.
3. Add an AST/source architecture test with narrow exceptions for provider
   acquisition and the generic caller-owned CLI helper.
4. Update README terminology.

The test checks ownership semantics, not a repository-wide ban on `os/exec`.
Provider acquisition subprocesses remain valid and are tested in their owning
packages.
