# Claude Code Integration

`localclaw` uses local Claude Code CLI subprocess execution as its LLM backend.

Implementation location:

- `internal/llm/claudecode/client.go`

## Execution model

- `PromptStream` executes:
  - `claude -p <input> --output-format stream-json --verbose --mcp-config <run-scoped.json>`
  - optional `--model <model>` when runtime selector sets model override
  - with `--strict-mcp-config` when enabled
  - with session continuation args based on config and persisted provider session state:
    - start mode default: `--session-id <generated-id>`
    - resume mode default: `--resume <provider-session-id>`
  - via `exec.CommandContext`.
- stdout JSONL stream is parsed into provider-agnostic events:
  - assistant text blocks -> `StreamEventDelta`
  - assistant tool-use blocks -> `StreamEventToolCall`
  - user tool-result blocks -> `StreamEventToolResult`
    - result data normalizes delegated payloads to execution output context
    - when `tool_use_result.structured_content` is present, it is promoted into canonical result data fields
    - provider raw payload remains available only in hidden/internal `provider_result`
  - discovered provider session IDs -> `StreamEventProviderMetadata` (`provider=claudecode`, `session_id=...`)
  - result event `result` field -> `StreamEventFinal`
- if a line is not valid JSON, adapter falls back to treating it as raw delta text.
- stderr is captured and included in surfaced execution errors.

`Prompt` is a synchronous wrapper over `PromptStream`:

- aggregates deltas
- prefers final event text when present
- otherwise returns trimmed aggregated delta text

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
  - `llm.claude_code.session_mode` (`always | existing | none`)
  - `llm.claude_code.session_arg` for start mode (default `--session-id`)
  - `llm.claude_code.resume_args` with `{sessionId}` placeholder
  - `llm.claude_code.session_id_fields` for parsing provider output fields

## Model catalog discovery

- Adapter supports provider model-catalog discovery for `/models`.
- Discovery uses a constrained JSON probe prompt and falls back to configured `llm.claude_code.profile` when probe data is unavailable.
- Claude Code models are currently reported with `reasoning: n/a` unless provider metadata adds explicit reasoning support.

## Error behavior

- stdout/stderr pipe setup errors are returned immediately.
- command start failures include context (`start claude code cli: ...`).
- non-zero exits return wrapped errors and include stderr text when available.
- `result` events with `is_error=true` are surfaced as adapter errors (even when process exits successfully).
