# Signal Channel Guide

`localclaw` supports Signal outbound delivery and inbound direct-message routing through local `signal-cli` subprocess execution.

## Scope

- Outbound Signal send support.
- Inbound direct-message polling support (`channels serve`).
- No webhook/listener server runtime.
- Group messages are never executed in inbound mode.

## Prerequisites

1. Install [`signal-cli`](https://github.com/AsamK/signal-cli).
2. Link/register your Signal account with `signal-cli`.
3. Confirm the account can send messages from the local machine.

Example (account/device setup depends on your Signal flow):

```bash
signal-cli -a +15551234567 listGroups
```

## Onboarding (Link Device)

If your account is not linked yet, this is the expected flow.

1. Start linking in terminal one and keep it running.

```bash
signal-cli link -n "localclaw"
```

This prints a `sgnl://linkdevice?...` URL.

2. In terminal two, render that URL to an image (replace `<link-url>` with the full `sgnl://...` output).

```bash
qrencode "<link-url>" -o ~/Downloads/signal-link.png
```

3. On your phone, open Signal and scan the generated image:
- Settings
- Linked devices
- Link new device
- Scan `~/Downloads/signal-link.png`

4. Wait for terminal one to show association success, then verify:

```bash
signal-cli listAccounts
```

If `qrencode` is missing, install it first (for example, `brew install qrencode` on macOS).

## Setup

Configure Signal channel settings.

```json
{
  "channels": {
    "enabled": ["signal"],
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

## Inbound Setup (Allowlist + Agent Routing)

Configure inbound policy under `channels.signal.inbound`.

```json
{
  "channels": {
    "enabled": ["signal"],
    "signal": {
      "cli_path": "signal-cli",
      "account": "+15551234567",
      "timeout_seconds": 10,
      "inbound": {
        "enabled": true,
        "allow_from": ["+15557654321", "+15559876543"],
        "agent_by_sender": {
          "+15557654321": "agent-support",
          "+15559876543": "agent-ops"
        },
        "default_agent": "default",
        "send_typing": true,
        "typing_interval_seconds": 5,
        "send_read_receipts": true,
        "poll_timeout_seconds": 5,
        "max_messages_per_poll": 10
      }
    }
  }
}
```

Run inbound worker:

```bash
go run ./cmd/localclaw channels serve
```

One-shot poll (smoke test):

```bash
go run ./cmd/localclaw channels serve --once
```

Inbound policy behavior:
- sender must be in `inbound.allow_from`
- `inbound.allow_from` values must be E.164 numbers (for example `+15557654321`)
- sender routes to `inbound.agent_by_sender[sender]` when present
- otherwise sender routes to `inbound.default_agent` (or `default` when unset)
- when `inbound.send_typing=true`, typing indicators are sent while the reply is running and stopped when done
- when `inbound.send_read_receipts=true`, read receipts are sent for accepted direct messages with valid timestamps
- group messages are always dropped

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

Inbound path:
1. `channels serve` calls `signal-cli -o json -a <account> receive --timeout <seconds> --max-messages <n> --ignore-attachments --ignore-stories` in a loop.
2. Runtime parses inbound envelopes and drops sync/group payloads.
3. Runtime enforces `inbound.allow_from`.
4. Runtime optionally sends a read receipt (`sendReceipt`) for accepted direct messages.
5. Runtime optionally starts a typing loop (`sendTyping`) while generating a response.
6. Runtime resolves sender -> agent -> session and runs prompt flow.
7. Runtime sends reply through Signal adapter and sends typing stop.

Command shape:
- Direct recipient: `signal-cli -a <account> send -m <text> <recipient>`
- Group recipient: `signal-cli -a <account> send -m <text> -g <group-id>`
- Typing start/refresh: `signal-cli -a <account> sendTyping <recipient>`
- Typing stop: `signal-cli -a <account> sendTyping -s <recipient>`
- Read receipt: `signal-cli -a <account> sendReceipt -t <timestamp> --type read <recipient>`

## Failure Modes

- Disabled channel: `channel "signal" is disabled`
- Inbound worker requires `channels.signal.inbound.enabled=true` and at least one `inbound.allow_from` sender
- Missing recipient after fallback: send rejected before subprocess execution
- Subprocess failure: stderr is surfaced in wrapped error text
- Timeout/cancellation: subprocess is canceled through `exec.CommandContext`
- Inbound non-allowlisted senders are ignored.
- Inbound group messages are ignored.
- Inbound typing/receipt failures are logged and do not block normal reply delivery.

## Session Metadata Persistence

When `agent_id` and/or `session_id` are provided on tool calls, localclaw updates session metadata with:
- `origin` (set to `signal` when unset/unknown)
- `delivery.channel` (`signal`)
