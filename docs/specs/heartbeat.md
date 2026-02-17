# Heartbeat Interval Prompt Runner

## Status

Draft

## Problem

`localclaw` currently wires heartbeat as a startup-only no-op (`heartbeat.Ping("localclaw startup heartbeat")`), and does not execute any periodic heartbeat workflow.
Operators already get a workspace `HEARTBEAT.md` bootstrap file, but runtime never uses it for scheduled maintenance prompts.
This leaves the heartbeat feature incomplete versus the intended "continuous periodic check" behavior.

## Scope

- Implement heartbeat as an interval-driven background loop while runtime is active.
- Use existing heartbeat config (`heartbeat.enabled`, `heartbeat.interval_seconds`) as the scheduling contract.
- On each interval, run a local LLM prompt that explicitly references workspace `HEARTBEAT.md`.
- Keep execution local-only and in-process, using existing runtime/provider plumbing.
- Keep heartbeat non-fatal: failures do not stop runtime startup or ongoing command-mode execution.

## Out of Scope

- Adding HTTP/gateway/listener behavior.
- Adding new scheduler subsystems or replacing cron with heartbeat.
- Adding new heartbeat config fields in this change.
- Multi-agent fan-out heartbeat runs across every configured agent workspace.
- Rich reporting UI for heartbeat outputs.

## Behavior Contract

Define expected behavior in concrete terms:

- inputs
  - `heartbeat.enabled` boolean.
  - `heartbeat.interval_seconds` positive integer.
  - Resolved default agent workspace path.
  - Workspace file `HEARTBEAT.md` (bootstrap-managed file).
- outputs
  - If heartbeat is enabled, runtime starts a background ticker loop after startup initialization completes.
  - On each interval tick, runtime submits a heartbeat prompt for the default agent/session context.
  - The heartbeat prompt includes a direct instruction to use `HEARTBEAT.md` from the resolved workspace as the source of checks/tasks.
  - Heartbeat loop stops when runtime context is canceled.
- error paths
  - If heartbeat is disabled, no loop is started.
  - If `HEARTBEAT.md` is missing/unreadable at run time, that tick is skipped and logged; next ticks continue.
  - If the provider prompt call fails, error is logged and future ticks continue.
  - If a prior heartbeat run is still active when a new tick arrives, the new tick is skipped (no overlapping runs).
- unchanged behavior
  - Runtime remains single-process, local-only, and subprocess-based for model execution.
  - Existing command modes (`doctor`, `tui`, `memory`, `mcp`) remain unchanged.
  - Existing config validation (`heartbeat.interval_seconds > 0` when enabled) remains in force.

## Implementation Notes

- `internal/heartbeat/monitor.go`
  - Evolve monitor from startup stub into interval-driven monitor behavior.
  - Add loop lifecycle management bound to runtime context cancellation.
  - Keep overlap guard in monitor or runtime wiring to avoid concurrent heartbeat prompt executions.
- `internal/runtime/app.go`
  - Keep startup ping behavior but wire heartbeat periodic execution from `App.Run`.
  - Inject heartbeat runner callback that invokes runtime prompt flow and references workspace `HEARTBEAT.md`.
  - Ensure startup remains non-blocking: heartbeat loop starts in background and does not stall command-mode entry.
- `docs/RUNTIME.md`
  - Update startup/runtime lifecycle notes to include heartbeat periodic loop semantics.
- `docs/CONFIGURATION.md`
  - Clarify that `heartbeat.interval_seconds` controls recurring heartbeat prompt cadence.

## Test Plan

- unit tests to add/update
  - `internal/heartbeat`
    - starts loop only when enabled.
    - ticks trigger callback at configured cadence.
    - cancellation stops loop.
    - overlapping tick is skipped while prior run is in progress.
  - `internal/runtime`
    - `App.Run` starts heartbeat periodic runner.
    - heartbeat callback builds prompt that references `HEARTBEAT.md`.
    - heartbeat failures are non-fatal to runtime startup and subsequent ticks.
- package-level focused commands for Red/Green loops
  - `go test ./internal/heartbeat`
  - `go test ./internal/runtime`
- full validation command(s)
  - `go test ./...`

## Acceptance Criteria

- [ ] With `heartbeat.enabled=true`, runtime starts a recurring heartbeat loop using `heartbeat.interval_seconds`.
- [ ] Each heartbeat run submits a prompt that references workspace `HEARTBEAT.md`.
- [ ] Heartbeat failures (missing file/provider error) do not terminate runtime; later ticks still execute.
- [ ] Overlapping heartbeat executions are prevented.
- [ ] Context cancellation cleanly stops heartbeat activity.
- [ ] Runtime/local-only architecture constraints remain unchanged.

## Rollback / Risk Notes

- Primary risk: periodic prompts may introduce background load or noisy failures if misconfigured.
- Mitigations:
  - disable quickly via `heartbeat.enabled=false`.
  - keep per-tick failures isolated and non-fatal.
  - skip overlapping ticks rather than queueing unbounded work.
- Rollback path: revert to current startup-only heartbeat ping behavior.
