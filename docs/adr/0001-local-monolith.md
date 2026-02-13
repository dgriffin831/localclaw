# ADR 0001: Local Monolithic CLI Architecture

## Status

Accepted

## Context

The project targets secure enterprise deployments where minimizing network-exposed attack surface is mandatory. We need a practical baseline preserving key OpenClaw capabilities without introducing distributed infrastructure.

## Decision

Implement `localclaw` as a single Go CLI process with strict local-only policy checks.

- No server/gateway modes.
- No port listeners.
- Local Claude Code CLI as the primary LLM path.
- Capability modules remain in-process packages.

## Consequences

Positive:

- Smaller attack surface.
- Simpler deployment and auditability.
- Lower operational complexity.

Trade-offs:

- Fewer scaling options.
- Integrations must fit local process model.
