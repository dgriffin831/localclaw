# Claude Code Integration

`localclaw` uses local Claude Code CLI subprocess execution as its LLM backend.

Implementation location:

- `internal/llm/claudecode/client.go`

## Execution model

- `PromptStream` executes:
  - `claude -p <input> --output-format stream-json --verbose --mcp-config <run-scoped.json>`
  - with `--strict-mcp-config` when enabled
  - with `--allowed-tools` for localclaw MCP runtime tools derived from request tool definitions (pre-approves memory MCP tools and avoids per-call permission denials)
  - via `exec.CommandContext`.
- stdout JSONL stream is parsed into provider-agnostic events:
  - assistant text blocks -> `StreamEventDelta`
  - assistant tool-use blocks -> `StreamEventToolCall`
  - user tool-result blocks -> `StreamEventToolResult`
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

## Error behavior

- stdout/stderr pipe setup errors are returned immediately.
- command start failures include context (`start claude code cli: ...`).
- non-zero exits return wrapped errors and include stderr text when available.
- `result` events with `is_error=true` are surfaced as adapter errors (even when process exits successfully).
