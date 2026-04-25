## Build & Test

```sh
make build          # Build → ./credproxyd
make vet            # go vet ./...
make lint           # golangci-lint
go test ./...       # Run all tests
go test ./internal/server/...  # Run server tests only
go test -run TestName ./...    # Run a specific test
```

## Rules

- Follow the design principles in ARCHITECTURE.md
- Keep files under 500 lines and functions under 50 lines
- No provider-specific logic in the proxy binary — Anthropic, AWS, git knowledge belongs in hook scripts only
- Do not overwrite user config files (~/.config/credproxyd/)
- Always write tests for new features and bug fixes. Do not consider work complete without tests
