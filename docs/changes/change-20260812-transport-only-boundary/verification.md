---
change: change-20260812-transport-only-boundary
role: verification
---

<!-- lifecycle is owned by change.md -->

# Verification

## Gates

- `go test ./architecture`
- `go test ./...`
- `go vet ./...`
- `go build ./...`
- `git diff --check`

The architecture mutation cases must fail for consumer executable policy,
consumer argv policy, operation endpoints, and proxy-core process execution.
