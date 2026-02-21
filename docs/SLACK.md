# Slack Channel Guide

`localclaw` supports outbound Slack delivery through the local runtime and MCP tool surface.

## Scope

- Outbound only in this phase.
- No inbound Slack event/webhook handling.
- `channels serve` is Signal inbound only; it does not run a Slack worker.

## Prerequisites

1. A Slack app with a bot token (`xoxb-...`).
2. Bot scope `chat:write` for message delivery.
3. The bot invited to target channels.

## Setup

1. Export your bot token in the env var named by `channels.slack.bot_token_env`.

```bash
export SLACK_BOT_TOKEN="xoxb-..."
```

2. Configure Slack channel settings.

```json
{
  "channels": {
    "enabled": ["slack", "signal"],
    "slack": {
      "bot_token_env": "SLACK_BOT_TOKEN",
      "default_channel": "C0123456789",
      "api_base_url": "https://slack.com/api",
      "timeout_seconds": 10
    }
  }
}
```

Notes:
- `default_channel` is optional, but Slack sends fail with `channel is required` if both the tool argument `channel` and `channels.slack.default_channel` are empty.
- `bot_token_env`, `api_base_url`, and `timeout_seconds` must be set to valid values when `slack` is included in `channels.enabled`.

## MCP Tool

Tool name: `localclaw_slack_send`

Arguments:
- `text` (required)
- `channel` (optional; falls back to `channels.slack.default_channel`)
- `thread_id` (optional)
- `agent_id` (optional)
- `session_id` (optional)

Structured result fields:
- `ok`
- `channel`
- `message_id` (Slack `ts`)
- `thread_id` (may be empty)

## Implementation Notes

- Adapter file: `internal/channels/slack/adapter.go`
- Runtime MCP dispatch: `internal/runtime/mcp_support.go`
- MCP tool handler: `internal/mcp/tools/channels.go`

Delivery path:
1. Runtime validates `channels.enabled` gate.
2. Slack adapter reads token from configured env key.
3. Adapter calls `POST /chat.postMessage` against `channels.slack.api_base_url`.
4. Response metadata maps to `channel`, `message_id`, and `thread_id`.

Caveat:
- MCP tool registration includes `localclaw_slack_send` even when Slack is disabled; calls then fail at runtime with `channel "slack" is disabled`.

## Failure Modes

- Disabled channel: `channel "slack" is disabled`
- Missing channel after fallback: `channel is required`
- Missing token env value: Slack send fails before request
- Missing or blank text: Slack send fails validation before request
- Slack API non-2xx or `ok=false`: mapped to deterministic send errors
- Timeout/cancellation: request aborts via context

## Session Metadata Persistence

When `agent_id` and/or `session_id` are provided on tool calls, localclaw updates session metadata with:
- `origin` (set to `slack` when unset/unknown)
- `delivery.channel`
- `delivery.threadId`
- `delivery.messageId`
