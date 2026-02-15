# Hybrid Tools + Skills Runtime Spec

## Status

Draft v1.0

## Problem / Motivation

`localclaw` currently has:

- local runtime memory tools (`memory_search`, `memory_get`) and a tool registry
- prompt-level tool instructions injected into user input
- no model-driven tool-call orchestration loop
- no real skills loader/eligibility/snapshot pipeline (skills registry is a stub)

This creates a mismatch against the desired design:

- localclaw-owned tools should be authoritative
- delegated provider/client tools should be possible, but policy-gated
- skills should be localclaw-governed and provider-agnostic

We need an implementation plan that preserves local-only, single-process boundaries while enabling hybrid tool execution.

## Goals

1. Define a hybrid execution model where local tools are authoritative and delegated tools are optional.
2. Add provider-agnostic runtime interfaces that support structured tool-call events when available.
3. Preserve current behavior for non-structured providers (prompt-injected fallback path).
4. Introduce localclaw-managed skills loading/filtering/snapshot prompt injection.
5. Add explicit policy controls for local vs delegated tools.
6. Keep runtime local-only and single-process.

## Out of Scope

- HTTP/gateway/server runtime inside `localclaw`.
- Direct remote MCP/gateway bridges.
- Full parity with OpenClaw channel/plugin tool ecosystem.
- Automatic remote skill registry installation in v1.
- Multi-provider fallback orchestration in one run loop (outside existing provider selection specs).

## Constraints

- Go `1.17`.
- `cmd/localclaw` single-process only.
- Local subprocess model execution only.
- Startup/runtime boundaries in `internal/runtime` remain centralized.
- Existing memory tool behavior and tests must not regress.

## Current State Summary

- Skills registry is currently a no-op (`Load` returns nil, `List` returns empty).  
  `internal/skills/registry.go`
- Tool registry only includes memory tools and runtime executes them directly.  
  `internal/skills/registry.go`  
  `internal/runtime/tools.go`
- Prompt assembly injects memory recall policy + tool schema text, then sends one composed prompt to Claude CLI.  
  `internal/runtime/tools.go`  
  `internal/runtime/app.go`  
  `internal/llm/claudecode/client.go`
- No runtime loop exists to receive model tool-call events and feed tool results back to the model.

## Proposed Design

### 1) Tool Ownership Model

Tool classes:

- `local`: implemented/executed by localclaw (authoritative path)
- `delegated`: declared by provider/client and executed outside localclaw runtime

Rules:

- Local tools are always mediated by localclaw policy and execution.
- Delegated tools are disabled by default.
- Delegated tools can be enabled only via explicit allowlist policy.
- Any unknown tool call is rejected with a structured error result.

### 2) Provider-Agnostic LLM Runtime Contract

Introduce a shared `internal/llm` contract package:

- stream events: text delta/final + optional structured tool-call events
- provider capability flags (e.g. supports structured tool calls)
- request options including:
  - system context
  - tool definitions
  - skill prompt block
  - session metadata

Compatibility mode:

- For providers without structured tool support (current Claude CLI path), runtime continues using prompt-injection fallback.
- Structured providers can opt into evented tool-call orchestration when available.

### 3) Tool Execution Loop

Add a runtime run-loop abstraction:

1. Build session context (bootstrap + skill prompt + tool defs + user input).
2. Start model run.
3. On tool-call event:
   - evaluate policy
   - route to local executor or delegated handler
   - emit structured tool result back into model run (when supported)
4. Continue until final assistant output.

Fallback semantics:

- If provider does not support structured tool calls, retain current one-shot `Prompt`/`PromptStream` behavior.
- Memory recall policy injection remains active in fallback mode.

### 4) Policy Model

Add tool policy config with global defaults and optional per-agent overrides.

Initial schema shape (v1):

- `tools.allow` / `tools.deny`
- `tools.delegated.enabled` (default `false`)
- `tools.delegated.allow` / `tools.delegated.deny`
- `agents.defaults.tools.*` and `agents.list[].tools.*` override support

Policy precedence:

1. global
2. agent default
3. specific agent

Evaluation order:

- normalize tool name
- deny match blocks
- allowlist applies when non-empty
- delegated tools require both delegated-enabled and allowlist pass

### 5) Skills Model (Localclaw-Governed)

Add actual skill loading and prompt assembly in `internal/skills`:

- load from workspace `skills/<name>/SKILL.md` (v1 scope)
- parse minimal frontmatter:
  - `name`, `description`
  - `user-invocable` (default true)
  - `disable-model-invocation` (default false)
- eligibility checks:
  - skill enabled/disabled in config
  - required bins/env/config flags (optional extensions)

Session handling:

- build a per-session skills snapshot on first message (or on explicit refresh triggers)
- inject concise “available skills” prompt block into runtime prompt assembly

Invocation guidance:

- system prompt guidance should remain localclaw-authored (not provider-authored)
- skills are selected and read by the model under localclaw prompt governance

### 6) Transcript + Observability

Add structured logging/transcript events for:

- tool call started
- tool call blocked (policy reason)
- tool call completed (ok/error)
- delegated tool call emitted (pending)

TUI should surface tool activity consistently (existing tool-card scaffolding can be reused).

### 7) Error Contract

Tool errors must be non-fatal by default:

- local executor errors return structured tool result `{ok:false,error:"..."}`
- unknown/blocked tools return policy-denied error result
- runtime continues unless provider/session abort conditions occur

Delegated tool failures:

- represented explicitly as delegated failure results
- no silent drops

### 8) Security / Boundary Contract

Unchanged:

- no HTTP/gateway/listener runtime in localclaw
- subprocess-only model adapters
- local-only policy validation remains enforced

New constraints:

- delegated tools must never bypass localclaw policy checks
- local tool execution remains the only path for filesystem/process access in localclaw

## Implementation Notes

Expected package touch points:

- `internal/config/config.go`
  - tool policy/delegation schema + defaults + validation
- `internal/llm/*`
  - shared interface package and provider capability plumbing
- `internal/runtime/*`
  - orchestration loop, tool routing, policy checks, prompt assembly updates
- `internal/skills/*`
  - loader, frontmatter parsing, snapshot generation, prompt rendering
- `internal/tui/app.go`
  - render structured tool events and status updates
- `docs/*`
  - configuration/runtime/testing/security updates

## Phased Plan (TDD Default)

### Phase 1: Foundation Contracts

- Add provider-agnostic LLM runtime interfaces with capability flags.
- Keep current Claude adapter behavior working unchanged via compatibility mode.

Red tests:

- runtime compiles and runs with provider-agnostic interfaces
- fallback mode still injects memory policy and tool schema text

### Phase 2: Policy + Tool Router

- Implement normalized allow/deny and delegated gating logic.
- Route tool calls through a single runtime executor.

Red tests:

- deny overrides allow
- unknown tool rejected
- delegated tool blocked by default
- per-agent override precedence works

### Phase 3: Structured Tool Loop

- Add event-driven run loop for providers that emit tool calls.
- Feed tool results back to model run when supported.

Red tests:

- simulated provider tool-call sequence yields final output
- tool error degrades gracefully and run continues
- abort/cancellation interrupts running tool and model stream safely

### Phase 4: Skills Loader + Snapshot

- Implement workspace skill discovery and snapshot prompt injection.
- Add basic invocation policy fields.

Red tests:

- snapshot contains eligible skills only
- `disable-model-invocation` excludes skill from prompt block
- session initialization stores/reuses snapshot as expected

### Phase 5: TUI + Docs

- Surface tool lifecycle events in UI status/log.
- Update docs/config contracts and testing guide.

Red tests:

- tool events render in TUI message stream
- status text reflects tool activity transitions

## Test Plan

Unit/integration test targets to add/update:

- `internal/config/config_test.go`
  - tool policy/delegated config validation
- `internal/runtime/tools_test.go`
  - router behavior, policy gating, structured loop fallbacks
- `internal/runtime/*_test.go` (new)
  - provider capability branching + tool loop lifecycle
- `internal/skills/*_test.go` (new)
  - skill parsing/loading/snapshot prompt behavior
- `internal/tui/app_test.go`
  - tool-event rendering/state transitions

Focused Red/Green commands:

- `go test ./internal/config -run TestValidate`
- `go test ./internal/runtime -run TestTool`
- `go test ./internal/skills -run Test`
- `go test ./internal/tui -run Test`

Full validation:

- `go test ./...`
- `go fmt ./...` (when Go files change)

## Acceptance Criteria

- [ ] Local tools remain authoritative and execute through one runtime router.
- [ ] Delegated tools are disabled by default and require explicit policy enablement.
- [ ] Provider adapters can advertise structured tool-call capability; fallback mode remains supported.
- [ ] Memory tool behavior remains backward compatible in fallback mode.
- [ ] Skills loader is functional (workspace scope), with snapshot prompt injection and invocation flags.
- [ ] Tool execution/policy outcomes are visible in transcript/TUI status surfaces.
- [ ] No local-only boundary regressions (no listeners/gateway/server paths added).

## Rollback / Risk Notes

Rollback approach:

- keep compatibility mode behind capability checks and default to fallback path
- feature-flag delegated tool handling in config (`tools.delegated.enabled`)
- if regressions occur, disable structured tool loop and keep current prompt-injection behavior

Primary risks:

- provider event format drift for structured tool calls
- policy misconfiguration causing accidental tool denial/allow
- context inflation from skill prompt blocks

Mitigations:

- strict adapter tests with fixture streams
- deterministic policy unit tests
- bounded skill prompt rendering (size limits + truncation rules in implementation)

