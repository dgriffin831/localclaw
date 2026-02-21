# Codex Integration

`localclaw` supports Codex as a local CLI subprocess provider.

Implementation location:

- `internal/llm/codex/client.go`

## Execution model

- `PromptStreamRequest` executes:
  - non-resume baseline: `codex exec --json -C <agents.defaults.workspace> ... -`
    - `-C` uses configured `agents.defaults.workspace` (runtime setting), not the resolved per-agent workspace path
  - resume path: `codex exec <resume_args...> ... -` with `--json` controlled by `llm.codex.resume_output`
    - `json` / `jsonl`: keep `--json`
    - `text`: omit `--json`
  - optional `-p <profile>` from `llm.codex.profile` (non-resume path)
  - optional `-m <model>` from `llm.codex.model` or runtime override
  - `-c model_reasoning_effort="<level>"` from runtime/default selector reasoning (`reasoning_override` or `llm.codex.reasoning_default`)
  - security-mode flags derived from runtime request metadata:
    - `full-access` -> `--dangerously-bypass-approvals-and-sandbox`
    - `sandbox-write` -> `--sandbox workspace-write --add-dir <resolved-workspace-path>`
    - `read-only` -> `--sandbox read-only --add-dir <resolved-workspace-path>`
  - for resume runs, security-managed flags are emitted before the `resume` subcommand (`codex exec <security flags> resume ...`) because `codex exec resume ...` does not accept those flags after the subcommand token
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
  - provider session ID metadata is emitted from any parsed event carrying a configured session-id field (commonly `thread_id` on events such as `thread.started`)
- Note: some Codex CLI builds do not emit tool lists in stream metadata; runtime `/tools` discovery then uses a Codex JSON self-report probe fallback.
- Note: when a stdout line is not parseable JSON, the adapter streams that line as raw `StreamEventDelta` text (compatibility fallback).
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
- sandboxed/read-only security modes require a resolved workspace path; request execution fails when missing.
- Process cancellation relies on `exec.CommandContext`.

## Error behavior

- pipe setup/start failures return immediately with context.
- stdin write/close failures are surfaced as adapter errors.
- scanner/read errors are returned with context.
- non-zero exits include stderr text when available.
- unsupported/missing security context returns a fast-fail request error.
- runtime session continuation may clear an invalid persisted provider session ID and retry once without resume when the error indicates an invalid/expired/missing session.

## Security config interaction

- `security.mode` is authoritative for Codex sandbox/permission posture.
- `llm.codex.extra_args` cannot include security-managed flags (`--dangerously-bypass-approvals-and-sandbox`, `--yolo`, `--sandbox`, `--add-dir`).
