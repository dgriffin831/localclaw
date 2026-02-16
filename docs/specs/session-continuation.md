# Session Continuation for CLI Providers

## Status

Draft v1.0

## Problem / Motivation

`localclaw` currently invokes both CLI providers (`claudecode`, `codex`) as one-shot prompt calls per turn.

That means:

- provider-native thread/session continuation is not reused across turns
- no provider session/thread ID is persisted in `session.SessionEntry`
- adapters cannot use provider-specific resume subcommands/flags

Result: each message runs without provider-native conversational continuity unless context is manually reintroduced through local bootstrap/system/tool prompts.

## Scope

- Add per-provider persisted CLI session IDs to local session metadata.
- Reuse persisted provider session IDs on subsequent prompts for the same `agent/session`.
- Add provider-specific resume argument handling in both adapters:
  - Claude Code (`claude`)
  - Codex (`codex`)
- Extend stream metadata so adapters can surface discovered provider session IDs back to runtime.
- Persist discovered provider session IDs during prompt streaming.
- Add focused tests and docs updates for the new behavior contract.

## Out of Scope

- Full transcript replay into each prompt turn.
- Multi-provider fallback with shared continuation across providers.
- Remote/network model clients.
- Reworking session-store layout beyond adding provider-session metadata fields.

## Behavior Contract

### Inputs

- Resolved runtime session identity (`agentID`, `sessionID`, `sessionKey`).
- Current LLM provider (`claudecode` or `codex`).
- Existing persisted provider session ID for `(sessionKey, provider)`, if any.
- Provider adapter start/resume arg configuration.

### Outputs

- First prompt in a provider session starts a new provider thread/session.
- Subsequent prompts for same `(sessionKey, provider)` use provider resume mode/args when possible.
- Runtime persists provider session ID in session metadata when adapter emits it.
- Session IDs are persisted per provider (not a single shared field).

### Error Paths

- If resume invocation fails with a recognized "invalid/expired/missing session" error:
  - clear persisted provider session ID for that provider
  - retry once as a fresh provider session
- If retry fails, return error as today with stderr context.
- If provider emits no session ID, run still succeeds but continuation is unavailable for next turn.

### Unchanged Behavior

- Local transcript JSONL persistence behavior is unchanged.
- Bootstrap/skills/tool policy prompt assembly remains unchanged.
- Local-only policy and single-process runtime constraints remain unchanged.

## Proposed Design

## 1) Session Metadata Schema

Add provider-scoped session ID map to `internal/session/types.go`:

- `ProviderSessionIDs map[string]string \`json:"providerSessionIds,omitempty"\``

Provider key normalization rules:

- lowercase
- trim surrounding whitespace
- canonical values for built-ins: `claudecode`, `codex`

Helper behavior (in `internal/session`):

- `GetProviderSessionID(entry, provider) string`
- `SetProviderSessionID(entry, provider, id)`
- `ClearProviderSessionID(entry, provider)`

## 2) LLM Contract Extensions

Extend shared contracts in `internal/llm/contracts.go`:

- `SessionMetadata.Provider string`
- `SessionMetadata.ProviderSessionID string`
- `ProviderMetadata.SessionID string`

This keeps provider-session continuation transport provider-agnostic while still preserving provider ownership.

## 3) Runtime Request + Persistence Flow

In `internal/runtime/tools.go` and `internal/runtime/app.go`:

1. During `buildPromptRequest`, read current `SessionEntry` and include:
   - `req.Session.Provider`
   - `req.Session.ProviderSessionID` (if persisted for active provider)
2. Wrap provider event stream in runtime (instead of pure pass-through) to:
   - forward all events unchanged
   - intercept `StreamEventProviderMetadata` with non-empty `SessionID`
   - persist `SessionEntry.ProviderSessionIDs[provider] = SessionID`
3. Keep persistence best-effort; never drop provider output due to metadata write failure.

## 4) Adapter Resume Semantics

### Claude Code adapter (`internal/llm/claudecode/client.go`)

Add adapter-level start/resume behavior:

- Start path (no persisted ID):
  - include provider new-session args (default: `--session-id <uuid>`)
- Resume path (persisted ID exists):
  - include provider resume args (default: `--resume <sessionId>`)

Session ID discovery:

- parse provider JSON stream lines for known fields:
  - `session_id`, `sessionId`, `conversation_id`, `conversationId`
- emit provider metadata event with discovered `SessionID`.

### Codex adapter (`internal/llm/codex/client.go`)

Add adapter-level start/resume behavior:

- Start path (no persisted ID):
  - existing `exec --json ...`
- Resume path (persisted ID exists):
  - use resume args template (default: `exec resume <sessionId> ...`)

Session ID discovery:

- parse Codex JSON events for known fields:
  - `thread_id`, `threadId`, `session_id`, `sessionId`
- emit provider metadata with `SessionID`.

Resume output handling:

- support resume output mode differences (`json`/`jsonl`/`text`) without breaking stream contract.
- default Codex resume output mode should tolerate plain text fallback.

## 5) Config Additions (Provider-Specific Resume Args)

Add minimal config fields for both providers under `llm` config structs:

- `session_mode`: `always | existing | none`
- `session_arg` (optional)
- `resume_args` (optional, supports `{sessionId}` placeholder)
- `session_id_fields` (optional list for parsing provider output)
- optional resume output mode for codex (`resume_output`)

Default behaviors:

- `claudecode`: `session_mode=always`
- `codex`: `session_mode=existing`

Validation:

- `session_mode` must be one of allowed values when set.
- `resume_args` placeholder validation for modes that require existing sessions.

## 6) Reset / Rotation Semantics

- `ResetSession(StartNew=true)` implicitly clears continuation by moving to a new local session ID.
- For same-session reset (`StartNew=false`), add explicit provider-session clear behavior in reset path.

## Implementation Notes

Expected touched files:

- `internal/session/types.go`
- `internal/session/store.go` (if helper wiring needed)
- `internal/llm/contracts.go`
- `internal/runtime/tools.go`
- `internal/runtime/app.go`
- `internal/llm/claudecode/client.go`
- `internal/llm/codex/client.go`
- `internal/config/config.go`
- `docs/CLAUDE_CODE.md`
- `docs/CONFIGURATION.md`
- `docs/RUNTIME.md`

## Test Plan

Unit tests to add/update:

- `internal/session`
  - provider session ID helper normalization/set/clear behavior
- `internal/runtime`
  - request includes persisted provider session ID
  - provider metadata session ID is persisted during stream
  - resume-failure clear-and-retry-once behavior
- `internal/llm/claudecode`
  - new session args vs resume args selection
  - session ID extraction from provider output fields
- `internal/llm/codex`
  - new session args vs resume args selection
  - `thread_id` extraction and metadata emission
  - resume text-output fallback handling
- `internal/config`
  - validation for new resume/session config fields

Focused commands for Red/Green:

- `go test ./internal/session`
- `go test ./internal/runtime`
- `go test ./internal/llm/claudecode`
- `go test ./internal/llm/codex`
- `go test ./internal/config`

Full validation:

- `go test ./...`

## Acceptance Criteria

- [ ] Provider session IDs are persisted per provider in session metadata.
- [ ] Runtime passes persisted provider session IDs back to adapters on subsequent turns.
- [ ] Claude adapter supports start + resume argument flows with session ID extraction.
- [ ] Codex adapter supports start + resume argument flows with session ID extraction.
- [ ] Resume failure on stale/invalid provider session clears persisted ID and retries once.
- [ ] Existing transcript persistence and prompt assembly behavior remain unchanged.
- [ ] Docs (`RUNTIME`, `CONFIGURATION`, `CLAUDE_CODE`) reflect the new continuation contract.

## Rollback / Risk Notes

Primary risk:

- stale provider session IDs causing repeated resume failures.

Mitigation:

- clear-and-retry-once fallback to fresh provider session.
- configurable `session_mode=none` as immediate kill-switch per provider.

Rollback approach:

- disable continuation via provider `session_mode=none` (or revert to pre-spec adapter paths).
