# localclaw

`localclaw` is a local-only, single-process Go CLI for running agent workflows on your machine.

It keeps execution boundaries strict: no hosted gateway, no inbound server runtime, and no direct model HTTP clients. LLM execution runs through local CLI subprocesses (Claude Code or Codex), with local capabilities exposed through an MCP server (`localclaw mcp serve`).
change as features are refined.

## Key Features

- Local-only, single-process runtime (`cmd/localclaw`).
- Command modes: `doctor`, `tui`, `backup`, `memory`, `channels`, `mcp`.
- Bubble Tea TUI with streaming responses, slash commands, and session continuation.
- Local provider adapters for Claude Code CLI and Codex CLI.
- MCP tool surface for memory, cron, sessions, workspace status, Slack send, and Signal send.
- SQLite-backed memory indexing/search/grep flows.
- Recurring cron jobs and periodic heartbeat prompts.
- Optional Signal inbound worker with allowlist-based sender routing.

## Quick Start

Prerequisites:

- Go `1.24.2+`
- One local provider CLI configured and available on `PATH`:
  - `claude` (Claude Code), or
  - `codex` (Codex CLI)

Run from source:

```bash
go test ./...
go run ./cmd/localclaw doctor
go run ./cmd/localclaw tui
```

Useful command examples:

```bash
go run ./cmd/localclaw memory status
go run ./cmd/localclaw memory search "incident summary"
go run ./cmd/localclaw backup
go run ./cmd/localclaw channels serve --once
go run ./cmd/localclaw mcp serve
```

If `-config` is omitted, `localclaw` loads `~/.localclaw/localclaw.json` when present; otherwise defaults are used. Commands that initialize runtime state also create `~/.localclaw/localclaw.json` when missing.

## Configuration Basics

Top-level config sections:

- `app`, `security`, `llm`, `channels`, `agents`, `session`, `backup`, `cron`, `heartbeat`

Minimal example:

```json
{
  "security": {
    "mode": "sandbox-write"
  },
  "llm": {
    "provider": "claudecode"
  }
}
```

Important defaults:

- `security.mode` defaults to `sandbox-write`.
- `llm.provider` defaults to `claudecode`.
- `app.root` defaults to `~/.localclaw`.

See full schema, defaults, and validation rules in [`docs/CONFIGURATION.md`](docs/CONFIGURATION.md).

## Command Modes

- `localclaw doctor` - startup checks and runtime diagnostics (`--deep` also probes the active LLM provider).
- `localclaw tui [initial-prompt]` - full-screen terminal UI (always-on mouse reporting; see `docs/TUI.md` for terminal-specific text-selection bypass keys).
- `localclaw backup` - create one compressed local backup archive.
- `localclaw memory <status|index|search|grep>` - memory tooling.
- `localclaw channels serve [--once]` - channel worker mode (Signal inbound processing).
- `localclaw mcp serve` - stdio MCP server.

## Documentation

- [Documentation index](docs/README.md)
- [Runtime lifecycle and command behavior](docs/RUNTIME.md)
- [Configuration reference](docs/CONFIGURATION.md)
- [Installation guide](docs/INSTALL.md)
- [Tools and MCP architecture](docs/TOOLS.md)
- [TUI behavior and controls](docs/TUI.md)
- [Memory model and workflows](docs/MEMORY.md)
- [Cron scheduling](docs/CRON.md)
- [Heartbeat behavior](docs/HEARTBEATS.md)
- [Sessions model](docs/SESSIONS.md)
- [Slack channel integration](docs/SLACK.md)
- [Signal channel integration](docs/SIGNAL.md)
- [Claude Code adapter](docs/CLAUDE_CODE.md)
- [Codex adapter](docs/CODEX_CLI.md)
- [Security boundaries](docs/SECURITY.md)
- [Testing guide](docs/TESTING.md)

## Notes

- Local-only boundary is intentional: no HTTP/gRPC gateway mode.
- Detailed implementation specifics are intentionally kept in `docs/`.
- See repository workflow/testing expectations in [`AGENTS.md`](AGENTS.md).
