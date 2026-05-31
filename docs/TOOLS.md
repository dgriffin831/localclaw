# Tools Architecture

LocalClaw exposes local capabilities only through stdio MCP.

## MCP Server

- Entrypoint: `localclaw mcp serve`
- Transport: stdin/stdout JSON-RPC
- Supported methods: `initialize`, `tools/list`, `tools/call`
- Server implementation: `internal/mcp/server.go`
- Tool registration: `internal/cli/mcp.go`

## Tool Inventory

Workspace:

- `localclaw_workspace_status`
- `localclaw_workspace_list`
- `localclaw_workspace_read`

Memory:

- `localclaw_memory_create`
- `localclaw_memory_search`
- `localclaw_memory_get`
- `localclaw_memory_grep`

Removed tools for cron, sessions, Slack, and Signal are intentionally not registered.

## Workspace Tools

- `workspace_status`: returns resolved workspace path and existence.
- `workspace_list`: lists readable control markdown and memory markdown files.
- `workspace_read`: reads allowed markdown files such as `SOUL.md`, `AGENTS.md`, or `memory/*.md`.

All workspace file reads are scoped to the resolved workspace and reject traversal.

## Memory Tools

- `memory_create`: creates a markdown note under `memory/` and refreshes the index.
- `memory_search`: ranked hybrid keyword/vector retrieval over indexed markdown chunks.
- `memory_get`: line-sliced read of an in-scope memory markdown file.
- `memory_grep`: literal/regex line matching across in-scope memory files.

Memory tool enablement is controlled by `agents.defaults.memory.tools` and per-agent overrides.
