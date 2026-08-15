---
id: change-20260815-exact-route-upstream-path
kind: change
title: Preserve exact upstream paths for exact proxy routes
status: done
created: '2026-08-15'
profile: sdd@1
intent: exact route requestをServeMux redirectとReverseProxy path joinで末尾slash付きへ変えず、configured
  upstream endpointへそのままforwardする。
outcomes:
- exact route pathとsubtree pathをredirectなしで処理する。
- empty stripped pathではconfigured upstream pathを末尾slashなしで保持する。
scope:
- credproxy/server.go
- credproxy/route.go
- credproxy/route_test.go
non_goals:
- route prefix stripping semanticsの変更
- providerまたはcredential contractの変更
change_classes:
- behavior
governance:
  gate: auto
  reasons: []
members:
- role: requirements
  path: changes/change-20260815-exact-route-upstream-path/requirements.md
  required: true
- role: implementation
  path: changes/change-20260815-exact-route-upstream-path/implementation.md
  required: true
- role: verification
  path: changes/change-20260815-exact-route-upstream-path/verification.md
  required: true
evidence_refs:
- type: command
  ref: go vet ./...
- type: command
  ref: go build ./...
- type: test
  ref: go test ./...
- type: command
  ref: dev-docs lint --conformance
- type: command
  ref: installed ctx sync succeeded and ctx doctor reported all checks ok with 39
    fresh entities
promotion:
- action: none
  reason: This is a localized routing bug fix and introduces no new persistent design
    rule.
unresolved_decisions: []
tags:
- proxy
- routing
owners: []
relations: []
source_paths:
- credproxy/server.go
- credproxy/route.go
- credproxy/route_test.go
summary: Prevent exact route requests from gaining a trailing slash before upstream
  forwarding.
updated: '2026-08-15'
closure:
  closed_at: '2026-08-15T04:01:15.655798+00:00'
  content_hash: sha256:7c75783e48a75fadfcd5fd5313e97e3bd8f4aea07ec47d721384603bd0a6aa6b
---

## Summary

`/sync`のようなexact requestが`/sync/`へredirectされ、upstream endpointにも余分な
slashが付いて404になる経路を修正する。

## Closure Notes


{% transition from="draft" to="ready" date="2026-08-15" %}
Requirements, implementation scope, and regression verification are complete.
{% /transition %}


{% transition from="ready" to="active" date="2026-08-15" %}
Implementation and full repository verification are complete.
{% /transition %}


{% transition from="active" to="closing" date="2026-08-15" %}
Exact-route behavior and installed host acceptance are verified.
{% /transition %}
