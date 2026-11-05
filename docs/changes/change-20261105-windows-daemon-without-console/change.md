---
id: change-20261105-windows-daemon-without-console
kind: change
title: Run credproxyd on Windows without a console window
status: done
created: '2026-11-05'
profile: sdd@1
intent: Run credproxyd on Windows without a console window that stops the daemon when
  it is closed, and keep its logs in a file.
outcomes:
- On Windows, credproxyd.exe is built without a console, so a logon task shows no
  window.
- credproxyd starts every helper process (script provider, secretenv hooks, aws, gcloud,
  ssh-agent) without a window.
- credproxyd --log-file appends JSON logs to a file, including startup failures, and
  keeps one previous generation.
scope:
- cmd/credproxyd/
- internal/childproc/
- secretenv/scripthook.go
- providers/awssso/provider.go
- providers/gcloudcli/metadata.go
- providers/sshagent/provider.go
- install.ps1
- tests/test_install_windows.ps1
- architecture/responsibility_test.go
- README.md
- docs/design/design-daemon-hook-runtime.md
- docs/changes/change-20261105-windows-daemon-without-console/
non_goals:
- Running credproxyd as a Windows service
- Changing the Linux and macOS service definitions or their logging
change_classes:
- capability
governance:
  gate: auto
  reasons: []
members:
- role: requirements
  path: changes/change-20261105-windows-daemon-without-console/requirements.md
  required: true
- role: implementation
  path: changes/change-20261105-windows-daemon-without-console/implementation.md
  required: true
- role: verification
  path: changes/change-20261105-windows-daemon-without-console/verification.md
  required: true
promotion:
- target: design-daemon-hook-runtime
  section: invariants
  action: upsert
  item:
    id: INV-003
    statement: On Windows credproxyd has no console. Every helper process it starts
      is started without a window, and its logs go to the file given by --log-file.
    enforcement: test
unresolved_decisions: []
tags: []
owners: []
relations:
- {type: modifies, target: design-daemon-hook-runtime}
source_paths: []
summary: Build credproxyd without a console on Windows, start its helper processes
  without a window, and add --log-file.
updated: '2026-11-05'
evidence_refs:
- type: test
  ref: internal/childproc/childproc_windows_test.go::TestNoWindowAddsCreateNoWindowAndKeepsExistingFlags
- type: test
  ref: cmd/credproxyd/logging_test.go
- type: test
  ref: tests/test_install_windows.ps1 (Windows PowerShell 5.1 and PowerShell 7, 2026-11-05)
- type: test
  ref: go test ./architecture/ (Windows 11, 2026-11-05)
- type: command
  ref: 'Windows 11: logon task re-registered with --log-file; daemon PE subsystem
    GUI, no child console, log file written, credproxy exec route served (2026-11-05)'
- type: source
  ref: internal/childproc/childproc.go
- type: source
  ref: cmd/credproxyd/logging.go
- type: source
  ref: install.ps1
- type: command
  ref: Windows 11 agent-environments update.ps1 -Module credproxy -> exit 0; pulled
    7fa1119, rebuilt, restarted the task (2026-11-05)
promotion_applied_at: '2026-11-05T13:56:22.140279+00:00'
closure:
  closed_at: '2026-11-05T14:11:55.114436+00:00'
  content_hash: sha256:c46df4910528a2843426fe031e5dd4cf551d4f3e00ad46ee6c9a1774a06ee36f
---

## Summary

The consuming environment starts credproxyd on Windows from a logon task in the user's session. As a console program it got a visible window; closing that window stopped the daemon, and its logs, written only to stderr, were lost.

## Closure Notes

The promotion to design-daemon-hook-runtime (INV-003) is applied. Closing first stopped with an exception because this repository's designs still used the older dev-docs format (plain-string boundary items). The docs were then migrated to the current dev-docs contract, and the change was closed.

{% transition from="draft" to="ready" date="2026-11-05" %}
Requirements, implementation and promotion are recorded.
{% /transition %}


{% transition from="ready" to="active" date="2026-11-05" %}
Implemented and tested.
{% /transition %}


{% transition from="active" to="closing" date="2026-11-05" %}
Verified on a Windows 11 host, including deployment through agent-environments.
{% /transition %}
