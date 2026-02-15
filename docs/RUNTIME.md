# Runtime Implementation Guide

This guide documents how `localclaw` boots and how runtime behavior is structured.

## Entrypoint and Modes

Entrypoint: `cmd/localclaw/main.go`

Supported command modes:
- `check` (default): runs startup initialization checks.
- `tui`: runs startup checks, then starts interactive Bubble Tea UI.

Examples:
```bash
go run ./cmd/localclaw
go run ./cmd/localclaw check
go run ./cmd/localclaw tui
go run ./cmd/localclaw -config ./localclaw.json tui
```

## Runtime Construction

`runtime.New(cfg)` performs two tasks:
1. Re-validates config (`cfg.Validate()`).
2. Builds an `App` struct containing all in-process modules.

The `App` object is the composition root for:
- local state boundaries (`memory`, `workspace`, `skills`)
- local scheduling/liveness (`cron`, `heartbeat`)
- channel adapters (`slack`, `signal`)
- local LLM adapter (`claudecode`)

## Startup Lifecycle

`App.Run(ctx)` currently executes:
1. `workspace.Init`
2. `memory.Init`
3. `skills.Load`
4. `cron.Start`
5. `heartbeat.Ping("localclaw startup heartbeat")`

Any step failure aborts startup with a wrapped error.

## Prompt APIs

Runtime exposes two prompt paths:
- `Prompt(ctx, input) (string, error)`
  - synchronous convenience wrapper around stream path.
- `PromptStream(ctx, input) (<-chan StreamEvent, <-chan error)`
  - incremental output for TUI streaming UX.

## Error and Cancellation Model

- OS signals (`SIGINT`, `SIGTERM`) cancel the root context.
- TUI run abort (`Esc`) cancels active prompt context.
- Claude CLI invocation uses `exec.CommandContext`, so cancellation terminates subprocess execution.
- Startup and prompt errors are surfaced with context-rich wrapping.

## Extension Rules

When extending runtime behavior:
- keep composition in `runtime.New`.
- keep startup ordering explicit and deterministic.
- add tests for boundary and policy regressions.
- do not add listener/server/gateway startup paths.
