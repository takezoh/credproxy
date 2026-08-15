---
change: change-20260815-exact-route-upstream-path
role: verification
---

<!-- lifecycle is owned by change.md -->

# Verification

## Verification

- `go test ./credproxy/...` — pass
- `go test ./...` — pass
- installed runtimeで`ctx sync` — `sync ok via Context Fabric service`
