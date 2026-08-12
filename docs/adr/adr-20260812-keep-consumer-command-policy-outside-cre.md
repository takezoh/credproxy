---
id: adr-20260812-keep-consumer-command-policy-outside-cre
kind: adr
title: Keep consumer command policy outside credproxy
status: accepted
created: '2026-08-12'
decision_makers:
- takezoh
confirmation: go test ./architecture
consequences:
  positive:
  - Consumer operation semantics stay with the consumer and credproxy remains reusable.
  negative:
  - Consumers without a protocol injection point cannot claim not-held credential
    delivery.
  neutral:
  - Fixed provider helper commands remain allowed as credential acquisition implementations.
tags: []
owners: []
relations: []
source_paths:
- credproxy/types.go
- credproxy/server.go
- architecture/responsibility_test.go
summary: Credential acquisition and transport remain in credproxy; consumer command
  policy is forbidden and enforced by architecture tests.
---

## Context

A closed-operation feature made credproxy validate an executable, a finite argv
grammar, environment variables, and then run a Context Fabric command. This kept a
secret out of the caller but transferred the consumer's operation policy into the
credential broker. The same design mistake had been rejected repeatedly, but no
repository gate represented the boundary.

## Decision

credproxy owns credential acquisition, authenticated transport, and protocol-level
injection. It MUST NOT select, admit, validate, or execute a credential consumer
operation.

Fixed credential-provider helpers remain allowed: their output is the credential
injection and their lifecycle belongs to the provider boundary. A generic
caller-selected delivery helper may execute the caller's command, but credproxy MUST
NOT decide which command is allowed or attach consumer-specific semantics to it.

Consumers requiring `not-held` delivery MUST expose a protocol injection point.
The consumer repository owns the operation and authorization semantics behind that
protocol.

## Consequences

{% consequence kind="positive" %}
Consumer operation semantics have one owner and the broker stays provider-neutral.
{% /consequence %}

{% consequence kind="negative" %}
A command-only consumer without a protocol injection point cannot be upgraded to
`not-held` by adding an executable policy to credproxy.
{% /consequence %}

{% consequence kind="neutral" %}
Provider helpers, env delivery, and caller-selected generic exec remain separate
delivery mechanisms; none is evidence of consumer admission.
{% /consequence %}

Confirmation: `go test ./architecture` rejects the closed-operation API/config
vocabulary and consumer process execution in the proxy core and daemon wiring.
