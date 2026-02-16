# Documentation Map

This directory is the implementation-level documentation surface for `localclaw`.

## Core guides

- `ARCHITECTURE.md`: component map and runtime boundaries.
- `RUNTIME.md`: startup flow, app wiring, and command modes.
- `CONFIGURATION.md`: JSON config contract, defaults, and validation rules.
- `EMBEDDINGS.md`: deprecated embedding reference and retrieval-v2 migration note.
- `TUI.md`: terminal UX model, keybindings, and slash commands.
- `CLAUDE_CODE.md`: local Claude Code CLI integration behavior.
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
