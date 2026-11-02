---
change: change-20261102-windows-native-transport
role: implementation
---

<!-- lifecycle is owned by change.md -->

# Implementation

## Content

### Implemented transport

- Added a Windows named-pipe adapter for the public broker and separate admin
  refresh endpoint. Pipe names are scoped to the current Windows SID. Listeners
  use an owner/SYSTEM DACL, reject remote clients, and use first-instance
  creation so an occupied pipe fails closed.
- A client first verifies the server process token SID against the current user
  and verifies the executable file identity, then sends a fixed non-secret
  preface. The listener reads that
  preface with a five-second deadline before impersonating the last pipe
  message on a locked OS thread, verifies the peer SID, and returns a fixed
  acknowledgement. Missing identities, invalid handshakes, and timeouts fail
  closed before HTTP handling. This ordering follows the
  [Microsoft `ImpersonateNamedPipeClient` contract](https://learn.microsoft.com/en-us/windows/win32/api/namedpipeapi/nf-namedpipeapi-impersonatenamedpipeclient),
  which uses the last message read for impersonation and requires requests to
  be rejected when impersonation fails. Unix
  `RequireSamePeerUID` remains based on the real Unix UID; Windows exposes the
  explicit `RequirePeerSID` configuration field instead.
- Windows CLI clients (`env`, `exec`, and `refresh`) use the native endpoints.
  Before `net/http` can send request bytes, the dialer checks the pipe server
  process token SID and compares the running image's file identity with the
  caller-supplied or adjacent protected `credproxyd.exe`. The Unix dialer and
  its existing socket defaults remain in a separate platform file.
- The 1Password token reader now has platform adapters. Windows opens local
  drive paths component by component without following reparse points, checks
  the token and its parent/grandparent owner SID and DACL, and reads through the
  validated file handle. It holds volume-root-to-leaf directory handles without
  write/delete sharing through token reading and server-image identity checks,
  preventing path replacement during admission. Unix keeps its prior UID and
  mode checks.
- Added `credproxy connect`, a generic stdin/stdout byte-stream relay for the
  broker. It holds one authenticated connection and relays bytes unchanged for
  host bridges, without adding route, method, or consumer policy.
- Added Windows-only tests for peer SID capture/rejection, missing identity,
  first-instance pipe squatting, zero-byte delivery to an unverified server,
  protected file ACL/reparse/mutation behavior, public broker auth, and admin
  refresh through the CLI.

### Image identity and ACL policy

Secret confidentiality and executable integrity use separate pure DACL
decisions over security data read from held Windows handles. The token policy
requires the current SID to own the token and accepts allow ACEs only for that
SID or SYSTEM. The managed-image policy accepts owners and full-access writers
from the current SID, SYSTEM, Administrators, and TrustedInstaller; all other
allow ACEs must be limited to read/execute rights. Unsupported ACE forms fail
closed. Path acquisition is shared, and each policy validates the file plus
its parent and grandparent while all opened ancestors remain held without
write/delete sharing.

The client compares the server process token SID with its own current user SID
independently of the expected image owner, then compares the running image's
stable file identity with the validated managed executable. This permits a
SYSTEM-, Administrators-, or TrustedInstaller-owned Program Files installation
without treating its owner as the daemon's process identity.

The active `docs/design/design-credential-broker.md` now records the Windows
named-pipe admission, server-image verification, and token-file confidentiality
invariants alongside the existing Unix `RequireSamePeerUID` contract. The
accepted consumer-policy ADR and implementation requirements remain unchanged.

The parent implementation can promote or refine these statements after review.

### Dependency and compatibility

Added `github.com/Microsoft/go-winio` v0.6.4 for Windows named-pipe listener
and dial support. That release requires Go 1.26, so `go.mod` now declares Go
1.26 and `golang.org/x/sys` / `golang.org/x/sync` were updated by module
resolution. Dependency preparation was completed separately from test runs.
