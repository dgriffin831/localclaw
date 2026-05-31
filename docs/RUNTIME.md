# Runtime

`localclaw` has one runtime boundary: local workspace and memory capabilities exposed over stdio MCP.

## Commands

- `doctor`: initializes runtime state and reports workspace diagnostics.
- `memory`: initializes runtime state, then runs `status`, `index`, `search`, or `grep`.
- `mcp serve`: initializes runtime state and serves MCP over stdin/stdout.
- `setup <claude|codex|opencode>`: invokes the selected harness with a setup prompt.

Removed command modes such as `tui`, `backup`, `channels`, and scheduler-related commands are intentionally unsupported.

## Startup Order

`App.Run(ctx)`:

1. Ensures configured workspaces exist.
2. Bootstraps workspace markdown files when missing.
3. Creates default config at `~/.localclaw/localclaw.json` if absent.

No model calls, background schedulers, channel workers, or hosted listeners are started.

## Setup Commands

Setup commands are harness-driven and do not write Claude/Codex/opencode config directly.

Each setup command asks the harness to configure a stdio MCP server:

- server name: `localclaw`
- command: `localclaw`
- args: `["mcp", "serve"]`

Use `--dry-run` to print the prompt.

## MCP Runtime

`localclaw mcp serve` supports:

- `initialize`
- `tools/list`
- `tools/call`

Tool handlers return structured results with `ok` and typed payload fields.
