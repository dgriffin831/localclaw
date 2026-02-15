# TUI UX Parity Spec (Openclaw-Inspired)

## Status

Draft v1.0

## Goal

Design and implement a professional, colorful, highly usable `localclaw tui` experience that is functionally comparable to `openclaw tui`, while preserving localclaw constraints:

- Go only
- local-first, local-only
- single-process
- no gateway/web server dependency

## Current gap

Today, `localclaw tui` is a basic line REPL with no streaming render, no markdown formatting, no multiline editor UX, and no structured run/tool/thinking status presentation.

## Openclaw behaviors to mirror

The following behaviors were reviewed directly from `~/dev/github/openclaw` implementation:

- Layout model: header + chat log + status line + footer + editor
  - `src/tui/tui.ts`
  - `docs/tui.md`
- Markdown-rendered chat messages (assistant + user) and markdown-rendered tool output
  - `src/tui/components/assistant-message.ts`
  - `src/tui/components/user-message.ts`
  - `src/tui/components/tool-execution.ts`
- Distinct theme palette with syntax-highlighted code blocks
  - `src/tui/theme/theme.ts`
  - `src/tui/theme/syntax-theme.ts`
- Streaming assembly that keeps thinking and content ordered, updates in place, and finalizes cleanly
  - `src/tui/tui-stream-assembler.ts`
  - `src/tui/tui-event-handlers.ts`
- Rich status UX: spinner, elapsed time, waiting animation/phrases, clear activity states (`sending`, `waiting`, `streaming`, `running`, `idle`, `error`)
  - `src/tui/tui.ts`
  - `src/tui/tui-waiting.ts`
- Better operator ergonomics: keybindings, pickers, overlays, input history, slash commands, local-shell gating prompt
  - `src/tui/components/custom-editor.ts`
  - `src/tui/components/searchable-select-list.ts`
  - `src/tui/components/filterable-select-list.ts`
  - `src/tui/tui-command-handlers.ts`
  - `src/tui/tui-local-shell.ts`

## Target localclaw UX

### 1) Visual structure

Single full-screen TUI with 5 regions:

1. Header: app name, model/provider/profile, cwd/session.
2. Transcript viewport: scrolling chat timeline with message cards.
3. Status line: run state + spinner + elapsed + short hint.
4. Footer metadata: mode toggles (thinking/verbose), token usage, connection/runtime state.
5. Composer: multi-line input box with visible border, placeholder, and keybinding hints.

### 2) Message rendering

Message types:

- User message card (subtle filled background)
- Assistant message card (markdown-rendered body)
- System notice (muted single-line or compact block)
- Tool activity card (phase: running/result/error, collapsible body)

Rendering requirements:

- Parse/render markdown for assistant and tool text.
- Syntax-highlight fenced code blocks.
- Support headings, lists, links, quotes, tables, inline code.
- Preserve formatting during streaming updates (no flicker, no duplicated lines).

### 3) Streaming + thinking UX

On send:

- Immediately append user message.
- Status transitions: `sending` -> `waiting` -> `streaming` -> `idle`/`error`.
- Show spinner + elapsed timer while run is active.

While receiving model output:

- Append/update assistant card incrementally (delta streaming).
- Show a thinking indicator when no text output has arrived yet (for example, `thinking...`).
- If explicit thinking blocks are available, optionally render a `[thinking]` section above content when enabled.

On completion:

- Finalize the assistant card.
- Ensure fallback text if stream ended with empty final payload.

### 4) Input/composer UX

Multi-line editor requirements:

- `Enter`: send message
- `Alt+Enter`: newline insertion
- `Ctrl+C`: clear compose buffer; second press within 1s exits
- `Ctrl+D`: exit when compose buffer is empty
- `Up/Down`: input history navigation (submitted prompts)
- Slash command autocomplete

Editor quality:

- Bordered text area with padding
- Dynamic height growth up to max rows, then internal scroll
- Retain cursor position and selection behavior while editing

### 5) Commands and controls

Initial command set (local-first adaptation):

- `/help`
- `/clear`
- `/exit`
- `/thinking <on|off>`
- `/verbose <on|off>`
- `/model <name>` (if runtime supports model override)
- `/status`

Keyboard toggles:

- `Ctrl+T`: toggle thinking visibility
- `Ctrl+O`: toggle tool card expansion
- `Esc`: abort active run

### 6) Professional look-and-feel

Theme requirements:

- Consistent palette with clear semantic colors: accent, system, success, warning, error
- Strong contrast for readability on dark and light-capable terminals
- Distinct visual treatment for user vs assistant vs system vs tools
- Non-default, intentionally designed typography style via ANSI weight/color hierarchy

Motion/feedback:

- Spinner animation for active runs
- Smooth in-place updates for stream deltas
- No full-screen flicker on each token

## Technical design (Go)

### Libraries

Adopt Charm stack:

- `github.com/charmbracelet/bubbletea` (event loop)
- `github.com/charmbracelet/bubbles/textarea` (multi-line input)
- `github.com/charmbracelet/bubbles/viewport` (scrollable transcript)
- `github.com/charmbracelet/bubbles/spinner` (status animation)
- `github.com/charmbracelet/lipgloss` (layout/theme)
- `github.com/charmbracelet/glamour` (markdown -> ANSI)

### Package layout

- `internal/tui/app.go`: Bubble Tea model/update/view root.
- `internal/tui/theme/theme.go`: palette + styles.
- `internal/tui/render/markdown.go`: markdown renderer + width-aware cache.
- `internal/tui/chat/log.go`: message model, streaming merges, tool cards.
- `internal/tui/input/editor.go`: textarea config, keymap, history ring.
- `internal/tui/commands/commands.go`: slash command parse + handlers.
- `internal/tui/status/status.go`: state machine (`sending/waiting/streaming/running/idle/error`).
- `internal/tui/stream/assembler.go`: per-run delta assembly.

### Runtime/LLM interface changes

Extend LLM/runtime interfaces for streaming:

- `PromptStream(ctx, input) (<-chan DeltaEvent, <-chan error)`
- Keep `Prompt(ctx, input)` for synchronous fallback paths.

`internal/llm/claudecode/client.go`:

- Replace blocking `cmd.Run()` flow with `StdoutPipe`/`StderrPipe` streaming reader.
- Emit delta events as stdout arrives.
- Emit final event on process exit.

`internal/runtime/app.go`:

- Add stream-oriented method used by TUI.

### State machine

Per run:

- `pending` -> `waiting` -> `streaming` -> `done|aborted|error`

UI status text is derived from this machine and includes elapsed duration.

### Rendering strategy

- Store raw markdown per assistant/tool message.
- Re-render only changed messages or when terminal width changes.
- Cache rendered ANSI output by `(messageID, width, version)`.
- Keep viewport anchored to bottom only when the user is already at bottom.

### Failure behavior

- Errors render as system notices, never panic the TUI.
- If markdown render fails, fall back to plain text.
- If streaming path fails, fall back to synchronous `Prompt` response.

## Implementation phases

### Phase 1: Foundation UI shell

- Bubble Tea app skeleton with regions (header/transcript/status/footer/composer).
- Theme and baseline message cards.
- Existing synchronous prompt path wired into new UI.

Acceptance:

- `go run ./cmd/localclaw tui` opens a full-screen app with styled layout.
- Send/receive works without streaming.

### Phase 2: Streaming pipeline

- Add `PromptStream` plumbing from Claude CLI to TUI.
- Per-run stream assembler and in-place transcript updates.
- Spinner + elapsed timer + status transitions.

Acceptance:

- Assistant response visibly streams.
- Status transitions are correct for normal, abort, and error cases.

### Phase 3: Markdown + syntax highlighting

- Glamour integration with custom style.
- Code-fence highlighting and link/list/quote/table rendering.
- Width-aware caching and reflow on resize.

Acceptance:

- Markdown fixtures render correctly and remain readable during resize.

### Phase 4: Advanced composer and controls

- Multi-line editing (`Alt+Enter` newline), history, keybindings.
- Slash command parser + autocomplete + toggles.
- Tool card expand/collapse behaviors.

Acceptance:

- Keyboard workflows are efficient and stable in long sessions.

### Phase 5: Polish and hardening

- Performance tuning for long transcripts.
- Deterministic tests for stream assembly and command parsing.
- End-to-end interaction tests with mocked streaming client.

Acceptance:

- No visible flicker under long streaming output.
- Test suite covers core UX logic.

## Test plan

Unit tests:

- Stream assembler ordering/fallback cases.
- Command parsing and alias handling.
- Status machine transitions.
- Markdown render fallback behavior.

Integration tests:

- Fake streaming LLM client with deterministic deltas.
- Abort path while streaming.
- Resize path reflow.

Manual QA scenarios:

- Long code-block response
- Very long multi-line prompt editing
- Error response from LLM process
- Rapid consecutive sends

## Explicit non-goals (initial parity pass)

- Gateway-based remote TUI protocol (openclaw-specific architecture)
- Multi-agent/session pickers backed by a remote registry
- Cross-channel delivery controls (Slack/Signal routing UI)

These can be layered later after core local TUI UX is complete.

## Success criteria

- Users can run `go run ./cmd/localclaw tui` and get a polished full-screen app.
- Assistant responses stream progressively with markdown formatting.
- Input experience supports real multi-line authoring and history editing.
- Run state/thinking/tool feedback is visible and understandable at all times.
