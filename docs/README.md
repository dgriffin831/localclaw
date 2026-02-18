# Documentation Map

This directory is the implementation-level documentation surface for `localclaw`.

## Core guides

- `ARCHITECTURE.md`: component map and runtime boundaries.
- `RUNTIME.md`: startup flow, app wiring, and command modes.
- `CONFIGURATION.md`: JSON config contract, defaults, and validation rules.
- `INSTALL.md`: cross-platform source install and `PATH` setup instructions.
- `CRON.md`: recurring scheduler behavior, implementation, and usage.
- `HEARTBEATS.md`: heartbeat scheduling, prompt behavior, and `HEARTBEAT.md` usage.
- `MEMORY.md`: retrieval-v2 search/grep model and implementation behavior.
- `TOOLS.md`: MCP/provider tools architecture, execution model, and implementation details.
- `SESSIONS.md`: session identity, continuation, storage, and lifecycle behavior.
- `TUI.md`: terminal UX model, keybindings, and slash commands.
- `SLACK.md`: Slack outbound channel setup and implementation details.
- `SIGNAL.md`: Signal (`signal-cli`) outbound + inbound channel setup and implementation details.
- `CLAUDE_CODE.md`: local Claude Code CLI integration behavior.
- `CODEX_CLI.md`: local Codex CLI integration behavior.
- `TESTING.md`: test locations, commands, and Red/Green workflow.
- `SECURITY.md`: local-only boundary and security controls.

## Specs and design history

- `specs/`: feature specs, implementation plans, and handoff notes.
- `specs/template.md`: baseline spec template.

## Principle

Specs and tests are delivery gates:

- Write or update specs for non-trivial behavior changes.
- Add or adjust tests with Red/Green loops before broad refactors.
- Validate with focused package tests, then `go test ./...`.
