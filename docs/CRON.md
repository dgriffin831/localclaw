# Cron Scheduler Guide

This guide explains `localclaw` cron scheduling behavior, implementation details, and operator usage.

## What It Does

`localclaw` exposes four MCP cron tools:

- `localclaw_cron_list`
- `localclaw_cron_add`
- `localclaw_cron_remove`
- `localclaw_cron_run`

These tools manage local recurring jobs that run agent prompts (not shell commands) inside the same `localclaw` process.

## Runtime Model

- Cron runs only while `App.Run(ctx)` is active.
- In practice, recurring execution is useful in long-running modes like:
  - `go run ./cmd/localclaw tui`
  - `go run ./cmd/localclaw mcp serve`
  - `go run ./cmd/localclaw channels serve`
- `doctor` and `memory` do call `App.Run`, but typically exit quickly, so they are not intended as recurring schedulers.
- No daemon/background service is started outside the `localclaw` process.

## Job Model

Cron jobs are intentionally simple:

- `schedule` (required)
- `message` (required)
- `timeout_seconds` (optional)
- `session_target` (optional; defaults to `isolated`)

Session targets:

- `default`: sends prompt to the default session (`default/default`)
- `isolated`: sends prompt to per-job isolated session (`default/cron-<job-id>` unless `agent_id` overrides agent)

## How To Use It

### 1. Start a long-running runtime

```bash
go run ./cmd/localclaw mcp serve
```

### 2. Add a job through MCP tools

Required fields:

- `schedule`
- `message`

Optional fields:

- `id`
- `agent_id`
- `session_target` (defaults to `isolated`)
- `wake_mode` (defaults to `next-heartbeat`; currently normalized/persisted only)
- `timeout_seconds`

Example: isolated job (default behavior)

```json
{
  "id": "hourly-review",
  "schedule": "0 * * * *",
  "message": "Run an hourly workspace review and report blockers.",
  "timeout_seconds": 60
}
```

Example: default-session job

```json
{
  "id": "default-session-reminder",
  "schedule": "*/5 * * * *",
  "session_target": "default",
  "message": "Check for urgent follow-ups and reply if needed."
}
```

### 3. List jobs

`localclaw_cron_list` returns jobs sorted by `id` and includes:

- `id`
- `agentId`
- `schedule`
- `sessionTarget`
- `wakeMode`
- `message`
- `timeoutSeconds`
- `createdAt`
- `updatedAt`
- `lastRunAt`
- `lastRunStatus`
- `lastRunError`
- `lastRunDurationMs`

### 4. Trigger a job manually

Use `localclaw_cron_run` with `id`.

Manual and scheduled runs share the same execution path and result contract:

- `id`
- `triggeredAt`
- `status` (`success`, `error`, `timeout`, `canceled`, `skipped`)
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

- Cron execution is prompt-based through runtime agent APIs.
- `session_target=default` runs in `default/default` (or `<agent_id>/default`).
- `session_target=isolated` runs in `cron-<job-id>` session for the resolved agent.
- `wake_mode` is currently metadata-only (`next-heartbeat` / `now`) and does not change run scheduling behavior.
- default per-run timeout is 5 minutes.
- `timeout_seconds` overrides per-job timeout when set.
- one job failing does not stop scheduler progress for other jobs.

## Error Contract

Common validation and runtime errors:

- scheduler disabled: `cron scheduler is disabled`
- missing fields: `schedule is required`, `message is required`, `id is required`
- invalid schedule: field-level parser errors
- invalid target: `sessionTarget must be one of: default, isolated`
- duplicate add id: `cron job "<id>" already exists`
- run/remove unknown id: `cron job "<id>" not found`

## Implementation Map

Primary files:

- `internal/cron/scheduler.go`
  - scheduler loop, validation, job lifecycle, run/remove/add/list behavior
- `internal/cron/schedule.go`
  - shared schedule parser + validator
- `internal/cron/store.go`
  - persistent store load/save + atomic writes
- `internal/runtime/cron_runtime.go`
  - runtime cron executor mapping entries to prompt calls
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
