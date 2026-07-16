---
id: design-provider-boundaries
kind: design
title: Credential provider boundaries
status: active
created: '2026-07-16'
scope_type: area
responsibilities:
- id: RESP-001
  statement: Define provider seams for HTTP injections and container launch contributions.
- id: RESP-002
  statement: Keep backend-specific acquisition, caching, allowlists, and lifecycle
    in provider packages.
invariants:
- id: INV-001
  statement: No provider-specific logic may enter the credproxy HTTP core.
  enforcement: review
- id: INV-002
  statement: Containers receive only declared short-lived credentials or constrained
    signing sockets.
  enforcement: test
boundaries:
  provides:
  - HTTP Provider Get/Refresh contract
  - container Provider Init/Routes/ContainerSpec contract
  consumes:
  - host AWS, gcloud, and ssh-agent facilities
  forbidden:
  - bind-mounting host refresh-token stores or unrestricted host SSH agent sockets
variability:
  fixed:
  - caller/provider ownership of backend knowledge and cache policy
  free:
  - HTTP route, bind mount, bridge, or combined delivery mechanism
capabilities:
- id: cap:credential-provider
  uniqueness: multiple
failure_responsibilities:
- Each provider validates its configuration and cleans up its host-side processes
  and sockets.
trust_boundaries:
- host credential backend to provider package
- provider-generated short-lived artifact to container
compatibility_policies:
- New providers implement existing public interfaces instead of adding backend switches
  to the core.
tags: []
owners: []
relations: []
source_paths:
- container/provider.go
- providers/awssso/README.md
- providers/gcloudcli/README.md
- providers/sshagent/README.md
summary: Backend-specific acquisition and container delivery remain isolated in provider
  packages.
---

## Purpose

Allow independent credential backends while preserving a provider-agnostic HTTP and container integration core.

## Responsibilities

HTTP Providers decide injection and refresh behavior. Container Providers describe routes, environment, mounts, bridge processes, and lifecycle needed for one project launch.

## Boundaries

AWS SSO serves synthetic credential-process JSON through an authenticated route. gcloud exposes a host metadata emulator and synthetic config. sshagent exposes a per-project socket containing only declared keys.

## Invariants

Host refresh tokens and private key files are never mounted into the container. Per-project authorization remains provider-owned.

## Collaboration

The container runner materializes each `container.Spec`; the proxy registers optional routes; provider contexts govern cleanup.

## Failure Responsibility

Providers reject unauthorized project/profile combinations and surface host command failures. Missing optional key files may be warned and skipped only where the provider contract explicitly allows it.

## Variability

A provider may use routes, mounts, bridges, periodic refresh, or a combination without changing the core.

## Conformance

Provider package tests and integration tests verify configuration, access control, token delivery, and cleanup.

## Related Decisions

- `adr-20260716-provider-isolation`
