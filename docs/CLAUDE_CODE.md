# Claude Code Integration

`localclaw` uses local Claude Code CLI as its LLM execution backend.

Implementation location:
- `internal/llm/claudecode/client.go`

## Execution Model

- `PromptStream` starts `claude -p <input>` via subprocess.
- stdout is read incrementally and emitted as stream delta events.
- accumulated output is emitted as a final event on successful completion.
- stderr is captured and included in surfaced execution errors.

`Prompt` is a synchronous wrapper that consumes stream events and returns final text.

## Input Validation

- Empty/whitespace prompts fail fast with `input is required`.

## Context Cancellation

- subprocess uses `exec.CommandContext`.
- cancelling context terminates the underlying process.
- TUI abort and process shutdown rely on this behavior.

## Environment Pass-through

The client may append environment variables when configured:
- `AWS_PROFILE=<profile>` when profile is set.
- `AWS_REGION` and `AWS_DEFAULT_REGION` when bedrock region is set.
- `LOCALCLAW_GOVCLOUD_MODE=1` when `use_govcloud` is enabled.

## Current Constraints

- Provider is fixed to `claudecode` in config validation.
- `localclaw` does not implement direct network model clients.
- GovCloud behavior is pass-through configuration, not custom transport code.
