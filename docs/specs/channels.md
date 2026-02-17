# Slack and Signal Channel Delivery

## Status

Draft

## V1 Build Policy

`localclaw` is still in v1 build-out. This spec intentionally avoids rollback plans, fallback execution paths, and legacy compatibility requirements. Implementation should be clean and forward-only.

## Problem

`localclaw` currently validates `channels.enabled` (`slack`, `signal`) and wires channel adapter placeholders in runtime, but both adapters are no-ops and there is no runtime dispatch path.

Current gap:
- `internal/channels/slack/adapter.go` and `internal/channels/signal/adapter.go` return `nil` without delivering anything.
- Runtime always constructs both adapters and does not honor `channels.enabled` at execution time.
- MCP has no channel-delivery tools, so providers cannot trigger Slack/Signal delivery through the local runtime boundary.

As a result, channel support exists only as config shape, not as working behavior.

## Scope

- Implement real outbound channel delivery for Slack and Signal (phase 1).
- Add runtime channel dispatch behavior that is explicitly gated by `channels.enabled`.
- Add MCP tools for channel delivery so provider sessions can request outbound notifications/messages.
- Add channel-specific config required for secure delivery and deterministic routing.
- Persist channel delivery metadata into session entries when `agent_id`/`session_id` are provided on tool calls.
- Add tests and docs updates for configuration, runtime behavior, and MCP tool usage.

## Out of Scope

- Inbound channel ingestion (webhooks, socket mode listeners, polling inboxes).
- New HTTP/gateway/listener server surfaces.
- New channel types beyond `slack` and `signal`.
- Rich message formatting/attachments (Slack blocks, files, reactions) in this phase.
- Retry queues, guaranteed delivery semantics, or background resend workers.

## Behavior Contract

Define expected behavior in concrete terms:
- inputs
  - `channels.enabled` remains the feature gate and channel allowlist.
  - New config block under `channels`:
    - `channels.slack` settings for auth + defaults (token env key, default channel, API base URL, timeout).
    - `channels.signal` settings for local CLI execution (binary path, sender account, default recipient/group, timeout).
  - MCP tools:
    - `localclaw_slack_send` with required `text`; optional `channel`, `thread_id`, `agent_id`, `session_id`.
    - `localclaw_signal_send` with required `text`; optional `recipient`, `agent_id`, `session_id`.
- outputs
  - Slack send:
    - Delivers text via Slack Web API (`chat.postMessage`) using configured bot token.
    - Returns structured result with at least `ok`, `channel`, `message_id` (`ts`), and `thread_id` when present.
  - Signal send:
    - Delivers text via configured local `signal-cli` subprocess (`exec.CommandContext`).
    - Returns structured result with at least `ok`, `recipient`, and `sent_at` (adapter timestamp).
  - Runtime only wires and exposes enabled channels; disabled channels are unavailable for dispatch and MCP tool calls.
  - When `agent_id`/`session_id` are supplied on tool calls, runtime updates session metadata:
    - `origin` set to channel origin when unset/unknown.
    - `delivery.channel`, `delivery.threadId`, `delivery.messageId` updated from adapter result when available.
- error paths
  - Calling Slack/Signal send while that channel is disabled returns a deterministic error (`channel "<name>" is disabled`).
  - Missing required channel config for an enabled channel fails runtime startup with wrapped error context.
  - Slack non-2xx/`ok=false` API responses return errors including API error code/message (without leaking token values).
  - Signal subprocess start/exit failures return wrapped stderr context.
  - Context cancellation/timeout aborts in-flight sends and returns cancellation errors.
  - Session metadata persistence failures after successful delivery return an error that includes delivery metadata for operator reconciliation.
- unchanged behavior
  - Runtime remains single-process and local-only (no listener/gateway/server mode added).
  - Channel allowlist remains strict to `slack` and `signal`.
  - Existing memory/workspace/cron/session MCP tools and command modes (`doctor`, `tui`, `memory`, `mcp`) remain supported.

## Implementation Notes

Call out touched packages/files and key design decisions.
- `internal/config/config.go`
  - Extend `ChannelsConfig` with `Slack` and `Signal` nested settings.
  - Add defaults and strict validation rules for enabled-channel requirements.
- `internal/channels/slack/adapter.go`
  - Replace no-op with HTTP client delivery implementation.
  - Use request-scoped timeout and redact token-bearing values from surfaced errors.
- `internal/channels/signal/adapter.go`
  - Replace no-op with `signal-cli` subprocess adapter (`exec.CommandContext`), including stderr capture.
- `internal/runtime/app.go`
  - Gate adapter wiring by `channels.enabled`.
  - Add dispatch helpers used by MCP backend methods.
  - Fail fast when enabled channels are misconfigured.
- `internal/runtime/mcp_support.go`
  - Add runtime channel send methods that call adapter dispatch and optionally persist session delivery metadata.
- `internal/mcp/tools/channels.go` (new)
  - Define `localclaw_slack_send` and `localclaw_signal_send` tool schemas + handlers.
- `internal/cli/mcp.go`
  - Register channel tools in MCP server setup.
- docs updates
  - `README.md`, `docs/RUNTIME.md`, `docs/CONFIGURATION.md`, `docs/SECURITY.md`, `docs/TESTING.md`.

Key design decisions:
- Phase 1 is outbound-only to avoid introducing inbound listener surfaces.
- Slack uses token-based Web API for deterministic message IDs/thread support.
- Signal uses local CLI subprocess execution to preserve local adapter boundary and straightforward cancellation semantics.

## Test Plan

- unit tests to add/update
  - `internal/config/config_test.go`
    - validate enabled-channel config requirements.
    - reject incomplete slack/signal config when enabled.
  - `internal/channels/slack/adapter_test.go`
    - success path maps API response to delivery result.
    - non-2xx and `ok=false` failure paths.
    - timeout/cancellation behavior.
  - `internal/channels/signal/adapter_test.go`
    - command construction for default and override recipient.
    - subprocess stderr/exit failure behavior.
    - timeout/cancellation behavior.
  - `internal/runtime` tests
    - runtime only wires enabled channels.
    - disabled channel dispatch returns deterministic error.
    - session delivery metadata is updated when session identifiers are provided.
  - `internal/mcp/tools/channels_test.go`
    - argument validation (`text`, optional routing fields).
    - backend call mapping and structured response shape.
  - `internal/cli/mcp_test.go`
    - MCP tool registration includes channel tools.
- package-level focused commands for Red/Green loops
  - `go test ./internal/config`
  - `go test ./internal/channels/slack`
  - `go test ./internal/channels/signal`
  - `go test ./internal/runtime`
  - `go test ./internal/mcp/tools`
  - `go test ./internal/cli`
- full validation command(s)
  - `go test ./...`
  - `go run ./cmd/localclaw mcp serve` (manual MCP smoke validation for channel tool exposure)

## Acceptance Criteria

- [ ] Enabled Slack and Signal channels deliver real outbound messages through their adapters.
- [ ] Runtime dispatch strictly honors `channels.enabled` and does not expose disabled channel behavior.
- [ ] MCP exposes `localclaw_slack_send` and `localclaw_signal_send` with validated argument contracts.
- [ ] Channel delivery results include stable structured metadata (channel/recipient + message identifiers where available).
- [ ] Session delivery metadata updates correctly when `agent_id`/`session_id` are supplied.
- [ ] Misconfigured enabled channels fail fast at startup with actionable errors.
- [ ] Docs are updated for channel config, runtime behavior, and testing commands.
- [ ] Full test suite passes (`go test ./...`).
