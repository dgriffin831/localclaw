# localclaw

`localclaw` is a private, Go-only, single-process CLI agent runtime for secure enterprise environments.

## Design goals

- Local-first and local-only execution model.
- No network listeners, no gateway/server mode, and no browser/node-distributed runtime.
- Primary LLM integration via local Claude Code CLI.
- Support Claude Code CLI-compatible AWS GovCloud Bedrock auth/model flows.
- Preserve OpenClaw-style capabilities: memory, workspace, skills, cron, heartbeat.
- Channels limited to Slack and Signal.

## Current status

This repository is initialized as a greenfield MVP kickoff with:

- Foundational architecture and security docs.
- Minimal runnable CLI skeleton.
- Strict startup policy checks for local-only mode.
- Initial tests for config validation and local-only enforcement.

## Quick start

```bash
/usr/local/go/bin/go test ./...
/usr/local/go/bin/go run ./cmd/localclaw
```

Run full-screen TUI mode:

```bash
/usr/local/go/bin/go run ./cmd/localclaw tui
```

TUI controls:

- `Enter` send message
- `Alt+Enter` insert newline
- `Esc` abort active run
- `Ctrl+T` toggle thinking visibility
- `Ctrl+O` toggle tool-card mode
- `Ctrl+C` clear input (press twice quickly to exit)
- `Ctrl+D` exit when input is empty

TUI slash commands:

- `/help`
- `/status`
- `/clear`
- `/thinking <on|off>`
- `/verbose <on|off>`
- `/model <name>`
- `/exit`

Optional config file:

```bash
/usr/local/go/bin/go run ./cmd/localclaw -config ./localclaw.json
/usr/local/go/bin/go run ./cmd/localclaw -config ./localclaw.json tui
```

## Documentation map

- `AGENTS.md` - repository workflow, TDD loop, and validation gates.
- `docs/README.md` - docs index and structure.
- `docs/ARCHITECTURE.md` - implementation-detail architecture map.
- `docs/RUNTIME.md` - startup flow and command mode behavior.
- `docs/CONFIGURATION.md` - config schema/defaults/validation contract.
- `docs/TUI.md` - terminal UX behavior and controls.
- `docs/CLAUDE_CODE.md` - local Claude Code CLI integration details.
- `docs/TESTING.md` - test coverage and Red/Green command loops.
- `docs/specs` - feature specs and implementation plans.
- `docs/adr` - architecture decision records.

## Scope guardrails

- Go only.
- Monolithic single-process CLI only.
- Local-only tools: filesystem, local process execution, local scheduler.
- No remote tool bridges, no browser automation, no web server surfaces.
