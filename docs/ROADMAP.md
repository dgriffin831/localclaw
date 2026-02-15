# Roadmap

## Current delivery status

- Workspace + memory parity spec milestones PR-01 through PR-15 are implemented on the current line.
- Legacy migration path (`memory.path` JSON import) is in place with one-time idempotent marker.
- Memory CLI and runtime tool surfaces are integrated and tested.

## Next phase: post-parity hardening

- Add explicit changelog/release process around migrations and operational defaults.
- Improve memory index observability (durations, cause tags, structured counters).
- Add larger fixture/benchmark suites for indexing throughput and autosync behavior.

## Longer horizon

- Backup/restore workflows for state root and per-agent memory DBs.
- Expanded policy controls and auditing for enterprise deployments.
- Additional operator ergonomics in TUI and CLI around memory/session diagnostics.
