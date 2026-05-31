# Architecture

`localclaw` is a local-only stdio MCP server focused on persistent workspace and markdown memory.

## Components

- Entrypoint: `cmd/localclaw/main.go`
- Runtime composition: `internal/runtime`
- Configuration: `internal/config`
- Workspace bootstrap/resolution: `internal/workspace`
- SQLite keyword/vector memory index/search: `internal/memory`
- MCP protocol/server: `internal/mcp`
- MCP tool definitions: `internal/mcp/tools`
- Command helpers: `internal/cli`

## Runtime Lifecycle

`App.Run` performs only local setup:

1. Ensure default and configured agent workspaces exist.
2. Bootstrap workspace control markdown files when missing.
3. Write the default `~/.localclaw/localclaw.json` if it does not already exist.

There is no TUI, scheduler, channel worker, hosted gateway, provider subprocess loop, or model client.

## Command Surface

```text
localclaw
|- doctor
|- memory {status,index,search,grep,model}
|- mcp {serve}
`- setup {claude,codex,opencode}
```

## MCP Tool Surface

- `localclaw_workspace_status`
- `localclaw_workspace_list`
- `localclaw_workspace_read`
- `localclaw_memory_create`
- `localclaw_memory_search`
- `localclaw_memory_get`
- `localclaw_memory_grep`

## State Layout

Default state root: `~/.localclaw`

```text
~/.localclaw/
  localclaw.json
  memory/<agentId>.sqlite
  models/embeddinggemma-300M-Q8_0.gguf
  workspace/
```

Workspace bootstrap files include:

- `AGENTS.md`
- `SOUL.md`
- `TOOLS.md`
- `IDENTITY.md`
- `USER.md`
- `SECURITY.md`
- `HEARTBEAT.md`
- `WELCOME.md`
- `BOOTSTRAP.md` for brand-new workspaces

`HEARTBEAT.md` is retained as a harness-readable workspace instruction file; LocalClaw no longer runs heartbeat scheduling itself.
