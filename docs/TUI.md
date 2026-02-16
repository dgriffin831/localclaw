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
- provider/effective-model tuple (`provider:<provider>  model:<effective_model>`)
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
- `Ctrl+O` toggles tool-card expansion in the transcript (`collapsed` summary vs `expanded` details).

## Input and keybindings

Composer behavior:

- `Enter`: submit input
- `Ctrl+J`: insert newline
- `Tab`: autocomplete selected slash command when typing `/...`
- `Shift+Tab`: move slash menu selection backward
- `Up/Down`: slash-menu navigation when visible; with menu closed they continue prompt history traversal only after a non-empty draft (or active history selection), otherwise they pass through for transcript scrolling
- `Ctrl+P` / `Ctrl+N`: prompt history navigation
- `Alt+Up` / `Alt+Down`: history navigation aliases
- `Mouse wheel`: transcript scroll

Global controls:

- `Esc`: abort active run
- `Ctrl+T`: toggle thinking visibility
- `Ctrl+O`: toggle tool-card expansion
- `Ctrl+Y`: toggle mouse capture (turn off to allow terminal text selection)
- `Ctrl+C`: clear composer; second press within 1 second exits
- `Ctrl+D`: exit when composer is empty

History rules:

- History is only used for single-line input.
- Newline-containing draft text is not replaced by history navigation.

## Slash commands

Implemented command set:

- `/help`
- `/shortcuts`
- `/status`
- `/tools`
- `/clear`
- `/reset`
- `/new`
- `/thinking <on|off>`
- `/verbose <on|off>`
- `/mouse <on|off>`
- `/model <name>`
- `/exit`
- `/quit`

Command behavior details:

- `/shortcuts` prints all available keyboard shortcuts and their behavior.
- `/status` prints one system line containing status, provider, configured model/profile, effective model, model override state, agent, session, workspace, thinking, verbose, and mouse-capture flags.
- `/tools` prints provider plus explicit ownership sections:
  - `provider_native` for provider-discovered native tools.
  - `localclaw_mcp` for localclaw runtime tools for the active agent.
- `/verbose on` emits `[verbose]` diagnostics for prompt/session summary, runtime/tool context, stream lifecycle counters/errors, transcript writes, and detailed tool call/result metadata.
- `/verbose off` suppresses the additional `[verbose]` diagnostics.
- `/mouse off` disables mouse capture so the terminal can highlight/select text normally.
- `/mouse on` re-enables wheel/click mouse capture for TUI interactions.
- `/clear` clears transcript messages without adding a confirmation line.
- `/reset` keeps current session ID and runs runtime reset hook path when app runtime is attached.
- `/new` rotates to a new session ID through runtime and then clears transcript.
- `/model <name>` sets a session-local model override for providers that support override flags (currently `codex`).
- `/model default` or `/model off` clears the active override.
- unsupported providers (for example `claudecode`) return an explicit notice and continue with configured defaults.
- `/exit` and `/quit` abort active run and quit.

Slash-menu behavior:

- Opens when composer input is a single-token slash prefix (`/`, `/h`, etc.).
- Closes when input contains whitespace/newlines after slash command token.
- Shows up to 6 entries, with `+N more` hint when filtered results exceed limit.
- Shows keyboard shortcut text as a right-hand column for commands that have direct keybinding equivalents.

## Startup and session reset/new UX

On TUI model creation:

- Adds `localclaw ready. Type /help for commands.` system line.
- Loads and renders workspace `WELCOME.md` (if present) as markdown system content.

On `/new`:

- Aborts active run if needed.
- Invokes runtime `ResetSession` with `StartNew=true`.
- Clears transcript and shows `started new session <id>`.
- Clears any active `/model` override.
- Re-renders workspace `WELCOME.md` if present.

On `/reset`:

- Aborts active run if needed.
- Invokes runtime `ResetSession` with `StartNew=false`.
- Clears transcript and shows `session reset`.
- Clears any active `/model` override.

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
- Tool call/result activity renders as transcript tool cards with ownership labels (`provider_native` or `localclaw_mcp`).
- Collapsed cards show summary (`tool`, ownership, terminal status).
- Expanded cards include call ID, arguments, status, error (if any), and result data keys/values.
- `data.provider_result` is intentionally hidden in expanded cards.
- `data.content` is rendered in a fenced block without truncation; JSON content is pretty-printed.

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
