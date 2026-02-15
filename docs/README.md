# Documentation Map

This directory is the implementation-level documentation surface for `localclaw`.

## Core Guides
- `ARCHITECTURE.md`: component map and runtime boundaries.
- `RUNTIME.md`: startup flow, app wiring, and command modes.
- `CONFIGURATION.md`: JSON config contract, defaults, and validation rules.
- `TUI.md`: terminal UX model, keybindings, and slash commands.
- `CLAUDE_CODE.md`: local Claude Code CLI integration behavior.
- `TESTING.md`: test locations, commands, and Red/Green workflow.

## Structured Design History
- `adr/`: architecture decisions.
- `specs/`: feature specs and implementation plans.

## Principle

Specs and tests are the primary delivery gate:
- Write or update spec for non-trivial behavior changes.
- Add/adjust failing tests before implementation where practical.
- Validate with focused tests, then full-suite tests.
