---
change: change-20260812-transport-only-boundary
role: requirements
---

<!-- lifecycle is owned by change.md -->

# Requirements

## Functional requirements

- FR-1: credproxy MUST NOT expose an API or configuration that selects a consumer
  executable, subcommand, argv grammar, environment, or process outcome.
- FR-2: credproxy MUST NOT execute a consumer operation in its proxy core or daemon
  wiring.
- FR-3: credential-provider helpers MAY execute fixed acquisition commands.
- FR-4: env, resolve, and generic caller-selected exec delivery MUST remain available
  and MUST NOT be treated as consumer admission.
- FR-5: protocol injection routes MUST remain compatible.
- FR-6: repository verification MUST reject reintroduction of consumer operation
  ownership.

## Acceptance

- The old operation endpoint, CLI, public types, config, and tests are absent.
- HTTP injection, providers, env, resolve, and generic exec tests pass.
- A mutation adding closed-operation vocabulary or core process execution fails the
  architecture test.
