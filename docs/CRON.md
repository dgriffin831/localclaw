# Cron Scheduler Guide

This guide explains `localclaw` cron scheduling behavior, implementation details, and operator usage.

## What It Does

`localclaw` exposes four MCP cron tools:

- `localclaw_cron_list`
- `localclaw_cron_add`
- `localclaw_cron_remove`
- `localclaw_cron_run`

These tools manage local recurring jobs that execute shell commands on the same machine as the `localclaw` process.

## Runtime Model

- Cron runs only while `App.Run(ctx)` is active.
- In practice, recurring execution is useful in long-running modes like:
  - `go run ./cmd/localclaw tui`
  - `go run ./cmd/localclaw mcp serve`
- `doctor` and `memory` do call `App.Run`, but typically exit quickly, so they are not intended as recurring schedulers.
- No daemon/background service is started outside the `localclaw` process.

## How To Use It

### 1. Start a long-running runtime

```bash
go run ./cmd/localclaw mcp serve
```

### 2. Add a job through MCP tools

Required fields:

- `schedule`
- `command`

Optional field:

- `id` (auto-generated when omitted)

Example tool call arguments:

```json
{
  "id": "hourly-report",
  "schedule": "0 * * * *",
  "command": "echo report >> /tmp/localclaw-report.log"
}
```

### 3. List jobs

`localclaw_cron_list` returns jobs sorted by `id` and includes:

- `id`
- `schedule`
- `command`
- `createdAt`
- `updatedAt`
- `lastRunAt`
- `lastRunStatus`
- `lastRunExitCode`
- `lastRunError`
- `lastRunDurationMs`

### 4. Trigger a job manually

Use `localclaw_cron_run` with `id`.

Manual and scheduled runs share the same execution path and result contract:

- `id`
- `triggeredAt`
- `status` (`success`, `error`, `timeout`, `canceled`)
- `exitCode` (when available)
- `error` (when present)

### 5. Remove a job

Use `localclaw_cron_remove` with `id`.

- If the job is running, it is canceled before removal.
- Removal is persisted immediately.

## Schedule Syntax

Supported schedule forms:

- 5-field cron: `minute hour day-of-month month day-of-week`
- tokens: `*`, `,`, `-`, `/`, integer values in field range
- macros:
  - `@yearly`
  - `@annually`
  - `@monthly`
  - `@weekly`
  - `@daily`
  - `@hourly`
  - `@reboot`

Notes:

- `@reboot` runs once per scheduler start.
- Missed schedules while runtime is offline are not backfilled.

## Persistence

Cron jobs are persisted at:

- `<app.root>/cron/jobs.json`

Persistence characteristics:

- atomic write via temp-file + rename
- loaded during `cron.Start`
- startup fails fast if persisted cron state is invalid

## Execution Details

Commands execute locally via:

- `/bin/sh -lc <command>`

Current execution defaults:

- per-run timeout defaults to 5 minutes
- non-zero exit records `status=error` and `exitCode`
- timeout records `status=timeout`
- cancellation records `status=canceled`
- execution failures do not stop the scheduler loop

## Error Contract

Common validation and runtime errors:

- scheduler disabled: `cron scheduler is disabled`
- missing fields: `schedule is required`, `command is required`, `id is required`
- invalid schedule: field-level parser errors
- duplicate add id: `cron job "<id>" already exists`
- run/remove unknown id: `cron job "<id>" not found`

## Implementation Map

Primary files:

- `internal/cron/scheduler.go`
  - scheduler loop, job lifecycle, run/remove/add/list behavior
- `internal/cron/schedule.go`
  - shared schedule parser + validator
- `internal/cron/store.go`
  - persistent store load/save + atomic writes
- `internal/cron/executor.go`
  - shell command execution and result normalization
- `internal/mcp/tools/cron.go`
  - MCP tool schema + argument handling + runtime backend mapping
- `internal/runtime/app.go`
  - scheduler wiring and startup integration

## Quick Verification

Recommended checks:

```bash
go test ./internal/cron
go test ./internal/mcp/tools
go test ./internal/runtime
go test ./...
```
