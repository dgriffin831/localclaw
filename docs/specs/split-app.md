# Split `internal/tui/app.go` Into Maintainable Modules

## Status

Draft v1.0

## Problem

`internal/tui/app.go` has grown to ~2050 lines and currently mixes multiple concerns:

- Bubble Tea lifecycle wiring (`Run`, `Init`, `Update`, `View`)
- input and keybinding behavior
- slash command parsing, autocomplete, and execution
- stream event handling and run lifecycle management
- transcript rendering and tool-card formatting
- markdown theme configuration and general helpers

This makes the file hard to navigate, increases merge conflict frequency, and slows safe changes because unrelated logic lives in one place.

## Scope

- Refactor `internal/tui/app.go` into multiple files within `internal/tui` (same package).
- Preserve external behavior and public entry points.
- Keep each resulting file under 1000 lines, with a target of substantially smaller modules.
- Remove legacy naming that references external inspiration in code identifiers.

## Out of Scope

- Feature changes to TUI behavior.
- New slash commands, keybindings, or runtime API changes.
- Rewriting UI styling or tool-card UX beyond parity-preserving extraction.
- Moving TUI logic into a different package.

## Behavior Contract

### Inputs

- Existing TUI messages/events (`tea.Msg`, stream events, slash input, keyboard input).
- Existing runtime/session/config dependencies passed into `newModel` and `Run`.

### Outputs

- Identical user-facing behavior for:
  - keybindings
  - slash command handling and autocomplete
  - tool-card rendering and expansion
  - status/header/input rendering
  - stream lifecycle transitions and transcript writes

### Error Paths

- Existing error handling semantics remain unchanged (prompt errors, runtime unavailable, abort/reset behavior).

### Unchanged Behavior

- `Run(ctx, app, cfg)` remains the TUI entry point.
- Session reset/new behavior remains unchanged.
- Tool ownership labels and `/tools` output semantics remain unchanged.
- Markdown rendering behavior remains unchanged.

## Proposed Module Split

All files remain in package `tui`.

1. `internal/tui/app.go` (~150-300 lines)
- Keep only entry points and top-level Bubble Tea hooks:
  - `Run`
  - `newProgram`
  - `Init`
  - `Update` (dispatcher only)
  - `View` (or a thin wrapper)

2. `internal/tui/model.go` (~250-400 lines)
- Core model/type definitions and constructor:
  - constants/types (`model`, `chatMessage`, `toolCardMessage`, msg wrappers)
  - style/color vars
  - `newModel`

3. `internal/tui/update_keys.go` (~250-450 lines)
- Key-driven update behavior:
  - key event routing
  - mouse toggle handling
  - history/slash selection key transitions
  - small helper handlers per key family

4. `internal/tui/update_stream.go` (~250-450 lines)
- Stream/status event handling:
  - stream delta/final/tool/provider metadata branches
  - status ticker and spinner handling
  - run-id and channel-close guards

5. `internal/tui/view_layout.go` (~200-350 lines)
- Rendering and layout:
  - `headerView`, `statusView`, `inputView`
  - `layout`, `adjustInputHeight`
  - shared layout helpers (`twoColumn`, `truncateText`, etc.)

6. `internal/tui/slash.go` (~250-450 lines)
- Slash command system:
  - command definitions
  - parser/autocomplete/menu formatting
  - `handleSlash`, `/tools` summary

7. `internal/tui/run_state.go` (~220-400 lines)
- Prompt run lifecycle and transcript write flow:
  - `submitInput`, `startRun`, `finishRun`, `abortRun`
  - `applyDelta`, `applyFinal`, `runSessionReset`

8. `internal/tui/transcript.go` (~300-500 lines)
- Transcript message construction and rendering:
  - `addSystem`/`addUser`/`addAssistant`
  - `renderTranscript`, `refreshViewport`
  - tool-card rendering/formatting helpers

9. `internal/tui/markdown_style.go` (~100-200 lines)
- Markdown renderer style config and pointer helpers:
  - `localclawMarkdownStyles`
  - `strPtr`, `boolPtr`, `uintPtr`

10. `internal/tui/diagnostics.go` (~120-240 lines)
- Verbose diagnostics and helper summarizers:
  - `addVerbose`, `emitVerboseRunStartDiagnostics`
  - `summarizeVerbose*`, `truncateVerboseText`

## Refactor Strategy (Incremental)

1. Baseline safety
- Run targeted TUI tests before changes:
  - `go test ./internal/tui`

2. Extract pure helpers first
- Move parsing/formatting functions with no side effects first.
- Re-run `go test ./internal/tui`.

3. Extract rendering modules
- Move header/status/input/transcript rendering.
- Re-run `go test ./internal/tui`.

4. Extract slash and history behavior
- Move slash parser/autocomplete/handlers and history methods.
- Re-run focused tests (`TestParseSlash`, slash/menu/history tests).

5. Extract run + stream lifecycle
- Move `startRun`/`finishRun`/stream event branches last since they have the most state coupling.
- Re-run `go test ./internal/tui`.

6. Final validation
- `go fmt ./...`
- `go test ./...`

## Naming Cleanup

Legacy naming that references external sources should be removed during this refactor.

- Rename the markdown style helper to a localclaw-owned identifier (`localclawMarkdownStyles`).
- Keep all style/theme helper names localclaw-owned and domain-oriented.

## Test Plan

- Keep existing `internal/tui/app_test.go` coverage passing during each extraction step.
- Add targeted tests only when refactor introduces helper seams that need direct verification.
- Run:
  - `go test ./internal/tui`
  - `go test ./...`

## Acceptance Criteria

- [ ] No single file in `internal/tui` exceeds 1000 lines.
- [ ] `internal/tui/app.go` is reduced to orchestration-focused code and is significantly smaller.
- [ ] Behavior remains unchanged as validated by existing TUI tests.
- [ ] Full repository tests pass (`go test ./...`).
- [ ] No legacy external-source naming references remain in code identifiers.

## Risk Notes

Primary risk is accidental behavioral drift during method moves due to shared mutable state on `model`.

Mitigations:
- extract in small phases with tests run after each phase
- avoid signature changes unless required
- keep methods on `model` until a later, deliberate redesign
