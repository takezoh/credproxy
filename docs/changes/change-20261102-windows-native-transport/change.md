---
id: change-20261102-windows-native-transport
kind: change
title: Windows authenticated credential transport
status: active
created: '2026-11-02'
profile: sdd@1
intent: Make native Windows credential delivery possible without weakening identity
  checks.
outcomes:
- On Windows, credproxyd serves the broker API and the admin refresh endpoint on a
  local named pipe restricted to the owner SID and SYSTEM, and authenticates each
  client by its token SID.
- Before sending any token or secret-bearing request, Windows clients verify the pipe
  server's process identity and owner SID.
- On Windows, the 1Password service-account token file and its parent directories
  are verified by owner SID and a restrictive DACL, rejecting reparse points.
scope:
- credproxy/
- cmd/
- providers/onepassword/
- internal/
- go.mod
- go.sum
- README.md
- docs/
non_goals: []
change_classes:
- capability
governance:
  gate: auto
  reasons: []
members:
- role: requirements
  path: changes/change-20261102-windows-native-transport/requirements.md
  required: true
- role: implementation
  path: changes/change-20261102-windows-native-transport/implementation.md
  required: true
- role: verification
  path: changes/change-20261102-windows-native-transport/verification.md
  required: true
promotion: []
unresolved_decisions: []
tags: []
owners: []
relations:
- {type: conformsTo, target: design-credential-broker}
source_paths: []
summary: Native Windows authenticated pipes and protected token access, preserving
  Unix semantics.
updated: '2026-11-02'
---

## Summary

## Closure Notes
