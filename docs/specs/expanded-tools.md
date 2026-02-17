# Expanded Tool Cards: Normalized Fields and Readable Results

## Status

Draft

## V1 Build Policy

`localclaw` is still pre-v1. Favor clean implementations only:
- no rollback plans
- no fallback execution paths unless explicitly requested
- no legacy compatibility shims

## Problem

Expanded tool cards currently combine provider payloads in a way that is hard to read and inconsistent across providers.

Current pain points:
- redundant fields appear in both call args and result data (for example `arguments`, `server`, and `tool`).
- complex Go values are rendered with `fmt.Sprint`, producing single-line `map[...]` output.
- long single-line values are truncated, hiding useful nested result content.
- Codex and Claude Code adapters shape delegated tool results differently, so expanded cards are not consistently structured.

## Scope

- Define a canonical shape for expanded tool-card metadata (call args + result data).
- Normalize provider delegated tool payloads before TUI rendering so Codex and Claude Code produce consistent structures.
- Remove redundant call metadata from result data where equivalent context is already shown.
- Render structured values (maps/slices/JSON) in multiline blocks for readability.
- Preserve current collapsed tool-card summaries and status lifecycle semantics.
- Update docs and tests that describe tool card behavior.

## Out of Scope

- Changing MCP server protocol contracts or tool schemas.
- Changing provider CLI stream formats.
- Adding new command modes, HTTP listeners, or non-TUI display surfaces.
- Retrofitting/migrating historical transcript files.
- Adding compatibility shims for legacy expanded-card output formats.

## Behavior Contract

Define expected behavior in concrete terms:

- inputs
  - `StreamEventToolCall` and `StreamEventToolResult` events from provider adapters.
  - Expanded tool-card rendering mode (`/tools` expansion toggle).
  - Tool args/result values that may include nested maps/slices/JSON strings.

- outputs
  - Expanded tool cards remain two-phase (`arg.*` and `data.*`) but use canonical payload shaping:
    - `arg.*` only contains call input context.
    - `data.*` only contains execution output context.
  - Result data omits redundant fields when already represented by header/call context (for example duplicate `tool`, `server`, `arguments` wrappers).
  - For delegated MCP-style result payloads, render structured tool output directly (prefer `structured_content` when present) instead of raw one-line wrapper maps.
  - Structured values in expanded cards render as multiline fenced blocks:
    - maps/slices -> pretty JSON block when serializable.
    - JSON strings -> pretty JSON block.
    - plain strings -> text block when long/multiline, inline when short.
  - `data.provider_result` remains hidden in expanded cards.
  - Collapsed mode remains unchanged: header-only summary (`tool [ownership] <name> • <status>`).

- error paths
  - If structured pretty-printing fails, renderer falls back to safe plain-text rendering.
  - Nil/empty args and nil/empty result data continue to render as `arg: none` / `data: none`.
  - Unknown tool classes continue to render with `unspecified` ownership.

- unchanged behavior
  - Status transitions (`running`, `completed`, `failed`) are unchanged.
  - Call/result card pairing by call ID remains unchanged.
  - Provider-native ownership label mapping remains unchanged.

## Implementation Notes

Call out touched packages/files and key design decisions.

- `internal/llm/codex/client.go`
  - tighten delegated tool argument extraction to preserve call inputs but avoid unnecessary wrapper duplication.
  - normalize delegated tool result data so completed events expose execution output fields, not full raw item mirrors.
  - for MCP tool completion payloads, prefer normalized structured result content over raw `item` map passthrough.

- `internal/llm/claudecode/client.go`
  - align delegated tool result shape with the same canonical output contract used by Codex where practical.
  - keep provider raw payload available only for hidden/internal diagnostic fields as needed.

- `internal/tui/transcript.go`
  - replace one-line `fmt.Sprint` rendering for complex values with structured block rendering helpers.
  - add dedupe filtering so result lines do not repeat call metadata already shown in header/args.
  - preserve existing hidden-key behavior (`provider_result`) and collapsed rendering behavior.

- `internal/tui/app_stream_test.go`
  - extend coverage for expanded-card readability and dedupe behavior.
  - assert structured map/slice output is multiline and not `map[...]`-style inline truncation.

- docs updates expected
  - `docs/TUI.md`
  - provider docs describing stream/tool metadata behavior (`docs/CODEX_CLI.md`, `docs/CLAUDE_CODE.md`) if output contracts are clarified.

Key design decisions:
- normalize as close to adapter boundaries as possible so downstream UI behavior is provider-agnostic.
- keep expanded-card structure stable (`arg.*` and `data.*`) while improving content quality/readability.
- prioritize deterministic output over preserving raw provider event shape in the user-facing TUI.

## Test Plan

- unit tests to add/update
  - `internal/llm/codex/client_test.go`
    - delegated MCP tool call/result normalization avoids duplicated wrapper fields in `ToolResult.Data`.
    - MCP result `structured_content` maps to canonical result data shape.
  - `internal/llm/claudecode/client_test.go`
    - delegated tool result normalization aligns with canonical result data keys.
  - `internal/tui/app_stream_test.go`
    - expanded cards render structured result maps as multiline blocks (not `map[...]` inline strings).
    - expanded cards avoid duplicated arg/data entries for equivalent metadata.
    - existing hidden `provider_result` behavior remains intact.

- package-level focused commands for Red/Green loops
  - `go test ./internal/llm/codex`
  - `go test ./internal/llm/claudecode`
  - `go test ./internal/tui -run TestExpandedToolCard`
  - `go test ./internal/tui`

- full validation command(s)
  - `go test ./...`
  - `go fmt ./...` (if Go files changed during implementation)

## Acceptance Criteria

- [ ] Expanded tool cards no longer show redundant duplicated metadata across `arg.*` and `data.*` for delegated tool calls.
- [ ] Structured tool results (maps/slices/JSON) render as readable multiline blocks in expanded mode.
- [ ] Expanded cards avoid truncating structured result payloads into unreadable single-line `map[...]` fragments.
- [ ] Codex and Claude Code delegated tool results follow the same canonical display contract in expanded cards.
- [ ] Collapsed tool-card summaries and status transitions remain unchanged.
- [ ] Existing hidden-field behavior for `data.provider_result` remains unchanged.
- [ ] Targeted adapter/TUI tests and full suite validation pass during implementation.
