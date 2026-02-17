# Cron Recurring Jobs

## Status

Draft

## Problem

`localclaw` exposes MCP cron tools (`localclaw_cron_list`, `localclaw_cron_add`, `localclaw_cron_remove`, `localclaw_cron_run`) and an in-process scheduler interface, but recurring execution is not implemented:
- `internal/cron/InProcessScheduler.Start` is a placeholder.
- Jobs are held in-memory only and are lost on process restart.
- `Run` only updates metadata and does not execute commands.

As a result, users can register cron jobs but `localclaw` cannot actually run recurring jobs.

## Scope

- Implement recurring execution in `internal/cron` for jobs added through existing MCP tools.
- Persist cron job definitions and runtime metadata under `app.root` so jobs survive restarts.
- Execute commands locally in-process using subprocesses (no new service/listener).
- Keep scheduler lifecycle tied to `App.Run(ctx)` and root context cancellation.
- Keep existing MCP tool names and argument shapes; extend responses only with additive fields if needed.
- Unify schedule validation and schedule parsing in `internal/cron` so MCP validation and runtime execution use the same rules.
- Add targeted tests and update docs (`README.md`, `docs/RUNTIME.md`, `docs/CONFIGURATION.md`, `docs/ARCHITECTURE.md`, `docs/TESTING.md`).

## Out of Scope

- New command modes (for example `localclaw cron ...`) in this phase.
- Daemonization/background service outside the running `localclaw` process.
- Distributed or multi-node scheduling.
- Remote command execution or network listeners.
- Cron syntax beyond the currently supported contract (5-field cron with `*`, `,`, `-`, `/` plus supported macros).
- Historical run log retention beyond latest run metadata per job.

## Behavior Contract

Define expected behavior in concrete terms:
- inputs:
  - `cron.enabled` gates the entire feature. When `false`, list/add/remove/run return `cron scheduler is disabled` (current behavior remains).
  - `cron add` input: optional `id`, required `schedule`, required `command`.
  - Supported schedule forms:
    - 5-field cron format: `minute hour day-of-month month day-of-week`
    - tokens: `*`, `,`, `-`, `/`, integers in current field ranges
    - macros: `@yearly`, `@annually`, `@monthly`, `@weekly`, `@daily`, `@hourly`, `@reboot`
  - Runtime context from `App.Run(ctx)` controls scheduler startup/shutdown.
- outputs:
  - `Start(ctx)` loads persisted jobs and starts a background scheduling loop.
  - Added jobs are validated, normalized, persisted immediately, and returned with timestamps.
  - `List` returns jobs sorted by `id` and includes at minimum current fields (`id`, `schedule`, `command`, `createdAt`, `updatedAt`, `lastRunAt`).
  - Scheduler executes due jobs recurring while the process is running.
  - Execution runs local commands via subprocess and records latest run outcome metadata (`lastRunAt` and status/exit details).
  - `Run(id)` triggers immediate execution through the same execution path as scheduled runs.
  - `Remove(id)` unschedules the job immediately and persists removal.
  - On restart, persisted jobs are reloaded and continue recurring execution without re-adding them.
  - Missed schedules while `localclaw` is not running are not backfilled; scheduling resumes from current time at startup.
- error paths:
  - Invalid schedule/command/id returns validation errors without mutating persisted state.
  - Duplicate IDs return `cron job "<id>" already exists`.
  - Missing IDs for run/remove return validation errors; unknown IDs return not-found behavior consistent with current contract.
  - Command execution failures (spawn error, non-zero exit, timeout, cancellation) are recorded on job metadata but must not stop the scheduler loop.
  - Persistence read/write failures return wrapped errors; startup fails fast if persisted cron state cannot be loaded.
- unchanged behavior:
  - Runtime remains single-process and local-only; no HTTP/gRPC/listener/gateway behavior is introduced.
  - MCP tool names remain:
    - `localclaw_cron_list`
    - `localclaw_cron_add`
    - `localclaw_cron_remove`
    - `localclaw_cron_run`
  - Existing manual cron tool workflows continue to work; recurring behavior is additive.

## Implementation Notes

Call out touched packages/files and key design decisions.
- `internal/cron/scheduler.go`
  - Replace placeholder `Start` with background loop, due-time calculation, and execution dispatch.
  - Ensure concurrency safety and clean shutdown on `ctx.Done()`.
- `internal/cron` (new files expected)
  - Schedule parser/validator shared by add path and MCP validation boundary.
  - Persistent store for jobs under `<app.root>/cron/jobs.json` using atomic write semantics.
  - Command executor utilities (context cancellation, exit status capture, timeout policy).
- `internal/runtime/app.go`
  - Construct scheduler with required settings (enabled flag + resolved state root/executor dependencies as needed).
  - Preserve startup order and startup failure wrapping (`cron start: ...`).
- `internal/runtime/mcp_support.go`
  - Keep runtime MCP wiring unchanged except for additive response fields if exposed by cron entries.
- `internal/mcp/tools/cron.go`
  - Reuse scheduler-owned validation logic to avoid drift.
  - Keep tool schemas/names stable; only additive structured response fields are allowed.
- `internal/config/config.go`
  - Keep `cron.enabled` as kill switch; add config fields only if strictly required by implementation.
  - If new fields are introduced, update defaults + validation + docs per repo rules.
- docs
  - Document that cron jobs execute only while a long-running `localclaw` mode is active (`tui` or `mcp serve`).
  - Document persistence location and execution semantics.

## Test Plan

- unit tests to add/update
  - `internal/cron/scheduler_test.go`
    - recurring execution fires for due jobs.
    - persistence reloads jobs across scheduler restart.
    - run/remove validation and not-found paths.
    - command failure/timeout paths update job metadata without stopping loop.
    - remove while running follows defined cancellation/unschedule semantics.
    - `@reboot` behavior executes once per scheduler start for persisted jobs.
  - `internal/mcp/tools/cron_test.go`
    - add path uses shared cron validation rules.
    - cron tool responses preserve existing fields and include additive run metadata when present.
  - `internal/runtime` tests
    - startup fails with wrapped error when cron state load fails.
    - runtime MCP cron methods continue to route to scheduler correctly.
  - `internal/config/config_test.go`
    - only needed if cron config schema changes.
- package-level focused commands for Red/Green loops
  - `go test ./internal/cron`
  - `go test ./internal/mcp/tools`
  - `go test ./internal/runtime`
  - `go test ./internal/config`
- full validation command(s)
  - `go test ./...`
  - `go run ./cmd/localclaw mcp serve` (manual smoke for long-running recurring execution)

## Acceptance Criteria

- [ ] `internal/cron.Start` runs a real background scheduler that executes due jobs repeatedly.
- [ ] Cron jobs are persisted and survive process restarts.
- [ ] Manual `cron_run` executes real commands and uses the same execution/status path as scheduled runs.
- [ ] Scheduler loop continues running after individual job failures.
- [ ] Existing MCP cron tool names and request schemas remain compatible.
- [ ] Cron validation is consistent between MCP tool boundary and scheduler internals.
- [ ] Documentation is updated to reflect runtime behavior, persistence, and operational limits.
- [ ] Full test suite passes (`go test ./...`).

## Rollback / Risk Notes

Describe fallback or rollback strategy if needed.
- Primary risks:
  - unbounded/long-running commands consuming local resources.
  - schedule parsing drift between MCP and scheduler behavior.
  - persisted store corruption causing startup failure.
- Mitigations:
  - enforce command timeout + non-overlapping execution policy per job.
  - centralize schedule validation/parsing in `internal/cron`.
  - use lock/atomic write patterns for cron state file.
- Rollback:
  - set `cron.enabled=false` to disable runtime cron execution immediately.
  - revert cron scheduler changes while keeping MCP surface stable.
