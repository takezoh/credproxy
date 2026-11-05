---
change: change-20261105-windows-daemon-without-console
role: implementation
---

<!-- lifecycle is owned by change.md -->

# Implementation

- `internal/childproc` provides `NoWindow(*exec.Cmd)`. On Windows it adds `CREATE_NO_WINDOW` (keeping existing creation flags) and sets `HideWindow`; elsewhere it does nothing. Every `exec.Command` in credproxyd's process tree calls it: the script provider, secretenv's script hook, the aws and gcloud helpers, and ssh-agent / ssh-add.
- `cmd/credproxyd/logging.go` owns logging. `logOutput` defaults to stderr; `openLogFile` creates the parent directory (0700), moves a file over `maxLogBytes` (10 MiB) to `<path>.1`, and opens it for append (0600). `initLogger` moved here and writes JSON to `logOutput`.
- `run` parses `--log-file`. Without it, behavior is unchanged. With it, `run` opens the file, installs a default JSON logger before the config is loaded, and logs the final error from `serve` (the former body of `run`).
- `install.ps1` sets `GOFLAGS` to the caller's value plus `-ldflags=-H=windowsgui` only for `credproxyd.exe` on Windows, and restores the caller's value afterwards. Using `GOFLAGS` keeps the `go build -o <out> <package>` arguments unchanged.
- `architecture/responsibility_test.go` compares the walked path with `/` separators. On Windows the `cmd/credproxyd/providers` exclusion never matched, so the fitness function failed there even on the unchanged base.
