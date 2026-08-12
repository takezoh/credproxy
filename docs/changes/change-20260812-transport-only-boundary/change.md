---
id: change-20260812-transport-only-boundary
kind: change
title: Restore credential proxy transport-only boundary
status: done
created: '2026-08-12'
profile: sdd@1
intent: Remove consumer command admission and execution from credproxy and make the
  boundary mechanically enforceable.
outcomes:
- Closed operation APIs and configuration are removed.
- Persistent design and an architecture test prevent reintroduction.
scope:
- credproxy closed-operation implementation
- credproxy design and ADR
- repository architecture conformance
non_goals:
- Removing credential-provider helper subprocesses
- Removing env, resolve, or generic caller-selected exec delivery
- Implementing a particular consumer operation
change_classes:
- responsibility
- boundary
- invariant
governance:
  gate: hard
  reasons:
  - User explicitly required the responsibility correction and mechanical prevention.
  approval_evidence: user instruction 2026-08-12 requiring removal of consumer command
    policy from credproxy and a repository-enforced prevention mechanism.
members:
- role: requirements
  path: changes/change-20260812-transport-only-boundary/requirements.md
  required: true
- role: implementation
  path: changes/change-20260812-transport-only-boundary/implementation.md
  required: true
- role: verification
  path: changes/change-20260812-transport-only-boundary/verification.md
  required: true
evidence_refs:
- type: command
  ref: go test ./...
- type: command
  ref: go vet ./...
- type: command
  ref: go build ./...
- type: command
  ref: git diff --check
promotion:
- target: design-credential-broker
  section: invariants
  action: upsert
  item:
    id: INV-008
    statement: Consumer command selection, executable admission, argv policy, and
      consumer process execution never enter credproxy APIs, configuration, or daemon
      runtime.
    enforcement: test
  reason: This responsibility boundary must constrain future features after the change
    closes.
unresolved_decisions: []
tags: []
owners: []
relations:
- {type: modifies, target: design-credential-broker}
source_paths:
- credproxy
- cmd/credproxy
- cmd/credproxyd
- architecture
summary: credproxy から consumer command policy と closed operation を除去し、architecture
  test で再導入を拒否する。
updated: '2026-08-12'
promotion_applied_at: '2026-08-12T10:45:10.622070+00:00'
closure:
  closed_at: '2026-08-12T10:45:29.888954+00:00'
  content_hash: sha256:563df903b527a1e0d1080fac0622b1577db2076498698562a9df1f5f52644f1f
---

## Summary

Restore credproxy to credential acquisition, transport, and injection. Context
Fabric and other consumers own their operations.

## Closure Notes
