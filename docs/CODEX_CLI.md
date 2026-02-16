# Codex Integration

`localclaw` supports Codex as a local CLI subprocess provider.

Implementation location:

- `internal/llm/codex/client.go`

## Execution model

- `PromptStreamRequest` executes:
  - `codex exec --json -C <workspace> ... -`
  - optional `-p <profile>` from `llm.codex.profile`
  - optional `-m <model>` from `llm.codex.model` or runtime override
  - optional passthrough args from `llm.codex.extra_args`
- prompt text is written to stdin (`-` argument) using `exec.CommandContext`.
- stdout JSONL events are parsed into provider-agnostic stream events:
  - `item.completed` with `agent_message` -> `StreamEventDelta`
  - `item.started` with `command_execution` -> `StreamEventToolCall` (`provider_native`)
  - `item.completed` with `command_execution` -> `StreamEventToolResult` (`provider_native`)
  - `session.configured` -> `StreamEventProviderMetadata` (provider/model/tools when available)
- if no explicit final event appears, adapter emits `StreamEventFinal` from aggregated deltas.

`Prompt`/`PromptRequest` are synchronous wrappers over streaming:

- aggregate deltas
- prefer final event text when present
- otherwise return trimmed aggregated delta text

## Model override behavior

- Runtime request options can include `model_override`.
- Codex adapter applies `model_override` first when present.
- If no override is provided, configured `llm.codex.model` is used.

## MCP wiring and config

- Adapter ensures Codex MCP server wiring in TOML config before each run.
- MCP server entry defaults to:
  - name: `localclaw`
  - command: `localclaw`
  - args: `["mcp","serve"]`
- config path resolution precedence:
  1. explicit `llm.codex.mcp.config_path`
  2. isolated home (`llm.codex.mcp.use_isolated_home` + optional `home_path`)
  3. `CODEX_HOME` env fallback
  4. `~/.codex/config.toml`

## Input validation and cancellation

- Empty/whitespace prompt input fails fast with `input is required`.
- Process cancellation relies on `exec.CommandContext`.

## Error behavior

- pipe setup/start failures return immediately with context.
- stdin write/close failures are surfaced as adapter errors.
- scanner/read errors are returned with context.
- non-zero exits include stderr text when available.
