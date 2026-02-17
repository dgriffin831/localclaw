# localclaw Architecture Snapshot

`localclaw` is a local-only, single-process Go CLI runtime.

## Runtime shape

- Entrypoint: `cmd/localclaw/main.go`
- Core runtime wiring: `internal/runtime/app.go`
- Prompt assembly and tool definitions: `internal/runtime/tools.go`
- TUI: `internal/tui`
- Session and transcript persistence: `internal/session`
- Workspace bootstrap/lifecycle: `internal/workspace`
- Memory indexing/search/grep: `internal/memory`
- Skills and MCP tool registry: `internal/skills`

## LLM providers

- Provider contract: `internal/llm/contracts.go`
- Claude Code CLI adapter: `internal/llm/claudecode/client.go`
- Codex CLI adapter: `internal/llm/codex/client.go`
- Both adapters execute via local subprocess invocation (`exec.CommandContext`).

## Command modes

- default (no command): CLI help
- `doctor`
- `tui`
- `memory`
- `mcp`

## Boundary constraints

- no HTTP/gRPC server or listener mode
- no gateway runtime
- local subprocess model execution only

Detailed architecture and behavior references are in `docs/ARCHITECTURE.md` and `docs/RUNTIME.md`.
