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

- Follow the design principles in ARCHITECTURE.md
- Keep files under 500 lines and functions under 50 lines
- No provider-specific logic in `credproxy/` (the HTTP core) — backend knowledge belongs in `providers/<name>/` (Go) or hook scripts (credproxyd)
- Do not overwrite user config files (~/.config/credproxyd/)
- Always write tests for new features and bug fixes. Do not consider work complete without tests
