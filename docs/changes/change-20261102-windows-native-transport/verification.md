---
change: change-20261102-windows-native-transport
role: verification
---

<!-- lifecycle is owned by change.md -->

# Verification

## Content

### Passed on Linux

- `docs lint` — passed; 16 documents indexed and no warnings.
- `go test ./architecture` — passed.
- `go test ./credproxy ./cmd/credproxy ./cmd/credproxyd/... ./providers/onepassword ./internal/nativeipc` — passed.
- `go test ./credproxy ./cmd/credproxy ./cmd/credproxyd/... ./providers/onepassword ./internal/nativeipc ./internal/winsecurefile` — passed on the revised candidate. This executes the platform-independent ACL-policy tests on Linux; it does not execute Windows ACL syscalls.
- The `cmd/credproxy` suite includes byte-stream relay checks for unchanged
  binary data, output errors, remote EOF, and cancellation — passed on Linux.
- `go build ./cmd/...` — passed.
- `env GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/...` — passed.
- `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c -o /tmp/credproxy-nativeipc.test.exe ./internal/nativeipc` — passed; Windows test sources compiled and were not executed.
- `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c -o /tmp/credproxy-winsecurefile.test.exe ./internal/winsecurefile` — passed; Windows ACL/reparse/held-handle tests compiled and were not executed.
- Windows test sources compiled with `go test -c` for `./credproxy`,
  `./internal/nativeipc`, `./internal/winsecurefile`, `./cmd/credproxy`,
  `./cmd/credproxyd/admin`, `./cmd/credproxyd/config`, and
  `./providers/onepassword` — passed. These
  commands produced Windows test binaries; they did not execute the tests.
- The Windows test sources also include bounded preface timeout and directory
  ancestry mutation checks; they compiled but were not executed here.

### Not performed

Native Windows tests were not run because this implementation host is Linux.
The pipe ACL behavior, Windows impersonation/reversion, file ACL evaluation,
reparse handling, and admin/CLI integration therefore have compile-time
coverage only and still need execution on Windows.

Native test selectors for a Windows host:

- `go test ./internal/nativeipc -run 'TestPipeListener|TestPipeHandshake|TestImpersonatedSID|TestListenPipe|TestDialPipe'`
- `go test ./internal/winsecurefile -run 'TestOpen'`
- `go test ./credproxy -run 'TestServer_namedPipe'`
- `go test ./cmd/credproxy -run 'TestRefreshUsesVerifiedNativeAdminPipe'`
- `go test ./cmd/credproxyd/admin -run 'TestDefaultSocketPathUsesCurrentSID'`
- `go test ./cmd/credproxyd/config -run 'TestLoadDefaultsToSIDScopedBrokerPipe'`
