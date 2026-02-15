# localclaw Architecture (Implementation Detail)

This document reflects the current implemented architecture.

## 1. Scope and Source of Truth

Primary implementation anchors:
- Entrypoint: `cmd/localclaw/main.go`
- Runtime composition: `internal/runtime/app.go`
- Config + migration mapping: `internal/config/config.go`
- Workspace lifecycle: `internal/workspace/manager.go`
- Session store/transcripts: `internal/session/*`
- Memory index/search/migration: `internal/memory/*`
- TUI runtime: `internal/tui/app.go`
- LLM adapter: `internal/llm/claudecode/client.go`
- Security boundary summary: `docs/SECURITY.md`

## 2. System Context

```text
Operator (terminal)
      |
      v
localclaw binary (single process)
  |- config load + compatibility mapping + validation
  |- runtime wiring
  |   |- workspace manager (bootstrap + resolution)
  |   |- session store/transcript writer
  |   |- memory indexing/search + migration helper
  |   |- skills/tool registry
  |   |- cron scheduler
  |   |- heartbeat monitor
  |   |- slack/signal adapters
  |   `- Claude Code client (subprocess)
  `- command modes
      |- check
      |- tui
      `- memory {status,index,search}
```

No server, gateway, or listener process exists in the architecture.

## 3. Startup Lifecycle

`App.Run` startup order:
1. workspace init
2. one-time legacy memory import (`memory.path` JSON -> `MEMORY.md`) for default agent workspace
3. memory service init
4. session store init
5. skills load
6. cron start
7. heartbeat ping

The migration step writes `.localclaw-legacy-memory-import-v1` in the workspace to keep import idempotent.

## 4. Storage Model

Default state root: `~/.localclaw`

```text
~/.localclaw/
  memory/<agentId>.sqlite
  agents/<agentId>/sessions/sessions.json
  agents/<agentId>/sessions/<sessionId>.jsonl
  workspace/                  # default agent workspace when configured as "."
  workspace-<agentId>/        # additional agent workspaces
```

Workspace memory sources:
- `MEMORY.md` / `memory.md`
- `memory/**/*.md`
- optional configured extra paths
- optional session transcript source when enabled

## 5. Local-Only Boundary

Config validation enforces:
- `security.enforce_local_only = true`
- `security.enable_gateway = false`
- `security.enable_http_server = false`
- `security.listen_address = ""`

Any violation fails startup.
