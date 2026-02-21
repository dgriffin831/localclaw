# Claude Code Integration

`localclaw` uses local Claude Code CLI subprocess execution as its LLM backend.

Implementation location:

- `internal/llm/claudecode/client.go`

## Execution model

- `PromptStream` executes:
  - `claude -p <input> --output-format stream-json --verbose --mcp-config <run-scoped.json>`
  - optional `--model <model>` when runtime selector sets model override
  - a run-scoped MCP config file is generated per request with one stdio server entry:
    - command: `localclaw`
    - args: `mcp serve`
    - file is removed after request completion
  - security-mode flags derived from runtime request metadata:
    - `full-access` -> `--dangerously-skip-permissions`
    - `sandbox-write` -> `--permission-mode acceptEdits --add-dir <resolved-workspace-path>`
    - `read-only` -> `--permission-mode dontAsk --add-dir <resolved-workspace-path>`
  - with `--strict-mcp-config` (enabled by runtime wiring)
  - with session continuation args based on config and persisted provider session state:
    - `session_mode=none`: no session args
    - persisted provider session present: resume args (default `--resume <provider-session-id>`)
    - no persisted session and `session_mode=always`: start args (default `--session-id <generated-id>`)
  - via `exec.CommandContext`.
- stdout JSONL stream is parsed into provider-agnostic events:
  - assistant text blocks -> `StreamEventDelta`
  - assistant tool-use blocks -> `StreamEventToolCall`
  - user tool-result blocks -> `StreamEventToolResult`
    - result data normalizes delegated payloads to execution output context
    - when `tool_use_result.structured_content` is present, it is promoted into canonical result data fields
    - provider raw payload is preserved under `provider_result`
  - discovered provider session IDs -> `StreamEventProviderMetadata` (`provider=claudecode`, `session_id=...`)
  - result event `result` field -> `StreamEventFinal`
- if a line is not valid JSON, adapter falls back to treating it as raw delta text.
- stderr is captured and included in surfaced execution errors.

`Prompt` is a synchronous wrapper over `PromptStream`:

- aggregates deltas
- prefers final event text when present
- otherwise returns trimmed aggregated delta text

`PromptRequest` is the synchronous request-aware wrapper over `PromptStreamRequest` and follows the same final/delta aggregation behavior.

## Input validation

- Empty/whitespace prompt input fails fast with `input is required`.

## Context cancellation

- Command uses `exec.CommandContext`, so context cancellation terminates the subprocess.
- TUI abort (`Esc`) and process shutdown rely on this behavior.

## Environment pass-through

Client appends environment values when configured:

- `AWS_PROFILE=<profile>` when `llm.claude_code.profile` is set.
- Other AWS/GovCloud settings are inherited from the parent process environment.

## Configuration notes

- `llm.provider` supports `claudecode` and `codex`.
- `localclaw` does not implement direct network model clients.
- session continuation config:
  - `llm.claude_code.extra_args` for provider flags (defaults to a LocalClaw MCP `--allowed-tools` list so first-run memory/workspace/session/cron/channel calls do not prompt for permission)
    - security-managed flags (`--dangerously-skip-permissions`, `--permission-mode`, `--add-dir`) are rejected in config; use `security.mode` instead.
    - `extra_args` are appended after runtime-managed args; avoid duplicate flags unless you intentionally want CLI-level override behavior.
  - `llm.claude_code.session_mode` (`always | existing | none`)
  - `llm.claude_code.session_arg` for start mode (default `--session-id`; used only when `session_mode=always` and no persisted provider session exists)
  - `llm.claude_code.resume_args` with `{sessionId}` placeholder (placeholder required when `session_mode=existing` and custom resume args are set)
  - `llm.claude_code.session_id_fields` for parsing provider output fields

## Model catalog discovery

- Adapter supports provider model-catalog discovery for `/models`.
- Discovery uses a constrained JSON probe prompt and falls back to configured `llm.claude_code.profile` when probe data is unavailable.
- Catalog responses are marked partial when probe data is unavailable or incomplete.
- Claude Code models are currently reported with reasoning unsupported.

## Session recovery caveat

- Runtime persists provider session IDs per localclaw session.
- On resume-related provider errors (for example "no conversation found"), runtime clears the persisted Claude session ID and retries once with a fresh session.

## Error behavior

- stdout/stderr pipe setup errors are returned immediately.
- command start failures include context (`start claude code cli: ...`).
- non-zero exits return wrapped errors and include stderr text when available.
- `result` events with `is_error=true` are surfaced as adapter errors (even when process exits successfully).
