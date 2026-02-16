# localclaw

`localclaw` is a local-only, single-process Go CLI agent runtime.

## Design goals

- Local-first operation with no network listeners.
- Single-process Go runtime (no gateway/server mode).
- Local Claude Code CLI subprocess integration for LLM execution.
- Enterprise-safe boundaries: explicit local-only policy validation.
- In-process capabilities for workspace, sessions, memory, skills, cron, and heartbeat.

## Current implementation snapshot

- Command modes: `check` (default), `tui`, `memory`, `mcp`.
- Agent-aware workspace resolution and bootstrap templates.
- Per-agent session metadata + transcript files under the configured state root.
- SQLite-backed memory indexing/search with CLI tooling (`memory status/index/search`).
- Runtime memory tools (`memory_search`, `memory_get`) injected when memory tools are enabled.
- Session lifecycle hooks for `/reset` and `/new` snapshot behavior.

## Quick start

```bash
go test ./...
go run ./cmd/localclaw
go run ./cmd/localclaw tui
go run ./cmd/localclaw memory status
go run ./cmd/localclaw mcp serve
```

Run specific command modes:

```bash
go run ./cmd/localclaw check
go run ./cmd/localclaw tui
go run ./cmd/localclaw memory status
go run ./cmd/localclaw memory index --force
go run ./cmd/localclaw memory search "incident summary"
go run ./cmd/localclaw mcp serve
```

Run with an explicit config file:

```bash
go run ./cmd/localclaw -config ./localclaw.json check
go run ./cmd/localclaw -config ./localclaw.json tui
go run ./cmd/localclaw -config ./localclaw.json memory status
go run ./cmd/localclaw -config ./localclaw.json mcp serve
```

On startup, `localclaw` creates `~/.localclaw/localclaw.json` if it does not exist.
This scaffold file is not auto-loaded unless you pass `-config`.

## TUI controls

- `Enter` send message
- `Ctrl+J` insert newline
- `Tab` autocomplete selected slash command while typing `/...`
- `Shift+Tab` move slash-command selection backward
- `Up/Down` navigate slash-command suggestions when slash menu is open; otherwise they continue prompt history traversal only after a non-empty draft (or active history selection), and default to transcript scrolling from an empty draft
- `Ctrl+P` / `Ctrl+N` (also `Alt+Up` / `Alt+Down`) navigate prompt history
- `Mouse wheel` scroll transcript viewport
- `Esc` abort active run
- `Ctrl+T` toggle thinking visibility
- `Ctrl+O` expand/collapse tool-card details in the transcript
- `Ctrl+Y` toggle mouse capture (off enables standard text selection)
- `Ctrl+C` clear input (press twice quickly to exit)
- `Ctrl+D` exit when input is empty

TUI slash commands:

- `/help`
- `/shortcuts`
- `/status`
- `/tools`
- `/clear`
- `/reset`
- `/new`
- `/thinking <on|off>`
- `/verbose <on|off>`
- `/mouse <on|off>`
- `/model <name>` (currently a placeholder; override is not implemented)
- `/exit`
- `/quit`

`/tools` shows ownership split sections:
- `provider_native` (provider-discovered native tools)
- `localclaw_mcp` (localclaw MCP-exposed tools for the active agent)

`/verbose on` adds `[verbose]` system diagnostics to the transcript, including:
- prompt/session summary at run start
- runtime context (workspace/tool availability)
- stream lifecycle details (first delta/final counters/errors)
- transcript write summaries for user/assistant messages
- detailed tool call/result metadata

On TUI startup and on `/new`, `localclaw` renders workspace `WELCOME.md` (if present) as a system message.

Optional waiting-text customization:

- Set `app.thinking_messages` in config to rotate custom waiting text.
- Messages rotate once per submitted prompt while status is waiting and no stream delta has arrived.
- If unset, default waiting text is `thinking`.

## Documentation map

- `AGENTS.md` - repository workflow, TDD loop, and validation gates.
- `ARCHITECTURE.md` - concise architecture snapshot.
- `docs/README.md` - implementation docs index.
- `docs/ARCHITECTURE.md` - implementation-detail architecture map.
- `docs/RUNTIME.md` - startup flow and command mode behavior.
- `docs/CONFIGURATION.md` - config schema/defaults/validation contract.
- `docs/EMBEDDINGS.md` - local embedding runtime setup and Hugging Face model installation.
- `docs/TUI.md` - terminal UX behavior and controls.
- `docs/CLAUDE_CODE.md` - local Claude Code CLI integration details.
- `docs/TESTING.md` - package coverage and Red/Green command loops.
- `docs/SECURITY.md` - local-only security boundary and controls.
- `docs/specs/` - feature specs and design history.

## Scope guardrails

- Go only.
- Monolithic single-process CLI only.
- Local subprocess execution only for LLM integration.
- No HTTP/gRPC listeners, no gateway/server surfaces.
