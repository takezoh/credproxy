## Build & Test

```sh
make build          # Build → ./credproxyd
make vet            # go vet ./...
make lint           # golangci-lint
go test ./...                     # Run all tests
go test ./credproxy/...           # Run proxy core tests only
go test ./providers/awssso/...    # Run a specific provider's tests
go test -run TestName ./...       # Run a specific test
```

## Rules

- Follow the active governing designs in `docs/design/` and accepted decisions in `docs/adr/`
- Keep files under 500 lines and functions under 50 lines
- No provider-specific logic in `credproxy/` (the HTTP core) — backend knowledge belongs in `providers/<name>/` (Go) or hook scripts (credproxyd)
- Do not overwrite user config files (~/.config/credproxyd/)
- Always write tests for new features and bug fixes. Do not consider work complete without tests
- Keep usage and provider setup in README files; keep durable responsibilities, boundaries, and invariants in dev-docs
- Update dev-docs frontmatter, relations, and lifecycle with the dev-docs CLI where possible, then run docs lint

## Credential broker responsibility boundary

Before changing an API, route/config schema, daemon behavior, or execution helper,
read the governing principle in
[`docs/design/design-credential-broker.md`](docs/design/design-credential-broker.md#boundaries)
and the accepted decision
[`docs/adr/adr-20260812-keep-consumer-command-policy-outside-cre.md`](docs/adr/adr-20260812-keep-consumer-command-policy-outside-cre.md).

credproxy owns credential acquisition, authenticated transport, delivery, and
protocol-level injection. It must not select, admit, validate, or execute a credential consumer operation. Fixed credential-provider helpers are allowed because
they acquire credentials; the generic caller-selected `exec` delivery surface is
allowed only while credproxy does not decide which consumer command is permitted or
attach consumer-specific semantics to it. A consumer requiring not-held delivery
must expose a protocol injection point and own the operation behind that protocol.

`go test ./architecture` is the mandatory fitness function for this boundary.
