# localclaw Architecture (Implementation Detail)

This document reflects the current implemented architecture.

## 1. Scope and source of truth

Primary implementation anchors:

- Entrypoint: `cmd/localclaw/main.go`
- Runtime composition: `internal/runtime/app.go`
- Runtime tools + prompt assembly: `internal/runtime/tools.go`
- Config loading + strict validation: `internal/config/config.go`
- Workspace lifecycle/bootstrap: `internal/workspace/manager.go`
- Session store/transcripts: `internal/session/*`
- Memory index/search/flush: `internal/memory/*`
- Session reset hook: `internal/hooks/session_memory.go`
- TUI runtime: `internal/tui/app.go`
- LLM adapters:
  - `internal/llm/claudecode/client.go`
  - `internal/llm/codex/client.go`
- Security boundary summary: `docs/SECURITY.md`

## 2. System context

```text
Operator (terminal)
      |
      v
localclaw binary (single process)
  |- config load + strict decode + validation
  |- runtime wiring
  |   |- workspace manager (resolve + bootstrap templates)
  |   |- session store + transcript writer
  |   |- runtime tool registry (memory tools + workspace/session/cron/channel MCP tools)
  |   |- skills registry
  |   |- cron scheduler
  |   |- heartbeat monitor
  |   |- slack/signal local adapters
  |   |- Claude Code client (subprocess)
  |   `- Codex client (subprocess)
  `- command modes
      |- default => help
      |- doctor
      |- tui
      |- memory {status,index,search,grep}
      |- channels {serve}
      `- mcp {serve}
```

No server, gateway, or listener process exists.

## 3. Startup lifecycle

`App.Run(ctx)` startup order:

1. `workspace.Init`
2. bootstrap `~/.localclaw/localclaw.json` if missing
3. `sessions.Init`
4. `skills.Load`
5. `cron.Start` (load persisted cron jobs + start in-process scheduling loop)
6. `heartbeat.Ping("localclaw startup heartbeat")`
7. `heartbeat.Start` (background ticker loop; overlapping ticks are skipped)

Any failure aborts startup.

## 4. Runtime execution model

Prompt flow:

- `Prompt` and `PromptStream` call session-aware variants.
- `buildPromptRequest` injects workspace bootstrap context on first prompt for a session.
- Bootstrap context re-injects after compaction count increases.
- Prompt streaming is request-based only; runtime does not use compatibility fallback prompt composition.

Session lifecycle:

- TUI appends user and assistant transcript messages to per-session JSONL files.
- Token estimates are tracked in `sessions.json` metadata (`totalTokens`).
- `/reset` and `/new` call `App.ResetSession`, which runs snapshot hook best-effort.
- `/new` rotates to a generated `s-YYYYMMDD-HHMMSS[-N]` session ID, avoiding collisions with existing session IDs and transcript files.

Memory/runtime tool behavior:

- Memory retrieval is keyword/FTS + grep/file-read based (`memory_search`, `memory_grep`, `memory_get`).
- Runtime and memory CLI construct managers on demand using resolved workspace + `app.root`-based paths.
- Cron scheduler stores jobs under `app.root` and executes local prompt messages while runtime modes are active.

## 5. Storage model

Default state root: `~/.localclaw`

```text
~/.localclaw/
  localclaw.json                        # scaffolded config file if missing
  memory/<agentId>.sqlite              # SQLite memory index store
  cron/jobs.json                       # persisted cron jobs + latest run metadata
  agents/<agentId>/sessions/sessions.json
  agents/<agentId>/sessions/<sessionId>.jsonl
  workspace/                            # when workspace config is "." for default agent
  workspace-<agentId>/                  # when workspace config is "." for non-default agent
```

Workspace bootstrap templates created when missing:

- `AGENTS.md`, `SOUL.md`, `TOOLS.md`, `IDENTITY.md`, `USER.md`, `HEARTBEAT.md`, `WELCOME.md`
- `BOOTSTRAP.md` only when a workspace is newly created

## 6. Local-only boundary

Local-only posture is architecture-level and non-configurable:

- single-process CLI runtime only
- no HTTP/gRPC server mode
- no gateway/listener config surface
- model execution via local subprocess adapters only
