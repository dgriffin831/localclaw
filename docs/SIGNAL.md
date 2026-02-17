# Signal Channel Guide

`localclaw` supports outbound Signal delivery through local `signal-cli` subprocess execution.

## Scope

- Outbound only in this phase.
- No inbound polling/listener or webhook runtime.

## Prerequisites

1. Install [`signal-cli`](https://github.com/AsamK/signal-cli).
2. Link/register your Signal account with `signal-cli`.
3. Confirm the account can send messages from the local machine.

Example (account/device setup depends on your Signal flow):

```bash
signal-cli -a +15551234567 listGroups
```

## Setup

Configure Signal channel settings.

```json
{
  "channels": {
    "enabled": ["slack", "signal"],
    "signal": {
      "cli_path": "signal-cli",
      "account": "+15551234567",
      "default_recipient": "+15557654321",
      "timeout_seconds": 10
    }
  }
}
```

Recipient formats:
- Direct recipient (E.164): `+15557654321`
- Group recipient: `group:<group-id>` or `signal:group:<group-id>`

## MCP Tool

Tool name: `localclaw_signal_send`

Arguments:
- `text` (required)
- `recipient` (optional; falls back to `channels.signal.default_recipient`)
- `agent_id` (optional)
- `session_id` (optional)

Structured result fields:
- `ok`
- `recipient`
- `sent_at` (adapter timestamp)

## Implementation Notes

- Adapter file: `internal/channels/signal/adapter.go`
- Runtime MCP dispatch: `internal/runtime/mcp_support.go`
- MCP tool handler: `internal/mcp/tools/channels.go`

Delivery path:
1. Runtime validates `channels.enabled` gate.
2. Signal adapter builds `signal-cli` command with configured account and target.
3. Adapter executes subprocess with context timeout.
4. Successful sends return `recipient` and `sent_at`.

Command shape:
- Direct recipient: `signal-cli -a <account> send -m <text> <recipient>`
- Group recipient: `signal-cli -a <account> send -m <text> -g <group-id>`

## Failure Modes

- Disabled channel: `channel "signal" is disabled`
- Missing recipient after fallback: send rejected before subprocess execution
- Subprocess failure: stderr is surfaced in wrapped error text
- Timeout/cancellation: subprocess is canceled through `exec.CommandContext`

## Session Metadata Persistence

When `agent_id` and/or `session_id` are provided on tool calls, localclaw updates session metadata with:
- `origin` (set to `signal` when unset/unknown)
- `delivery.channel` (`signal`)
