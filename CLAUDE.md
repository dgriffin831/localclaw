# Claude Code Integration Notes

## Primary execution path

`localclaw` invokes local Claude Code CLI as the LLM backend.

## Expected contract (MVP)

- Configured binary path (`llm.claude_code.binary_path`).
- Prompt/response execution over local process invocation.
- Timeout and stderr capture handled in-process.

## GovCloud Bedrock compatibility

For AWS GovCloud use cases, configure Claude Code CLI with GovCloud-compatible profile/auth settings and model targets. `localclaw` passes through that configuration context rather than implementing custom Bedrock transport.

## Non-goals

- No alternate LLM provider in MVP.
- No direct network model client implementation in `localclaw`.
