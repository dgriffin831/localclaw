# Backup Lifecycle and Manual Snapshots

## Status

Draft

## V1 Build Policy

`localclaw` is still pre-v1. Favor clean implementations only:
- no rollback plans
- no fallback execution paths unless explicitly requested
- no legacy compatibility shims

## Problem

`localclaw` currently has no built-in backup workflow for core local state.
Operators can lose `localclaw.json`, cron state, or workspace data if local files are deleted or corrupted.
There is also no manual CLI command for creating a point-in-time backup, and no retention policy to prune old backup archives.

## Scope

- Add a top-level `backup` config block with lifecycle controls:
  - `backup.auto_save`
  - `backup.auto_clean`
  - `backup.interval`
  - `backup.retain_count`
- Set default backup config values:
  - `backup.auto_save = true`
  - `backup.auto_clean = true`
  - `backup.interval = "1d"`
  - `backup.retain_count = 3`
- Add a `localclaw backup` command that manually creates one compressed backup archive.
- Store backup archives under `<app.root>/backups` (default path resolves to `~/.localclaw/backups`).
- Include these data sets in each backup:
  - `<app.root>/localclaw.json`
  - `<app.root>/cron/` state files
  - resolved workspace directories (default agent and configured agents)
- Run auto-save and auto-clean jobs in background goroutines only for long-running modes:
  - `localclaw tui`
  - `localclaw channels serve` (excluding `--once`)
  - `localclaw mcp serve`
- Apply retention cleanup by count using `backup.retain_count`.

## Out of Scope

- Restore command or restore workflow.
- Remote/cloud backup targets.
- Encryption/password-protected backups.
- Configurable backup destination path in this change.
- Incremental/differential backups.

## Behavior Contract

Define expected behavior in concrete terms:

- inputs
  - `backup.auto_save` toggles periodic backup creation in long-running modes.
  - `backup.auto_clean` toggles periodic retention cleanup in long-running modes.
  - `backup.interval` is a positive duration string (for example: `30m`, `12h`, `1d`).
  - `backup.retain_count` is a positive integer count of archives to keep.
  - `localclaw backup` command invocation with no required subcommand.

- outputs
  - `localclaw backup` creates `<app.root>/backups` when missing.
  - Manual backups are compressed archives using `tar.gz`.
  - Archive filenames include date/time stamp for uniqueness and ordering:
    - `localclaw-backup-YYYYMMDD-HHMMSSZ.tar.gz`
  - Each archive contains normalized paths for:
    - `localclaw.json`
    - `cron/`
    - `workspace/` and `workspace-<agentId>/` when present
  - In long-running modes, auto-save and auto-clean run in goroutines and stop when root context is canceled.
  - Retention cleanup keeps the newest `backup.retain_count` matching backup archives and removes older ones.
  - `localclaw backup` prints a success line with created archive path.

- error paths
  - Config validation fails startup when:
    - `backup.interval` is blank/invalid/non-positive.
    - `backup.retain_count <= 0`.
  - Manual `localclaw backup` returns non-zero on backup creation failure.
  - Background auto-save/auto-clean failures are logged and do not terminate active long-running command modes.
  - Backup/cleanup runs must not overlap; if a prior run is still active, the next tick is skipped.
  - Missing optional source directories (for example absent `cron/`) do not panic; backup continues with available sources.

- unchanged behavior
  - Runtime remains local-only, single-process CLI.
  - No HTTP/gateway/listener behavior is introduced.
  - Existing startup initialization ordering in `App.Run` remains intact.

## Implementation Notes

- `internal/config/config.go`
  - add `BackupConfig` to `Config`.
  - add defaults and validation for `auto_save`, `auto_clean`, `interval`, `retain_count`.
- `internal/backup`
  - add backup manager for archive creation, interval scheduling helpers, and retention cleanup.
  - provide serialization/locking to avoid overlapping backup and clean runs.
- `cmd/localclaw/main.go`
  - wire `backup` as a known top-level command.
  - update root help text and command dispatch.
- `internal/cli`
  - add backup command handler for manual one-shot backup execution.
  - keep argument contract simple (`localclaw backup` with no positional args).
- runtime/command wiring
  - start background backup goroutines only from long-running command paths (`tui`, `channels serve`, `mcp serve`), not from short-lived modes.
  - skip starting backup loops for `channels serve --once`.
- docs updates expected
  - `README.md` (new command surface)
  - `docs/CONFIGURATION.md` (new `backup` schema/defaults/validation)
  - `docs/RUNTIME.md` (background backup lifecycle)
  - `docs/TESTING.md` (new test targets)

## Test Plan

- unit tests to add/update
  - `internal/config/config_test.go`
    - default values for `backup.*`.
    - validation failures for invalid `backup.interval`.
    - validation failures for non-positive `backup.retain_count`.
  - `internal/backup/*_test.go`
    - creates `tar.gz` archive with required files/directories.
    - filename timestamp/date format.
    - retention keeps newest N and removes older archives.
    - overlapping interval runs are skipped.
    - missing optional source directories do not fail backup.
  - `cmd/localclaw/main_test.go`
    - recognizes `backup` as known command.
    - help text includes `backup`.
  - `internal/cli/*_test.go`
    - `localclaw backup` manual command success path.
    - positional argument rejection for `backup`.
    - long-running mode wiring starts backup goroutines only for intended modes.

- package-level focused commands for Red/Green loops
  - `go test ./internal/config -run TestValidate`
  - `go test ./internal/backup`
  - `go test ./internal/cli`
  - `go test ./cmd/localclaw`

- full validation command(s)
  - `go test ./...`

## Acceptance Criteria

- [ ] `localclaw backup` creates a compressed, date-stamped backup archive.
- [ ] Backup archives include `localclaw.json`, cron state, and workspace data.
- [ ] Backup archives are written under `<app.root>/backups` (default `~/.localclaw/backups`).
- [ ] `backup.auto_save`, `backup.auto_clean`, `backup.interval`, and `backup.retain_count` exist with requested defaults.
- [ ] Invalid backup config values fail fast during config validation.
- [ ] Auto-save and auto-clean run as goroutines only in long-running modes (`tui`, `channels serve`, `mcp serve`), with `channels serve --once` excluded.
- [ ] Retention cleanup enforces `backup.retain_count` by removing oldest archives.
- [ ] Background backup failures are non-fatal to long-running runtime modes.
