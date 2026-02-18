# localclaw

`localclaw` is a local-only, single-process Go CLI agent runtime.

## Design goals

- Local-first operation with no network listeners.
- Single-process Go runtime (no gateway/server mode).
- Local Claude Code and OpenAI Codex CLI subprocess integrations for LLM execution.
- Enterprise-safe boundaries: hard local-only runtime constraints.
- In-process capabilities for workspace, sessions, memory, skills, cron, and heartbeat.

## Current implementation snapshot

- Command modes: `doctor`, `tui`, `backup`, `memory`, `channels`, `mcp`.
- Agent-aware workspace resolution and bootstrap templates, including `BOOTSTRAP.md` sentinel semantics (`exists` => setup pending; delete after setup to complete bootstrap).
- Security-mode execution policy via top-level `security.mode` (`full-access`, `sandbox-write`, `read-only`) with provider-specific flag translation.
- Per-agent session metadata + transcript files under the configured `app.root`.
- SQLite-backed memory indexing/search/grep with CLI tooling (`memory status/index/search/grep`).
- Runtime memory tools (`memory_search`, `memory_grep`, `memory_get`) injected when `agents.*.memory` enables them.
- Runtime cron tools (`localclaw_cron_list`, `localclaw_cron_add`, `localclaw_cron_remove`, `localclaw_cron_run`) with recurring local prompt execution while runtime is active.
- Outbound channel delivery tools for Slack (`localclaw_slack_send`) and Signal (`localclaw_signal_send`).
- Inbound Signal worker mode with sender allowlist policy and sender-to-agent routing (`channels serve`).
- Session lifecycle hooks for `/reset` and `/new` snapshot behavior.

## Quick start

```bash
go test ./...
go run ./cmd/localclaw
go run ./cmd/localclaw doctor
go run ./cmd/localclaw doctor --deep
go run ./cmd/localclaw tui
go run ./cmd/localclaw backup
go run ./cmd/localclaw memory status
go run ./cmd/localclaw channels serve --once
go run ./cmd/localclaw mcp serve
```

Run specific command modes:

```bash
go run ./cmd/localclaw doctor
go run ./cmd/localclaw doctor --deep
go run ./cmd/localclaw tui
go run ./cmd/localclaw backup
go run ./cmd/localclaw memory status
go run ./cmd/localclaw memory index --force
go run ./cmd/localclaw memory search "incident summary"
go run ./cmd/localclaw memory grep "incident-1234"
go run ./cmd/localclaw channels serve
go run ./cmd/localclaw mcp serve
```

Run with an explicit config file:

```bash
go run ./cmd/localclaw -config ./localclaw.json doctor
go run ./cmd/localclaw -config ./localclaw.json tui
go run ./cmd/localclaw -config ./localclaw.json backup
go run ./cmd/localclaw -config ./localclaw.json memory status
go run ./cmd/localclaw -config ./localclaw.json channels serve
go run ./cmd/localclaw -config ./localclaw.json mcp serve
```

Running `localclaw` with no command prints the detailed CLI help page.

On startup commands (`doctor`, `tui`, `memory`, `channels serve`, `mcp serve`), `localclaw` creates `~/.localclaw/localclaw.json` if it does not exist.
When `-config` is omitted, `localclaw` auto-loads `~/.localclaw/localclaw.json` when present.

Cron scheduling notes:

- recurring jobs run only while a runtime mode is active (`tui`, `mcp serve`, or another command that keeps `App.Run` alive).
- jobs are persisted at `<app.root>/cron/jobs.json` and reloaded on startup.
- missed windows while `localclaw` is not running are not backfilled.

Backup lifecycle notes:

- manual snapshots: `localclaw backup` creates `tar.gz` archives under `<app.root>/backups`.
- auto-save and auto-clean loops run only in long-running modes:
  - `tui`
  - `channels serve` (not `--once`)
  - `mcp serve`
- backup retention is count-based via `backup.retain_count`.

## TUI controls

- `Enter` send message (`/` commands run immediately; non-slash prompts queue FIFO while a run is active)
- `Shift+Enter` insert newline
- `Tab` autocomplete selected slash command while typing `/...`
- `Shift+Tab` move slash-command selection backward
- `Up/Down` navigate slash-command suggestions when slash menu is open; otherwise navigate prompt history
- `Ctrl+P` / `Ctrl+N` (also `Alt+Up` / `Alt+Down`) navigate prompt history
- `PgUp` / `PgDn` scroll transcript viewport by page
- `Ctrl+Up` / `Ctrl+Down` scroll transcript viewport by line
- `Mouse wheel` scroll transcript viewport
- `Esc` abort active run
- `Ctrl+O` expand/collapse tool-card details in the transcript
- `Ctrl+Y` toggle mouse capture (off enables standard text selection)
- `Ctrl+C` clear input (press twice quickly to exit)
- `Ctrl+D` exit when input is empty

When prompts are queued, the composer shows queued single-line previews above the input box in next-to-run order.

TUI slash commands:

- `/help`
- `/shortcuts`
- `/status`
- `/tools`
- `/models [refresh]`
- `/clear`
- `/reset`
- `/new`
- `/sessions`
- `/resume <session_id>`
- `/delete <session_id>`
- `/verbose <on|off>`
- `/mouse <on|off>`
- `/model <provider>/<model>[/<reasoning>]` (`/model <model>` keeps current provider; `/model default` or `/model off` resets to defaults)
- `/exit`
- `/quit`

`/tools` shows provider-reported tools only (source of truth for current session).
If tools are not yet discovered, `/tools` triggers a background probe and refreshes once metadata arrives.
For providers that omit explicit tool metadata in stream events (for example Codex), localclaw runs a provider-side JSON self-report probe as a fallback.

`/models` lists discovered provider model catalogs grouped by provider and includes an `active` summary.
`/models refresh` forces a re-discovery run.
`/model` changes the active provider/model/reasoning selector for the current TUI session and applies it to subsequent prompts and metadata probes.

`/verbose on` adds `[verbose]` system diagnostics to the transcript, including:
- prompt/session summary at run start
- runtime context (workspace/tool availability)
- stream lifecycle details (first delta/final counters/errors)
- transcript write summaries for user/assistant messages
- detailed tool call/result metadata

On TUI startup and on `/new`, `localclaw` renders workspace `WELCOME.md` (if present) as a system message.
When workspace `BOOTSTRAP.md` exists and the active session has no transcript yet, TUI auto-submits a first onboarding seed prompt: `Wake up, my friend!`.

Optional waiting-text customization:

- Set `app.thinking_messages` in config to rotate custom waiting text.
- Messages rotate once per submitted prompt while status is waiting and no stream delta has arrived.
- If unset, default waiting text is `thinking`.
- The status row icon is animated and uses the same `thinking` icon for all statuses.

Optional TUI startup defaults:

- Set `app.default.verbose` to control initial verbose diagnostics mode (`false` by default).
- Set `app.default.mouse` to control initial mouse capture (`false` by default).
- Set `app.default.tools` to control initial tool-card expansion (`false` by default).

## Documentation map

- `AGENTS.md` - repository workflow, TDD loop, and validation gates.
- `ARCHITECTURE.md` - concise architecture snapshot.
- `docs/README.md` - implementation docs index.
- `docs/ARCHITECTURE.md` - implementation-detail architecture map.
- `docs/RUNTIME.md` - startup flow and command mode behavior.
- `docs/CONFIGURATION.md` - config schema/defaults/validation contract.
- `docs/CRON.md` - recurring cron scheduling behavior, implementation, and usage.
- `docs/HEARTBEATS.md` - heartbeat scheduling behavior and `HEARTBEAT.md` authoring guide.
- `docs/MEMORY.md` - memory retrieval v2 model (`memory_search` + `memory_grep`) and implementation notes.
- `docs/TUI.md` - terminal UX behavior and controls.
- `docs/SLACK.md` - Slack setup and outbound delivery implementation details.
- `docs/SIGNAL.md` - Signal (`signal-cli`) setup and outbound delivery implementation details.
- `docs/CLAUDE_CODE.md` - local Claude Code CLI integration details.
- `docs/CODEX_CLI.md` - local Codex CLI integration details.
- `docs/TESTING.md` - package coverage and Red/Green command loops.
- `docs/SECURITY.md` - local-only security boundary and controls.
- `docs/specs/` - feature specs and design history.

## Scope guardrails

- Go only.
- Monolithic single-process CLI only.
- Local subprocess execution only for LLM integration.
- No HTTP/gRPC listeners, no gateway/server surfaces.
