# Architecture

## Overview

`localclaw` is a monolithic Go CLI process with strict local-only runtime boundaries.

- Entry: `cmd/localclaw`
- Core orchestration: `internal/runtime`
- Config and policy: `internal/config`
- Capability modules:
  - `internal/memory`
  - `internal/workspace`
  - `internal/skills`
  - `internal/cron`
  - `internal/heartbeat`
- Channel adapters:
  - `internal/channels/slack`
  - `internal/channels/signal`
- LLM adapter:
  - `internal/llm/claudecode`

## Process model

Single process only. No background daemon mode, no network listeners, and no external service host process.

## Boundaries

- Inbound/outbound channels are represented as local adapters.
- LLM execution is delegated to local Claude Code CLI.
- Scheduler and heartbeat are local in-process primitives.
- Memory and workspace operate on local filesystem paths.

## Future decomposition (in-process)

As features grow, composition remains package-level in the same binary, preserving local-only controls and startup policy enforcement.
