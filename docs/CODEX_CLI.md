# Codex Integration

`localclaw` supports Codex as a local CLI subprocess provider.

Implementation location:

- `internal/llm/codex/client.go`

## Execution model

- `PromptStreamRequest` executes:
  - non-resume baseline: `codex exec --json -C <workspace> ... -`
  - resume path: `codex exec <resume_args...> ... -` with `--json` controlled by `llm.codex.resume_output`
    - `json` / `jsonl`: keep `--json`
    - `text`: omit `--json`
  - optional `-p <profile>` from `llm.codex.profile` (non-resume path)
  - optional `-m <model>` from `llm.codex.model` or runtime override
  - optional `-c model_reasoning_effort="<level>"` from runtime/default selector reasoning
  - optional passthrough args from `llm.codex.extra_args`
    - default config includes `--skip-git-repo-check`
- request input is composed with `llm.ComposePromptFallback(req)`, then written to stdin (`-` argument) using `exec.CommandContext`.
- stdout JSONL events are parsed into provider-agnostic stream events:
  - `item.completed` with `agent_message` -> `StreamEventDelta`
  - `item.started` with `command_execution` -> `StreamEventToolCall`
  - `item.completed` with `command_execution` -> `StreamEventToolResult`
  - `item.started`/`item.completed` for delegated non-message tool types (for example `web_search`, `mcp_tool_call`) -> `StreamEventToolCall`/`StreamEventToolResult`
    - delegated call args are normalized to input context (for example flattened `arguments`/`input` payloads without wrapper duplication)
    - delegated result data prefers structured execution payloads (`result.structured_content` when present) and omits redundant wrappers (`server`, `tool`, `arguments`, `result`)
  - `session.configured` -> `StreamEventProviderMetadata` (provider/model/tools when available)
  - `thread.started` -> provider session ID metadata
- Note: some Codex CLI builds do not emit tool lists in stream metadata; runtime `/tools` discovery then uses a Codex JSON self-report probe fallback.
- if no explicit final event appears, adapter emits `StreamEventFinal` from aggregated deltas.

`Prompt`/`PromptRequest` are synchronous wrappers over streaming:

- aggregate deltas
- prefer final event text when present
- otherwise return trimmed aggregated delta text

## Model override behavior

- Runtime request options can include `model_override`.
- Codex adapter applies `model_override` first when present.
- If no override is provided, configured `llm.codex.model` is used.
- Runtime request options can include `reasoning_override`.
- If no reasoning override is provided, configured `llm.codex.reasoning_default` is used.

## Model catalog discovery

- Adapter supports provider model-catalog discovery for `/models`.
- Discovery path:
  - reads Codex config (`model` + `notice.model_migrations`) from resolved Codex config TOML
  - runs a constrained JSON probe prompt for provider-reported model catalog and reasoning metadata
  - merges/deduplicates probe + config results and marks partial results when probe/config data is incomplete

## MCP wiring and config

- Adapter ensures Codex MCP server wiring in TOML config before each run.
- MCP server entry defaults to:
  - name: `localclaw`
  - command: `localclaw`
  - args: `["mcp","serve"]`
- config path resolution precedence:
  1. explicit `llm.codex.mcp.config_path`
  2. `CODEX_HOME` env fallback
  3. `~/.codex/config.toml`

## Input validation and cancellation

- Empty/whitespace prompt input fails fast with `input is required`.
- Process cancellation relies on `exec.CommandContext`.

## Error behavior

- pipe setup/start failures return immediately with context.
- stdin write/close failures are surfaced as adapter errors.
- scanner/read errors are returned with context.
- non-zero exits include stderr text when available.
