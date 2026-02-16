# Runtime Implementation Guide

This guide documents how `localclaw` boots and how runtime behavior is structured.

## Entrypoint and modes

Entrypoint: `cmd/localclaw/main.go`

Supported command modes:

- `check` (default): runs startup initialization checks, then verifies resolved workspace and session-store paths.
- `tui`: runs startup initialization, then starts Bubble Tea UI.
- `memory`: runs startup initialization, then executes memory subcommands (`status`, `index`, `search`).
- `mcp`: runs startup initialization, then serves stdio JSON-RPC MCP requests (`serve` subcommand).

Examples:

```bash
go run ./cmd/localclaw
go run ./cmd/localclaw check
go run ./cmd/localclaw tui
go run ./cmd/localclaw memory status
go run ./cmd/localclaw memory index --force
go run ./cmd/localclaw memory search "incident summary"
go run ./cmd/localclaw mcp serve
```

## Runtime construction

`runtime.New(cfg)`:

1. Re-validates config (`cfg.Validate()`).
2. Builds an `App` struct containing in-process modules.

`App` currently composes:

- workspace manager (`internal/workspace`)
- session store + transcript writer (`internal/session`)
- legacy memory store interface (`internal/memory/store.go` no-op implementation)
- runtime tool registry (`internal/skills` + `internal/runtime/tools.go`)
- cron scheduler and heartbeat monitor
- channel adapters (`slack`, `signal`)
- provider-agnostic LLM client contract (`internal/llm`) with Claude Code CLI adapter (`internal/llm/claudecode`)

## Startup lifecycle

`App.Run(ctx)` currently executes:

1. `workspace.Init`
2. bootstrap default config file at `~/.localclaw/localclaw.json` when missing
3. `memory.Init`
4. `sessions.Init`
5. `skills.Load`
6. `cron.Start`
7. `heartbeat.Ping("localclaw startup heartbeat")`

Any step failure aborts startup with wrapped context.

## Prompt APIs

Runtime exposes session-default and session-explicit prompt paths:

- `Prompt(ctx, input) (string, error)`
- `PromptStream(ctx, input) (<-chan llm.StreamEvent, <-chan error)`
- `PromptForSession(ctx, agentID, sessionID, input)`
- `PromptStreamForSession(ctx, agentID, sessionID, input)`

Prompt assembly (`buildPromptRequest` -> compatibility fallback prompt) behavior:

- Resolves `agentID/sessionID` into stable `session_key`.
- Injects workspace bootstrap context on first prompt in a session.
- Re-injects bootstrap context after compaction count increases.
- Injects memory recall policy + tool schema when memory tools are enabled.
- Injects a localclaw-authored skills block from workspace skill snapshots.
- Appends original user input under `User input:` in fallback mode.

Provider compatibility:

- If provider supports request options (`llm.RequestClient`), runtime passes structured request fields (`system_context`, tool defs, skills block, session metadata).
- If provider does not support request options, runtime composes one fallback prompt string and calls `Prompt` / `PromptStream`.

Structured tool loop:

- If provider advertises `StructuredToolCalls=true`, runtime intercepts `tool_call` events.
- Runtime executes policy checks + local/delegated routing, then emits `tool_result` events.
- Tool results are sent back to provider callbacks when present.
- Tool failures are non-fatal; stream continues unless run context is cancelled.

## Runtime tools

Runtime tool execution surface:

- `ToolDefinitions(agentID)`
- `ExecuteTool(ctx, ToolExecutionRequest)`

Supported tools:

- `memory_search`
- `memory_get`

Tool enablement:

- Controlled by resolved `memorySearch.enabled` for the agent.
- Disabled tools return graceful error payloads instead of panics.

Tool policy:

- Policy resolution precedence: global -> `agents.defaults.tools` -> `agents.list[].tools`.
- Deny list overrides allow list.
- Unknown tools are rejected with structured errors.
- Delegated tools are blocked unless delegated policy is enabled and allowlisted.

Skills snapshot behavior:

- Runtime loads workspace skills from `skills/<name>/SKILL.md`.
- Snapshot prompt blocks are cached per session key.
- Snapshot cache refreshes when session compaction count increases or session resets.

Tool manager construction:

- Uses resolved workspace path and session-root path.
- Resolves SQLite store path from `state.root` + `memorySearch.store.path`.
- Uses `memory.SQLiteIndexManager` for sync/search/get operations.

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

Memory flush behavior:

- `RunMemoryFlushIfNeeded` evaluates compaction-adjacent thresholds and workspace writability.
- `RunMemoryFlushIfNeededAsync` dispatches flush checks in a goroutine.

## Error and cancellation model

- OS signals (`SIGINT`, `SIGTERM`) cancel root context.
- TUI `Esc` cancels active run context.
- Claude CLI invocation uses `exec.CommandContext`, so cancellation terminates subprocesses.
- Tool failures are returned as structured errors in `ToolExecutionResult`.

## Extension rules

When extending runtime behavior:

- keep composition in `runtime.New`.
- keep startup ordering explicit and deterministic.
- add tests for policy and lifecycle changes.
- do not add listener/server/gateway startup paths.
