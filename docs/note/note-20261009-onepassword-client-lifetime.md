---
id: note-20261009-onepassword-client-lifetime
kind: note
title: Retain the 1Password SDK client during secret resolution
status: draft
created: '2026-10-09'
tags: []
owners: []
relations: []
source_paths:
- providers/onepassword/provider.go
- providers/onepassword/sdk_lifetime_test.go
summary: Retain the SDK parent client and keep it alive through Resolve so its finalizer
  cannot invalidate the native client ID.
---

## Finding and correction

The resolver returned `client.Secrets()` without retaining `*sdk.Client`. In SDK v0.4.1, the parent client owns a finalizer that releases the underlying client ID, while SecretsAPI does not retain that parent. Garbage collection could therefore leave a reachable resolver with an invalid client ID.

The resolver now owns the SDK client and uses `runtime.KeepAlive` after each Resolve call. Credentials, routes, cache policy and user configuration are unchanged.

## Verification

- `go test ./...` passed, including the mandatory architecture tests.
- `go vet ./...` passed.
- `go test -race -count=20 ./providers/onepassword` passed.
- Restoring the previous lifetime behavior made the regression test fail with `client released while resolver is reachable`; the fixed implementation was then restored.
- `golangci-lint` was unavailable on the test PATH.

The fixed broker binary was installed with the previous binary backed up, and the user service was restarted. After SDK initialization, `dsh acp --help` completed successfully through the existing credential hook. This verifies live credential delivery and CLI startup; no model prompt was submitted. No secret values or user configuration were changed.
