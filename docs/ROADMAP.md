# Roadmap

## Current delivery status

- Workspace/session/memory parity milestones are implemented in current runtime.
- Memory CLI (`status`, `index`, `search`) is integrated.
- Session reset/new snapshot hooks and memory flush plumbing are in place.
- Startup enforces local-only boundaries and scaffolds default config file.

## Next phase: hardening and observability

- Improve memory index observability (durations, trigger cause tags, structured counters).
- Expand autosync and large-workspace benchmark coverage.
- Tighten operator diagnostics for memory/session state in CLI and TUI.

## Longer horizon

- Backup/restore workflows for state root and per-agent memory DBs.
- Expanded policy controls and auditability for enterprise deployments.
- Provider abstraction work for additional local subprocess-backed LLM adapters.
