# localclaw Architecture (Snapshot)

`localclaw` is a single-process, local-only Go CLI runtime.

## Runtime shape

- Entrypoint: `cmd/localclaw/main.go`
- Core wiring: `internal/runtime/app.go`
- Command modes: `check`, `tui`, `memory`
- No HTTP/gateway/server listeners.

## Startup order (`App.Run`)

1. workspace init
2. bootstrap default config file (`~/.localclaw/localclaw.json`) if missing
3. memory store init (legacy no-op store)
4. session store init
5. skills load
6. cron start
7. heartbeat ping

## Core boundaries

- Workspace + bootstrap templates: `internal/workspace`
- Sessions/transcripts: `internal/session`
- Memory index/search/flush/hooks: `internal/memory`, `internal/hooks`
- Runtime tool orchestration: `internal/runtime/tools.go`
- TUI: `internal/tui`
- Local Claude Code adapter: `internal/llm/claudecode`

Implementation details: `docs/ARCHITECTURE.md`.
