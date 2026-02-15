# localclaw Architecture (Snapshot)

`localclaw` is a single-process, local-only Go CLI runtime.

## Runtime shape

- Entrypoint: `cmd/localclaw/main.go`
- Core wiring: `internal/runtime/app.go`
- Command modes: `check`, `tui`, `memory`
- No HTTP/gateway/server listeners.

## Startup order

1. workspace init
2. one-time legacy memory import (`memory.path` JSON -> `MEMORY.md`) with marker `.localclaw-legacy-memory-import-v1`
3. memory init
4. session init
5. skills load
6. cron start
7. heartbeat ping

## Core boundaries

- Workspace: `internal/workspace`
- Sessions/transcripts: `internal/session`
- Memory index/search/migrations: `internal/memory`
- TUI: `internal/tui`
- Local Claude Code adapter: `internal/llm/claudecode`

Implementation details: `docs/ARCHITECTURE.md`.
