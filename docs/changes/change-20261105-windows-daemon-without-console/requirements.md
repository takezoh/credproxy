---
change: change-20261105-windows-daemon-without-console
role: requirements
---

<!-- lifecycle is owned by change.md -->

# Run credproxyd on Windows without a console window

## Background

- agent-environments registers a current-user logon task that starts `credproxyd.exe --config ...`. The task runs in the user's interactive session.
- `credproxyd.exe` was a console program. Windows gave it a console window; closing it, or pressing Ctrl+C in it, stopped the daemon until the next logon or setup.
- credproxyd logs only to stderr, so on Windows the logs were visible only in that window and were not kept.
- Linux runs credproxyd as a systemd user service (logs in journald) and macOS as a LaunchAgent (logs in `~/Library/Logs/credproxyd.log`). Neither shows a window.

## Requirements

- R1: On Windows, build `credproxyd.exe` without a console. `credproxy.exe` stays a console program for terminals.
- R2: credproxyd starts every helper process without a window. A console program started from a process without a console otherwise gets a new visible window each time.
- R3: Add `--log-file <path>`. It creates the parent directory, appends JSON logs, records failures that happen before the configured log level is known and the final error, and keeps one previous generation when the file grows past 10 MiB.
- R4: Keep the caller's `GOFLAGS` when the installer adds the Windows build flag.
- R5: Do not run credproxyd as a Windows service. That needs administrator rights and a different account, unlike the per-user services on Linux and macOS.

## Acceptance

- The installer test checks that only the Windows daemon build gets `-ldflags=-H=windowsgui` and that `GOFLAGS` is restored.
- Tests cover the window flag on helper processes, the log file and its rotation, and the JSON log output.
- On a Windows 11 host, the daemon started by the logon task has no console process, writes its log file, and serves routes.
