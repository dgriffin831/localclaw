# Signal Inbound Routing and Policy

## Status

Draft

## Problem

`localclaw` can send outbound channel messages but cannot receive inbound messages and route them to agent sessions.

Operators need:
- inbound Signal message processing
- deterministic sender -> agent assignment
- strict sender allowlist policy
- hard prohibition of group-message execution

## Scope

- Add Signal inbound receive loop using local `signal-cli` subprocess execution.
- Add sender allowlist policy for inbound Signal messages.
- Add sender-to-agent assignment config.
- Route accepted inbound messages through runtime prompt execution and send replies back through Signal adapter.
- Add `channels` CLI mode to run inbound processing.

## Out of Scope

- Slack inbound in this phase.
- Any HTTP/gateway/listener server surfaces.
- Group-message execution or group routing.

## Behavior Contract

Inputs:
- `channels.signal.inbound` config:
  - `enabled`
  - `allow_from` (required when enabled)
  - `agent_by_sender` (optional sender -> agent mapping)
  - `default_agent` (fallback agent)
  - `poll_timeout_seconds`
  - `max_messages_per_poll`
- CLI command mode: `localclaw channels serve [--once]`

Outputs:
- Accepted direct messages from allowlisted senders invoke runtime prompt flow for assigned agent/session.
- Runtime sends reply to sender via existing Signal adapter.
- Session metadata is updated through existing Signal send path.

Policy:
- Messages from senders not in `allow_from` are ignored.
- Group messages are always ignored.
- If inbound is enabled and `allow_from` is empty, config validation fails.

Errors:
- Misconfigured inbound policy fails startup validation.
- `channels serve` returns actionable errors for missing/disabled inbound Signal configuration.
- `signal-cli receive` execution errors are surfaced with stderr context.

## Test Plan

- `internal/config/config_test.go`
  - inbound allowlist requirements
  - sender->agent validation
- `internal/channels/signal/receive_test.go`
  - `signal-cli receive` JSON parsing and command construction
- `internal/runtime/channels_inbound_test.go`
  - allowlist enforcement
  - group-message drop behavior
  - sender->agent routing behavior
- `internal/cli/channels_test.go`
  - CLI mode/subcommand validation and once-run behavior
- `cmd/localclaw/main_test.go`
  - command help and known-command wiring for `channels`
