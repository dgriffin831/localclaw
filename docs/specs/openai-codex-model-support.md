# OpenAI Codex Model Support Spec

## Status

Draft v1.0

## Problem / Motivation

`localclaw` currently hard-codes `llm.provider=claudecode` and wires runtime/TUI types directly to the Claude adapter package.

This blocks operators who want to run `localclaw` with OpenAI Codex while preserving the existing architecture constraints:

- single-process Go CLI
- local subprocess-based model invocation
- no HTTP/gRPC server or gateway runtime in `localclaw`

The existing `/model` slash command is also a placeholder and does not apply any model override.

## Goals

1. Add OpenAI Codex as a first-class LLM provider alongside Claude Code.
2. Keep execution local through subprocess invocation (`exec.CommandContext`) only.
3. Preserve current runtime prompt assembly behavior (bootstrap context + memory tool policy + session key).
4. Introduce provider-agnostic LLM stream/event interfaces so runtime/TUI are not tied to one provider package.
5. Implement practical model override behavior in TUI (`/model <name>`) for providers that support model flags.
6. Maintain backward compatibility for existing Claude Code configs and workflows.

## Scope

- Config schema and validation updates for provider allowlist expansion.
- Runtime/provider abstraction refactor needed to support multiple subprocess-backed LLM adapters.
- New Codex adapter package with prompt/stream/error handling.
- TUI `/model` behavior from placeholder to effective runtime override flow.
- Required test and documentation updates for changed behavior contracts.

## Out of Scope

- Direct OpenAI HTTP client integration inside `localclaw`.
- Any network listener/server/gateway mode.
- Multi-provider fallback chains in a single prompt attempt.
- Persistent per-session model overrides in session storage (v1 keeps override in-memory in TUI process only).

## Constraints

- Go `1.17`.
- Local-only runtime posture remains enforced.
- Startup order and command modes remain unchanged.
- Claude Code behavior must not regress.
- Codex integration must be non-interactive and automation-safe.

## Current State Summary

- `internal/config/config.go` validates `llm.provider` as only `claudecode`.
- `internal/runtime/app.go` stores `llm claudecode.Client`.
- `internal/tui/app.go` stream types are `claudecode.StreamEvent`.
- `/model <name>` prints “not implemented”.
- `docs/CLAUDE_CODE.md` describes Claude-specific adapter behavior.

## Terminology

- **Provider**: local CLI backend (`claudecode`, `codex`).
- **Model**: model identifier passed to provider CLI when supported (for example via `--model`).

## Behavior Contract

### Inputs

- Config:
  - `llm.provider`: `claudecode` or `codex`.
  - Provider-specific sections: `llm.claude_code` and `llm.codex`.
- Prompt text from runtime/TUI.
- Optional runtime model override (from `/model`) for active TUI session.

### Outputs

- `Prompt(...)` returns final assistant text.
- `PromptStream(...)` yields stream events:
  - `delta` chunks
  - `final` completion text
- TUI header/status show selected provider and effective model info.

### Error Paths

- Unsupported provider: startup validation failure.
- Missing provider binary path for active provider: startup validation failure.
- Empty prompt input: immediate provider error (`input is required`).
- Subprocess start/stream/wait failures: surfaced with context and stderr text when present.
- Invalid `/model` usage: user-facing system message (usage/error).
- Provider without model-override support: explicit user-facing notice; run proceeds with configured default.

### Unchanged Behavior

- Local-only policy enforcement (`security.*`) remains unchanged.
- Command modes remain `check`, `tui`, `memory`.
- Prompt assembly semantics in runtime (`buildPromptInput`) remain unchanged.
- Claude Code remains default provider in `Default()`.

## Proposed Design

## 1) Provider-Agnostic LLM Interface

Add an `internal/llm` core package for shared contracts:

- `type Client interface`
  - `Prompt(ctx, input string) (string, error)`
  - `PromptStream(ctx, input string, opts PromptOptions) (<-chan StreamEvent, <-chan error)` (or equivalent options-aware API)
- `type StreamEvent { Type, Text }`
- `type PromptOptions { ModelOverride string }`

Rationale:
- removes `runtime`/`tui` dependency on `internal/llm/claudecode` event types.
- allows adding providers without repeating runtime/TUI rewrites.

Compatibility requirement:
- keep a no-options convenience path for existing call sites (wrapper overload/helper).

## 2) Codex Provider Adapter

Add `internal/llm/codex/client.go` as a subprocess adapter.

Execution contract (v1):
- invoke non-interactive mode using `codex exec`.
- request structured output with `--json`.
- pass prompt via stdin (`-` argument) to avoid shell escaping/length issues.
- optional flags:
  - `--model <name>` when configured or overridden
  - `--profile <name>` when configured
  - provider-specific pass-through args (optional list in config)

JSON stream parsing contract:
- parse JSONL events from stdout.
- treat Codex `item.completed` with `item.type=="agent_message"` as assistant content.
- emit as `delta` and aggregate for `final` event.
- ignore non-message events except for diagnostics hooks.

Failure contract:
- capture stderr; include it in wait/start/read errors.
- preserve context cancellation semantics via `exec.CommandContext`.

## 3) Claude Provider Adapter Alignment

Keep `internal/llm/claudecode/client.go` behavior unchanged functionally, but align signatures/types with shared `internal/llm` contracts.

No behavior regressions allowed for:
- streaming delta/final events
- env handling (`AWS_PROFILE`, region, govcloud marker)
- error wrapping with stderr

## 4) Runtime Provider Selection

In `runtime.New(cfg)`:
- select provider by `cfg.LLM.Provider`.
- instantiate either Claude or Codex adapter.
- store as provider-agnostic `llm.Client`.

Add a small factory function (runtime-local or `internal/llm/factory`) to keep branching isolated and testable.

## 5) Config Schema Changes

Extend `LLMConfig`:

- keep:
  - `provider`
  - `claude_code`
- add:
  - `codex`

Proposed `CodexConfig` fields:

- `binary_path` (string, required when provider=`codex`, default `codex`)
- `profile` (string, optional)
- `model` (string, optional provider default model)
- `extra_args` ([]string, optional)

Validation updates:
- `llm.provider` allowlist: `claudecode`, `codex`.
- provider-specific required field checks:
  - Claude: `llm.claude_code.binary_path` required when provider is `claudecode`.
  - Codex: `llm.codex.binary_path` required when provider is `codex`.
- keep existing Claude auth mode validation semantics.

Defaults:
- `provider` stays `claudecode` to avoid behavior surprise.
- `llm.codex.binary_path` default `codex`.
- `llm.codex.profile/model/extra_args` default empty.

## 6) TUI Model Selection UX

Implement `/model <name>` with session-local override:

- stores override in TUI model state (not persisted).
- applies to subsequent prompts only.
- `/model` with empty arg returns usage.
- `/model default` (or `/model off`) clears override.

Display rules:
- header `/status` include:
  - provider
  - configured model
  - active override if set

Runtime/TUI call path:
- TUI passes `PromptOptions{ModelOverride: ...}` in stream calls.
- providers that support override (Codex) apply it.
- providers that do not (Claude, unless future support exists) ignore with explicit `/status` note or one-time system message.

## 7) Documentation Changes

Update:

- `README.md` (supported providers + config snippets)
- `docs/CONFIGURATION.md` (new `llm.codex` schema + validation)
- `docs/RUNTIME.md` (provider-agnostic runtime wiring)
- `docs/ARCHITECTURE.md` and `ARCHITECTURE.md` (LLM adapters list)
- `docs/SECURITY.md` (provider constraint statement from “claudecode only” to allowlist)

Add:

- `docs/CODEX.md` parallel to `docs/CLAUDE_CODE.md`, documenting Codex adapter command/stream/error behavior.

## 8) Security and Policy Notes

- `localclaw` still does not expose any network listener or model HTTP client.
- Codex and Claude remain subprocess integrations only.
- Keep policy guardrails unchanged (`security.enforce_local_only`, no gateway/listener flags).
- Ensure adapter construction never shells out via `sh -c`; pass args directly to `exec.CommandContext`.

## Implementation Plan (TDD-First)

## Phase 1: Shared LLM Contracts + Provider Wiring

Changes:
- add provider-agnostic LLM interface/types package.
- refactor runtime and TUI to use shared stream types.
- add provider factory and `cfg.LLM.Provider` switch.

Red tests:
- runtime creation path selects codex provider when configured.
- compile-time coverage for stream types no longer tied to claudecode package.

Green:
- minimal code changes to pass tests without behavior drift.

## Phase 2: Codex Adapter

Changes:
- add `internal/llm/codex/client.go`.
- implement `Prompt`/`PromptStream`.
- parse `codex exec --json` JSONL output; emit message events.

Red tests:
- prompt requires input.
- stdout JSON message parsing produces expected final text.
- stderr surfaced on non-zero exit.
- cancellation terminates subprocess.

Green:
- implement adapter + helpers; keep subprocess + stream cleanup robust.

## Phase 3: Config + Validation + Defaults

Changes:
- extend `LLMConfig` and defaults.
- validation allowlist/provider-specific required checks.

Red tests:
- provider `codex` validates with codex binary path set/defaulted.
- invalid provider rejected.
- codex provider fails when binary path blank.
- claudecode path validation still enforced when provider=claudecode.

Green:
- implement schema/validation changes with backward compatibility.

## Phase 4: TUI `/model` Override

Changes:
- add model override state to TUI model.
- route prompt options to runtime/provider.
- update header/status formatting.

Red tests:
- `/model` sets override.
- `/model default` clears override.
- `/status` includes provider + effective model.

Green:
- implement state handling and display updates.

## Phase 5: Docs + Final Validation

Run:
- `go test ./internal/config`
- `go test ./internal/runtime`
- `go test ./internal/tui`
- `go test ./internal/llm/...` (new package tests)
- `go test ./...`
- `go fmt ./...`

Update docs listed above.

## Test Plan

Unit tests to add/update:

- `internal/config/config_test.go`
  - provider allowlist and codex config validation.
- `internal/runtime/local_only_test.go`
  - runtime new/select provider behavior.
- `internal/runtime/tools_test.go`
  - prompt assembly unchanged across providers.
- `internal/tui/app_test.go`
  - `/model` functional behavior and status/header output.
- `internal/llm/codex/client_test.go` (new)
  - JSON event parse, stderr error surfacing, cancellation, empty input.
- optional: `internal/llm/claudecode/client_test.go` (new baseline parity tests).

Focused Red/Green commands:

- `go test ./internal/config -run TestValidate`
- `go test ./internal/runtime -run TestNew`
- `go test ./internal/tui -run TestHandleSlash`
- `go test ./internal/llm/codex -run Test`

Full validation:

- `go test ./...`
- `go fmt ./...`
- `go run ./cmd/localclaw check`
- `go run ./cmd/localclaw tui` (manual smoke)

## Acceptance Criteria

- [ ] `llm.provider` supports both `claudecode` and `codex`.
- [ ] `runtime.New` successfully wires the selected provider via local subprocess adapter.
- [ ] Codex adapter supports prompt + stream behavior and context cancellation.
- [ ] Existing Claude adapter behavior remains backward compatible.
- [ ] `/model <name>` applies model override in TUI for subsequent prompts.
- [ ] Config validation/docs/security docs reflect new provider allowlist.
- [ ] Full test suite passes (`go test ./...`) and formatting is clean.

## Risks / Rollback Notes

Risks:
- Codex JSON event schema changes in future CLI versions.
- Provider abstraction refactor could accidentally regress Claude stream behavior.

Mitigations:
- keep parser tolerant to unknown event types.
- add adapter-focused regression tests for both providers.
- isolate provider selection logic to a small factory.

Rollback:
- restore `llm.provider=claudecode` allowlist only.
- keep shared interfaces if already merged (safe internal abstraction), or revert factory and codex package in one commit.

## Open Questions

1. Should `/model` override persist across `/new` session creation, or reset each new session? (spec assumes reset on `/new` and `/reset`).
2. Should Codex-specific advanced flags (sandbox/approval/config overrides) be exposed in first release or deferred until a follow-up spec? (this spec keeps only `binary_path`, `profile`, `model`, `extra_args`).
