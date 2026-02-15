# Claude Code Integration

`localclaw` uses local Claude Code CLI subprocess execution as its LLM backend.

Implementation location:

- `internal/llm/claudecode/client.go`

## Execution model

- `PromptStream` executes `claude -p <input>` using `exec.CommandContext`.
- stdout is streamed as `StreamEventDelta` chunks.
- on successful completion, a `StreamEventFinal` event is emitted with full trimmed output.
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
- `AWS_REGION` and `AWS_DEFAULT_REGION` when `llm.claude_code.bedrock_region` is set.
- `LOCALCLAW_GOVCLOUD_MODE=1` when `llm.claude_code.use_govcloud=true`.

## Configuration notes

- `llm.provider` is currently constrained to `claudecode`.
- `llm.claude_code.auth_mode` is validated (`default|aws_profile|bedrock`) but is not yet translated into explicit CLI flags inside this adapter.
- `localclaw` does not implement direct network model clients.

## Error behavior

- stdout/stderr pipe setup errors are returned immediately.
- command start failures include context (`start claude code cli: ...`).
- non-zero exits return wrapped errors and include stderr text when available.
