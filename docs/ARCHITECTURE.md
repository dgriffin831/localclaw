# localclaw Architecture (Implementation Detail)

This document reflects the current implemented architecture.

## 1. Scope and source of truth

Primary implementation anchors:

- Entrypoint: `cmd/localclaw/main.go`
- Runtime composition: `internal/runtime/app.go`
- Runtime tools + prompt assembly: `internal/runtime/tools.go`
- Config + compatibility mapping: `internal/config/config.go`
- Workspace lifecycle/bootstrap: `internal/workspace/manager.go`
- Session store/transcripts: `internal/session/*`
- Memory index/search/flush: `internal/memory/*`
- Session reset hook: `internal/hooks/session_memory.go`
- TUI runtime: `internal/tui/app.go`
- LLM adapter: `internal/llm/claudecode/client.go`
- Security boundary summary: `docs/SECURITY.md`

## 2. System context

```text
Operator (terminal)
      |
      v
localclaw binary (single process)
  |- config load + compatibility mapping + validation
  |- runtime wiring
  |   |- workspace manager (resolve + bootstrap templates)
  |   |- session store + transcript writer
  |   |- runtime tool registry (memory_search/memory_grep/memory_get)
  |   |- skills registry
  |   |- cron scheduler
  |   |- heartbeat monitor
  |   |- slack/signal local adapters
  |   `- Claude Code client (subprocess)
  `- command modes
      |- check
      |- tui
      `- memory {status,index,search,grep}
```

No server, gateway, or listener process exists.

## 3. Startup lifecycle

`App.Run(ctx)` startup order:

1. `workspace.Init`
2. bootstrap `~/.localclaw/localclaw.json` if missing
3. `memory.Init` (legacy store interface, no-op implementation)
4. `sessions.Init`
5. `skills.Load`
6. `cron.Start`
7. `heartbeat.Ping("localclaw startup heartbeat")`

Any failure aborts startup.

## 4. Runtime execution model

Prompt flow:

- `Prompt` and `PromptStream` call session-aware variants.
- `buildPromptInput` can inject workspace bootstrap context on first prompt for a session.
- Bootstrap context re-injects after compaction count increases.
- When memory tools are enabled (`agents.*.memorySearch.enabled`), prompt assembly appends:
  - memory recall policy text
  - runtime tool definitions (`memory_search`, `memory_grep`, `memory_get`)
  - resolved `session_key`

Session lifecycle:

- TUI appends user and assistant transcript messages to per-session JSONL files.
- Token estimates are tracked in `sessions.json` metadata (`totalTokens`).
- `/reset` and `/new` call `App.ResetSession`, which runs snapshot hook best-effort.
- `/new` rotates to a generated `s-YYYYMMDD-HHMMSS[-N]` session ID, avoiding collisions with existing session IDs and transcript files.

Memory/runtime tool behavior:

- Memory retrieval is keyword/FTS + grep based (`memory_search` and `memory_grep`).
- Runtime and memory CLI construct managers on demand using resolved workspace + state paths.
- Legacy `memory.Store` on `App` remains a minimal no-op compatibility surface.

## 5. Storage model

Default state root: `~/.localclaw`

```text
~/.localclaw/
  localclaw.json                        # scaffolded config file if missing
  memory/<agentId>.sqlite              # SQLite memory index store
  agents/<agentId>/sessions/sessions.json
  agents/<agentId>/sessions/<sessionId>.jsonl
  workspace/                            # when workspace config is "." for default agent
  workspace-<agentId>/                  # when workspace config is "." for non-default agent
```

Workspace bootstrap templates created when missing:

- `AGENTS.md`, `SOUL.md`, `TOOLS.md`, `IDENTITY.md`, `USER.md`, `HEARTBEAT.md`, `WELCOME.md`
- `BOOTSTRAP.md` only when a workspace is newly created

## 6. Local-only boundary

Config validation enforces:

- `security.enforce_local_only = true`
- `security.enable_gateway = false`
- `security.enable_http_server = false`
- `security.listen_address = ""`

Any violation fails startup before runtime wiring.
