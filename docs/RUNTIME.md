# Runtime Implementation Guide

This guide documents how `localclaw` boots and how runtime behavior is structured.

## Entrypoint and modes

Entrypoint: `cmd/localclaw/main.go`

Supported command modes:

- no command (default): renders detailed CLI help.
- `doctor`: runs startup initialization checks and validates resolved workspace/session-store paths with detailed output.
- `doctor --deep`: runs `doctor` checks plus deep checks (currently an LLM prompt probe).
- `tui`: runs startup initialization, then starts Bubble Tea UI.
- `backup`: creates one compressed backup archive under `<app.root>/backups`.
- `memory`: runs startup initialization, then executes memory subcommands (`status`, `index`, `search`, `grep`).
- `channels`: runs startup initialization, then runs channel workers (`serve` subcommand).
- `mcp`: runs startup initialization, then serves stdio JSON-RPC MCP requests (`serve` subcommand).

Examples:

```bash
go run ./cmd/localclaw
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

## Runtime construction

`runtime.New(cfg)`:

1. Re-validates config (`cfg.Validate()`).
2. Builds an `App` struct containing in-process modules.

`App` currently composes:

- workspace manager (`internal/workspace`)
- session store + transcript writer (`internal/session`)
- runtime tool registry (`internal/skills` + `internal/runtime/tools.go`)
- cron scheduler (persistent recurring jobs) and heartbeat monitor
- channel adapters (`slack`, `signal`) wired only when present in `channels.enabled`
- provider-agnostic LLM client contract (`internal/llm`) with local CLI adapters:
  - Claude Code (`internal/llm/claudecode`)
  - Codex (`internal/llm/codex`)

## Startup lifecycle

`App.Run(ctx)` currently executes:

1. `workspace.Init`
2. bootstrap default config file at `~/.localclaw/localclaw.json` when missing
3. `sessions.Init`
4. `skills.Load`
5. `cron.Start` (loads `<app.root>/cron/jobs.json` and starts background scheduling loop)
6. `heartbeat.Ping("localclaw startup heartbeat")`
7. start heartbeat background loop (when enabled) using `heartbeat.interval_seconds`
   - each tick submits a local prompt that references workspace `HEARTBEAT.md`
   - heartbeat tick errors are logged and do not fail runtime startup

Any step failure aborts startup with wrapped context.

## Prompt APIs

Runtime exposes session-default and session-explicit prompt paths:

- `Prompt(ctx, input) (string, error)`
- `PromptStream(ctx, input) (<-chan llm.StreamEvent, <-chan error)`
- `PromptForSession(ctx, agentID, sessionID, input)`
- `PromptStreamForSession(ctx, agentID, sessionID, input)`

Prompt assembly (`buildPromptRequest`) behavior:

- Resolves `agentID/sessionID` into stable `session_key`.
- Adds provider metadata (`provider`) and persisted provider-native session ID (`provider_session_id`) when available.
- Adds resolved workspace path (`workspace_path`) and configured security mode (`security_mode`) into request session metadata.
- Injects workspace bootstrap context on first prompt in a session when `BOOTSTRAP.md` exists (sentinel for pending setup).
- Re-injects bootstrap context after compaction count increases.
- Injects a localclaw-authored skills block from workspace skill snapshots.
- Carries provider-agnostic prompt options (for example model override) to request-capable adapters.
- Prompt request construction fails if active workspace resolution fails.

Provider compatibility:

- Runtime requires provider support for request options (`llm.RequestClient` + `SupportsRequestOptions=true`).
- If request options are unavailable, runtime returns an error instead of falling back to prompt-string compatibility mode.

MCP-first hard cutover:

- Runtime no longer intercepts or executes provider-emitted structured `tool_call` events in the prompt stream path.
- `PromptStreamForSession` forwards provider stream events and wraps them with continuation persistence behavior:
  - intercepts `provider_metadata` events with `session_id`
  - persists session IDs per provider in session metadata (`providerSessionIds`)
  - if a resume call fails with recognized stale/invalid session errors, clears the persisted provider session ID and retries once with a fresh provider session.
- Provider-native and localclaw MCP execution happens provider-side; runtime remains local-only orchestrator and transcript/session manager.

## Runtime tools

Runtime-defined tool registry entries:

- `memory_search`
- `memory_grep`
- `memory_get`

MCP server tools (`localclaw mcp serve`):

- `localclaw_memory_search`
- `localclaw_memory_grep`
- `localclaw_memory_get`
- `localclaw_workspace_status`
- `localclaw_cron_list`
- `localclaw_cron_add`
- `localclaw_cron_remove`
- `localclaw_cron_run`
- `localclaw_sessions_list`
- `localclaw_sessions_history`
- `localclaw_sessions_delete`
- `localclaw_session_status`
- `localclaw_slack_send`
- `localclaw_signal_send`

Retrieval model details and migration notes are documented in `docs/MEMORY.md`.

Tool enablement:

- Controlled by resolved `memory.enabled` and `memory.tools.{search,get,grep}` for the agent.
- Disabled tools return graceful error payloads instead of panics.
- `ToolDefinitions(agentID)` only reports locally available memory tools for UI/status surfaces.

Channel dispatch behavior:

- Runtime sends Slack and Signal through MCP runtime methods:
  - `MCPSlackSend`
  - `MCPSignalSend`
- Calls are gated by `channels.enabled`.
- Disabled channel sends return `channel \"<name>\" is disabled`.
- When `agent_id`/`session_id` are provided, runtime persists channel delivery metadata into session entries.

Signal inbound behavior:

- `channels serve` currently runs Signal inbound processing only.
- Runtime polls `signal-cli receive` using JSON output (`-o json`) and local subprocess execution.
- Allowed inbound senders are enforced by `channels.signal.inbound.allow_from`.
- Group messages are always dropped (never routed/executed).
- Accepted direct messages are routed to agent sessions using:
  - `channels.signal.inbound.agent_by_sender`
  - fallback `channels.signal.inbound.default_agent`
- Optional read receipts are sent for accepted direct messages when `channels.signal.inbound.send_read_receipts=true`.
- Optional typing indicators are sent/refreshed while reply generation runs when `channels.signal.inbound.send_typing=true`.
- Session ids are derived per sender (`signal-<digits>`) to keep per-sender thread continuity.

Cron behavior:

- `cron.enabled=false` disables scheduler startup and all MCP cron methods (`cron scheduler is disabled`).
- schedules support 5-field cron expressions plus macros: `@yearly`, `@annually`, `@monthly`, `@weekly`, `@daily`, `@hourly`, `@reboot`.
- job metadata persists latest run outcome fields (`lastRunAt`, `lastRunStatus`, `lastRunError`, `lastRunDurationMs`).
- jobs execute runtime prompts using `agent_id`, `message`, `timeout_seconds`, and `session_target`.
- `wake_mode` is normalized/persisted (`next-heartbeat` or `now`) and currently does not change execution path.
- `session_target` values are `default` and `isolated` (defaulting to `isolated` when omitted).
- `localclaw_cron_run` uses the same prompt execution/status path as scheduled runs.
- `@reboot` jobs run once per scheduler start.
- missed schedules while runtime is offline are not backfilled.

Heartbeat behavior:

- `heartbeat.enabled=false` disables recurring heartbeat ticks.
- `heartbeat.interval_seconds` controls recurring heartbeat cadence.
- each tick resolves default workspace `HEARTBEAT.md` and skips/logs when file read fails.
- each successful tick submits a local prompt in `default/main` that references `HEARTBEAT.md`.
- overlapping heartbeat executions are skipped while a prior tick is still running.
- heartbeat tick failures are non-fatal; future ticks continue.

Backup behavior:

- `localclaw backup` creates one `tar.gz` snapshot under `<app.root>/backups`.
- backup auto-save/auto-clean loops are started only in long-running command paths:
  - `tui`
  - `channels serve` (excluding `--once`)
  - `mcp serve`
- auto-save/auto-clean cadence uses `backup.interval`.
- auto-clean keeps newest matching archives by `backup.retain_count`.
- backup and cleanup runs are serialized; overlapping ticks are skipped and logged.
- background backup errors are logged and do not terminate the active command mode.

Skills snapshot behavior:

- Runtime loads workspace skills from `skills/<name>/SKILL.md`.
- Snapshot prompt blocks are cached per session key.
- Snapshot cache refreshes when session compaction count increases or session resets.

Tool manager construction:

- Uses resolved workspace path and session-root path.
- Resolves SQLite store path from `app.root` + `memory.store.path`.
- Uses `memory.SQLiteIndexManager` for sync/search/get/grep operations.

## Session helpers and lifecycle hooks

Runtime helpers:

- `ResolveSession(agentID, sessionID)` and defaults (`default/main`)
- `ResolveWorkspacePath`, `ResolveSessionsPath`, `ResolveTranscriptPath`
- `AddSessionTokens` for coarse token accounting
- `AppendSessionTranscriptMessage` for transcript persistence

Reset behavior:

- `ResetSession` runs session memory snapshot hook best-effort.
- Hook failures are logged but non-fatal.
- `StartNew=true` rotates to a unique timestamp-based session ID.
- `StartNew=false` clears persisted provider continuation IDs for the current local session.

Memory flush behavior:

- `RunMemoryFlushIfNeeded` evaluates compaction-adjacent thresholds and workspace writability.
- `RunMemoryFlushIfNeededAsync` dispatches flush checks in a goroutine.

## Error and cancellation model

- OS signals (`SIGINT`, `SIGTERM`) cancel root context.
- TUI `Esc` cancels active run context.
- Claude CLI invocation uses `exec.CommandContext`, so cancellation terminates subprocesses.
- MCP tool failures are returned from MCP handlers as structured tool errors.

## Extension rules

When extending runtime behavior:

- keep composition in `runtime.New`.
- keep startup ordering explicit and deterministic.
- add tests for policy and lifecycle changes.
- do not add listener/server/gateway startup paths.
