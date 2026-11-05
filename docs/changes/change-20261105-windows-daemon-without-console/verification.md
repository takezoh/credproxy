---
change: change-20261105-windows-daemon-without-console
role: verification
---

<!-- lifecycle is owned by change.md -->

# Verification

Windows 11 host, Go toolchain from the user PATH, 2026-11-05.

- `go test ./cmd/credproxyd/ ./internal/childproc/ ./architecture/`: passed. `internal/childproc` runs a real `cmd.exe` with the flag and reads its output.
- `go test ./...`: 24 failing tests, all in packages that also fail on the unchanged base (1dd81e3) on this host (awssso and gcloudcli fakes are POSIX scripts, among others). The base had 25; the one fewer is the architecture test fixed here. No test fails only with this change.
- `tests/test_install_windows.ps1`: passed under Windows PowerShell 5.1 and PowerShell 7. Against the previous installer it fails at the credproxyd `GOFLAGS` check.
- Real build with `install.ps1` into a temporary `BINDIR`: `credproxyd.exe` has PE subsystem 2 (GUI) and `credproxy.exe` subsystem 3 (console). Running the windowless daemon with a missing config and `--log-file` exited 1 and wrote the error as one JSON line to the log file.
- Deployed through agent-environments on the same host: the logon task was re-registered with `--log-file %LOCALAPPDATA%\credproxyd\credproxyd.log` and started. The running daemon's parent is the Task Scheduler service, it has no child console process, and the log file records startup and the named-pipe listener. `credproxy exec --route dev-skills-env -- cmd.exe /d /c "exit 7"` ran the child after fetching the route (exit status 7), and an unknown route returned 404.
- After pushing 7fa1119, agent-environments `update.ps1 -Module credproxy` pulled the managed checkout, rebuilt, and restarted the task (exit 0). The restarted daemon is a GUI-subsystem process with no child console, its log file gained a new startup record, and `credproxy exec --route dev-skills-env` again ran the child (exit status 7).
- Not exercised live: the routes on this host use the 1Password provider only, so no helper process was started by the deployed daemon. The window flag is covered by the `internal/childproc` test.
