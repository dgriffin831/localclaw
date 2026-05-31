# Repository Guidelines

This file is the source of truth for agentic coding practices in `localclaw`.

## Product Boundary

`localclaw` is a local-only stdio MCP server for persistent workspace and markdown memory.

- Keep the process model single-process CLI only.
- Do not add HTTP/gRPC servers, gateway mode, or listeners.
- Do not add LocalClaw-owned chat execution, model API clients, provider adapters, TUI, cron, channels, backup loops, or LocalClaw skills.
- Claude, Codex, and opencode own model execution, sessions, scheduling, and skills.

## Stack

- Go `1.24.2`
- Entrypoint: `cmd/localclaw/main.go`
- Config: `internal/config`
- Runtime wiring: `internal/runtime`
- Workspace bootstrap: `internal/workspace`
- Memory index/search: `internal/memory`
- MCP server/tools: `internal/mcp`, `internal/mcp/tools`
- CLI helpers: `internal/cli`

## Commands

```bash
go test ./...
go fmt ./...
go run ./cmd/localclaw doctor
go run ./cmd/localclaw memory status
go run ./cmd/localclaw memory index --force
go run ./cmd/localclaw memory search "incident summary"
go run ./cmd/localclaw memory grep "token-123"
go run ./cmd/localclaw memory model status
go run ./cmd/localclaw mcp serve
go run ./cmd/localclaw setup opencode --dry-run
```

Supported command modes:

- `doctor`
- `memory`
- `mcp`
- `setup`

## TDD Workflow

Behavior changes should follow Red -> Green -> Validate -> Deliver.

1. Define expected inputs, outputs, errors, and unchanged behavior.
2. Add the smallest targeted failing test.
3. Run a focused test and confirm the intended failure.
4. Implement the minimum fix.
5. Re-run focused tests, then `go test ./...`.
6. Run `go fmt ./...` when Go files changed.
7. Summarize behavior and validation.

## Configuration Rules

- Config decoding is strict; removed/unknown keys should fail.
- Top-level config is limited to `app` and `agents`.
- Any new config field must update:
  - structs
  - defaults
  - validation
  - `docs/CONFIGURATION.md`
- Supported memory source is `memory`.

## MCP Rules

The default tool surface is only:

- `localclaw_workspace_status`
- `localclaw_workspace_list`
- `localclaw_workspace_read`
- `localclaw_memory_create`
- `localclaw_memory_search`
- `localclaw_memory_get`
- `localclaw_memory_grep`

All workspace and memory file tools must enforce resolved-workspace scoping and reject traversal.

## Setup Rules

`localclaw setup <claude|codex|opencode>` must call the real harness with instructions. It must not directly edit Claude, Codex, or opencode config files.

## Docs

Keep these aligned with behavior:

- `README.md`
- `UPGRADE.md`
- `docs/ARCHITECTURE.md`
- `docs/RUNTIME.md`
- `docs/CONFIGURATION.md`
- `docs/TOOLS.md`
- `docs/MEMORY.md`
- `docs/SECURITY.md`
- `docs/TESTING.md`

## Git Hygiene

- Use Conventional Commit prefixes when committing.
- Stage intentionally; avoid blind `git add .`.
- Never commit secrets, local env files, editor caches, compiled binaries, or build artifacts.
