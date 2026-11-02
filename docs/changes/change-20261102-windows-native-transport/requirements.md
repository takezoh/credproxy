---
change: change-20261102-windows-native-transport
role: requirements
---

# Windows authenticated credential transport

Implement native Windows credential transport for the public broker HTTP API, daemon administrative refresh, and all CLI env/resolve/exec/refresh consumers. No WSL or unauthenticated TCP substitute. The purpose is preserving credential isolation and delivery semantics on Windows, with Linux/macOS regression safety.

- Named pipe listener uses explicit owner-SID and SYSTEM DACL, local clients only and first-instance admission. Reject squatted pipe conflicts. Authenticate accepted clients by impersonated token SID; missing/failed identity fails closed.
- Before any token or secret-bearing request, the native client verifies pipe server process identity/token owner SID and the expected protected managed server executable. Require an explicit expected server executable/identity input if necessary; never trust a pipe name or PID alone. A wrong/squatted server receives zero request bytes.
- Preserve RequireSamePeerUID as Unix-only per governing INV-005; Windows policy uses an explicit SID equivalent, never a synthetic UID/hash. Update config/README/design for current native fields without altering provider/route/token semantics or existing defaults.
- Cover both public API and separate admin listener, readiness checks, CLI native default location and connection paths. Do not bypass routing/client-ID/token authorization.
- 1Password protected token file and parent/grandparent directory verification require platform implementation. Windows verifies owner SID and restrictive effective DACL, rejects reparse components, reads from held validated file handle. Do not merely split syscall.Stat_t or skip Unix mode checks on Windows. Unix behavior unchanged. Baseline GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/... currently fails provider.go:282 undefined syscall.Stat_t.
- Preserve no credential consumer executable/argv admission in broker or daemon. Validating the broker endpoint executable is transport authentication only, never consumer policy. Do not expose raw secrets or secret paths in logs, test artifacts or assistant output.
- Go files under500 lines/functions under50. Use build-tagged platform adapters. Depend on established Go Windows pipe helper if justified and latest dependency. No actual daemon/home provisioning, login, network credential calls, commits or pushes.
- Run focused ordinary Go tests, mandatory go test ./architecture, Linux build and Windows cross-build. Add native Windows tests for correct SID, wrongSID, missing identity, squatted pipe zero-byte request, ACL/reparse/file mutation, admin+CLI integration. On Linux distinguish compile checks and fake adapter coverage from native execution; clearly report Windows unperformed.

Scope: credproxy public transport/identity, cmd/credproxyd admin/config/wiring, cmd/credproxy clients, providers/onepassword protected token platform checks, lowlevel native IPC package, related tests/dependency files and owning docs. Do not add consumer-specific semantics or change unrelated providers.
