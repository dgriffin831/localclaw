# MVP Spec

## Functional requirements

- CLI executable (`localclaw`) starts and validates config.
- Local-only policy guard blocks any server/listener feature flags.
- Core modules present with stable interfaces:
  - memory
  - workspace
  - skills
  - cron
  - heartbeat
  - channels (slack, signal)
  - llm (claudecode)
- Claude Code CLI adapter executes a local command and returns output.

## Non-functional requirements

- Go only.
- Single-process.
- No network-exposed surfaces.
- Default secure config.

## Acceptance checks

- `go test ./...` passes.
- `go run ./cmd/localclaw` runs and exits cleanly.
- Invalid network-listen config fails fast at startup.
