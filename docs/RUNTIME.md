# Runtime Implementation Guide

This guide documents how `localclaw` boots and how runtime behavior is structured.

## Entrypoint and modes

Entrypoint: `cmd/localclaw/main.go`

Supported command modes:

- no command (default): renders detailed CLI help.
- `doctor`: runs startup initialization checks and validates resolved workspace/session-store paths with detailed output.
- `doctor --deep`: runs `doctor` checks plus deep checks (currently an LLM prompt probe).
- `tui`: runs startup initialization, then starts Bubble Tea UI.
- `memory`: runs startup initialization, then executes memory subcommands (`status`, `index`, `search`, `grep`).
- `mcp`: runs startup initialization, then serves stdio JSON-RPC MCP requests (`serve` subcommand).

Examples:

```bash
go run ./cmd/localclaw
go run ./cmd/localclaw doctor
go run ./cmd/localclaw doctor --deep
go run ./cmd/localclaw tui
go run ./cmd/localclaw memory status
go run ./cmd/localclaw memory index --force
go run ./cmd/localclaw memory search "incident summary"
go run ./cmd/localclaw memory grep "incident-1234"
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
- cron scheduler and heartbeat monitor
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
5. `cron.Start`
6. `heartbeat.Ping("localclaw startup heartbeat")`

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
- Injects workspace bootstrap context on first prompt in a session.
- Re-injects bootstrap context after compaction count increases.
- Injects a localclaw-authored skills block from workspace skill snapshots.
- Carries provider-agnostic prompt options (for example model override) to request-capable adapters.

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

Supported tools:

- `memory_search`
- `memory_grep`
- `memory_get`
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
