# TUI Implementation Guide

This document describes the current terminal UI behavior implemented in `internal/tui/app.go`.

## Runtime Model

`localclaw tui` runs a Bubble Tea program with:
- header
- transcript viewport
- status line
- footer
- bordered multiline input composer

The TUI depends on runtime streaming (`app.PromptStream`) for incremental assistant output.

## Status Lifecycle

The UI status state machine uses:
- `idle`
- `sending`
- `waiting`
- `streaming`
- `aborted`
- `error`

Behavior notes:
- elapsed timer is shown while status is busy (`sending/waiting/streaming`).
- if waiting and no delta has arrived, status text shows `thinking` when thinking is enabled.

## Input and Keybindings

Composer behavior:
- `Enter`: submit input
- `Alt+Enter`: insert newline
- `Up/Down`: prompt history navigation (single-line entries)

Global controls:
- `Esc`: abort active run
- `Ctrl+T`: toggle thinking visibility
- `Ctrl+O`: toggle tool-card expansion flag
- `Ctrl+C`: clear composer; press twice quickly to exit
- `Ctrl+D`: exit when composer is empty

## Slash Commands

Implemented slash command set:
- `/help`
- `/status`
- `/clear`
- `/reset`
- `/new`
- `/thinking <on|off>`
- `/verbose <on|off>`
- `/model <name>` (reports not implemented override)
- `/exit` and `/quit`

## Rendering

- User and assistant messages are rendered as markdown blocks using Glamour.
- System notices are rendered as muted text.
- Assistant messages include `(streaming...)` marker while stream is active.
- Viewport remains bottom-anchored when user is at bottom or when forced.

## Testing Surface

Current unit coverage in `internal/tui/app_test.go`:
- slash command parsing (`parseSlash`)
- elapsed formatting (`formatElapsed`)
- input model accepts typing updates

When changing keybindings, slash commands, or status behavior:
1. update `internal/tui/app_test.go`
2. update `README.md`
3. update this document
