# TUI Implementation Guide

This document describes current terminal UI behavior in `internal/tui/app.go`.

## Runtime model

`localclaw tui` runs a Bubble Tea full-screen program (`tea.WithAltScreen`, `tea.WithMouseCellMotion`) with:

- header line
- transcript viewport
- status line
- bordered multiline composer with slash-command menu and keybinding hint line

Streaming output comes from `app.PromptStreamForSession`.

## Header and status

Header currently shows:

- app label (`# localclaw`)
- provider/profile tuple (`model:<provider>/<claude_profile>`)
- resolved workspace path

Status state machine values:

- `idle`
- `sending`
- `waiting`
- `streaming`
- `aborted`
- `error`

Behavior notes:

- Busy statuses show spinner + elapsed time.
- While waiting and no stream delta has arrived, status text shows active thinking message when thinking visibility is on.
- Thinking messages come from `app.thinking_messages`; fallback is `thinking`.
- `Ctrl+O` toggles an internal `toolsExpanded` flag and status text only (tool cards are not currently rendered as a separate transcript type).

## Input and keybindings

Composer behavior:

- `Enter`: submit input
- `Ctrl+J`: insert newline
- `Tab`: autocomplete selected slash command when typing `/...`
- `Shift+Tab`: move slash menu selection backward
- `Up/Down`: slash-menu navigation when visible
- `Ctrl+P` / `Ctrl+N`: prompt history navigation
- `Alt+Up` / `Alt+Down`: history navigation aliases
- `Mouse wheel`: transcript scroll

Global controls:

- `Esc`: abort active run
- `Ctrl+T`: toggle thinking visibility
- `Ctrl+O`: toggle tool expansion flag
- `Ctrl+C`: clear composer; second press within 1 second exits
- `Ctrl+D`: exit when composer is empty

History rules:

- History is only used for single-line input.
- Newline-containing draft text is not replaced by history navigation.

## Slash commands

Implemented command set:

- `/help`
- `/status`
- `/tools`
- `/clear`
- `/reset`
- `/new`
- `/thinking <on|off>`
- `/verbose <on|off>`
- `/model <name>`
- `/exit`
- `/quit`

Command behavior details:

- `/status` prints one system line containing status, provider, agent, session, workspace, thinking, and verbose flags.
- `/tools` prints provider plus explicit ownership sections:
  - `provider_native` for provider-discovered native tools.
  - `localclaw_mcp` for localclaw runtime tools for the active agent.
- `/verbose on` emits `[verbose]` diagnostics for prompt/session summary, runtime/tool context, stream lifecycle counters/errors, transcript writes, and detailed tool call/result metadata.
- `/verbose off` suppresses the additional `[verbose]` diagnostics.
- `/clear` clears transcript messages without adding a confirmation line.
- `/reset` keeps current session ID and runs runtime reset hook path when app runtime is attached.
- `/new` rotates to a new session ID through runtime and then clears transcript.
- `/model <name>` currently reports "not implemented" and does not change provider/model runtime behavior.
- `/exit` and `/quit` abort active run and quit.

Slash-menu behavior:

- Opens when composer input is a single-token slash prefix (`/`, `/h`, etc.).
- Closes when input contains whitespace/newlines after slash command token.
- Shows up to 6 entries, with `+N more` hint when filtered results exceed limit.

## Startup and session reset/new UX

On TUI model creation:

- Adds `localclaw ready. Type /help for commands.` system line.
- Loads and renders workspace `WELCOME.md` (if present) as markdown system content.

On `/new`:

- Aborts active run if needed.
- Invokes runtime `ResetSession` with `StartNew=true`.
- Clears transcript and shows `started new session <id>`.
- Re-renders workspace `WELCOME.md` if present.

On `/reset`:

- Aborts active run if needed.
- Invokes runtime `ResetSession` with `StartNew=false`.
- Clears transcript and shows `session reset`.

## Run lifecycle

When submitting non-slash input:

1. Add user message to transcript.
2. Update coarse session token accounting (`EstimateTokensFromText`).
3. Append user message to session transcript file.
4. Trigger asynchronous memory flush check.
5. Start stream run context and wait for stream events/errors.

Completion behavior:

- Delta chunks append to active assistant message.
- Final payload replaces assistant text when non-empty.
- If final and delta are both empty, assistant message becomes `(no output)`.
- Assistant final text is appended to transcript file and token accounting.
- Tool call/result system messages include ownership source labels (`provider_native` or `localclaw_mcp`).

## Rendering

- User, assistant, and markdown-enabled system messages render through Glamour.
- `WELCOME.md` and markdown user/assistant content are rendered, not shown as raw markdown.
- Viewport remains bottom-anchored when user is at bottom or when forced by update.

## Testing surface

Current tests in `internal/tui/app_test.go` cover:

- slash parsing and autocomplete behavior
- `/help`, `/tools`, `/new`, `/reset` command effects
- welcome message startup/new rendering
- status/thinking message behavior
- history navigation keybindings
- header workspace path resolution
- layout overflow safeguards
- Bubble Tea program startup options

When changing keybindings, slash commands, or status behavior:

1. update `internal/tui/app_test.go`
2. update `README.md`
3. update this document
