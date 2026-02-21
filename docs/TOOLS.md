# Tools Architecture

This document defines the canonical tools architecture for `localclaw`.

## Core model

`localclaw` uses one tool execution model only:

- tools execute provider-side (Claude Code or Codex)
- local capabilities are exposed as MCP tools from `localclaw mcp serve`
- runtime does not execute a host-managed local tool loop during prompt streaming

This is a deliberate design choice and a regression guard.

## Source of truth

For an active prompt session, provider-reported tools are the source of truth.

- `/tools` in TUI uses provider metadata discovery, not a runtime fallback list
- if provider tools are not discovered yet, `/tools` reports `not discovered yet` (or `discovering...`) until discovery completes
- runtime metadata probe entrypoint: `internal/runtime/provider_tools.go`
- Codex fallback probe (when tool list is missing from stream metadata) uses constrained JSON self-report parsing in the same file

## End-to-end flow

1. Prompt starts through runtime:
   - `internal/runtime/app.go`
   - `internal/runtime/llm_runtime.go`
2. Provider adapter runs local CLI subprocess:
   - Claude: `internal/llm/claudecode/client.go`
   - Codex: `internal/llm/codex/client.go`
3. Provider invokes tools (including `mcp__localclaw__...`) using MCP wiring.
4. Adapter normalizes provider tool events into:
   - `StreamEventToolCall`
   - `StreamEventToolResult`
5. Runtime passes tool events through to TUI.
6. TUI renders tool cards from normalized events:
   - header: `tool <name> • <status>`
   - expanded metadata: `arg.*` and `data.*`

## MCP server implementation

MCP server runtime:

- entrypoint: `localclaw mcp serve`
- only `serve` is supported under `mcp`; positional args are rejected
- command wiring: `internal/cli/mcp.go`
- JSON-RPC server: `internal/mcp/server.go`

Supported MCP methods:

- `initialize`
- `tools/list`
- `tools/call`

Tool registration is centralized in `internal/cli/mcp.go` via `mcp.ToolRegistration`.

## Tool inventory (MCP-exposed)

Memory:

- `localclaw_memory_search`
- `localclaw_memory_get`
- `localclaw_memory_grep`

Workspace:

- `localclaw_workspace_status`

Cron:

- `localclaw_cron_list`
- `localclaw_cron_add`
- `localclaw_cron_remove`
- `localclaw_cron_run`

Sessions:

- `localclaw_sessions_list`
- `localclaw_sessions_history`
- `localclaw_sessions_delete`
- `localclaw_session_status`

Channels:

- `localclaw_slack_send`
- `localclaw_signal_send`

Tool definitions and schemas live in `internal/mcp/tools/*`.

## Runtime backends

Each MCP tool delegates to runtime methods (for example `MCPMemorySearch`, `MCPCronAdd`, `MCPSessionsList`) in:

- `internal/runtime/mcp_support.go`

Tool handlers return structured MCP responses with `ok` + typed payload fields.

## Provider normalization contract

Adapters normalize provider-specific tool payloads before TUI rendering:

- call args represent input context (`arg.*`)
- result data represents execution output context (`data.*`)
- delegated structured payloads prefer `structured_content` where available
- redundant wrappers are stripped where possible (`tool`, `server`, `arguments`, wrapper `result`)

Implementation:

- Codex normalization: `internal/llm/codex/client.go`
- Claude normalization: `internal/llm/claudecode/client.go`

TUI rendering behavior:

- renderer: `internal/tui/transcript.go`
- hides `data.provider_result`
- pretty-prints structured values as multiline fenced blocks
- deduplicates overlapping `arg.*` vs `data.*` metadata

## Policy and gating

MCP tool policy wrapper:

- `internal/mcp/tools/policy.go`
- applied in `internal/cli/mcp.go` (`applyMCPToolPolicy`)
- current `mcp serve` wiring builds policy with empty allow/deny lists, so tools are allowed unless a custom policy is injected in code/tests

Memory tool enablement also applies at runtime backend boundaries:

- checks in `internal/runtime/tools.go` and `internal/runtime/mcp_support.go`
- controlled by `agents.defaults.memory.*` and per-agent overrides

Provider-side allowlists can further restrict callable tools:

- Claude Code defaults include an explicit `--allowed-tools` MCP list from `internal/config/config.go` (`defaultClaudeAllowedMCPTools`)
- runtime prompt requests currently do not inject `ToolDefinitions`; provider-visible tools come from provider metadata and provider CLI configuration

## Architecture constraints

Do not add or restore:

- host-side runtime execution of tool calls from prompt stream events
- dual ownership/display paths that imply separate local execution lanes
- non-MCP tool execution surfaces for prompt-time tool calls

All prompt-time tool execution should continue to flow through provider orchestration plus MCP.
