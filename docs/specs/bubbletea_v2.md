# Bubble Tea v2 Migration Plan (Shift+Enter Reliability)

## Status

Draft (research complete, implementation not started)

## V1 Build Policy

`localclaw` is still pre-v1. Favor clean implementations only:
- no rollback plans
- no fallback execution paths unless explicitly requested
- no legacy compatibility shims

## Problem / Motivation

`localclaw` currently uses:
- `github.com/charmbracelet/bubbletea v0.21.0`
- `github.com/charmbracelet/bubbles v0.13.0`
- `github.com/charmbracelet/lipgloss v0.6.0`

On this stack, `Shift+Enter` is not reliably distinguishable from `Enter` in many terminals. The product requirement is to support multiline authoring where `Shift+Enter` inserts a newline and `Enter` submits.

Upstream direction is Bubble Tea v2 keyboard enhancements. This spec defines a staged migration plan to Bubble Tea v2-era APIs and dependencies so Shift+Enter is available wherever the terminal path supports progressive keyboard enhancements.

## Research Summary (As Of February 18, 2026)

- Bubble Tea release channel:
  - Latest stable v1 release: `v1.3.10` (published September 17, 2025).
  - Latest v2 release: `v2.0.0-rc.2` (published November 17, 2025), marked prerelease.
- Bubble Tea maintainer guidance (issue/PR thread #1385, April 10, 2025):
  - Shift+Enter is not possible in many terminals without newer keyboard enhancement support.
  - Maintainer points to Bubble Tea v2 keyboard enhancements documentation.
- Bubble Tea v2 discussion (`#1374`) documents:
  - progressive keyboard enhancements
  - `KeyboardEnhancementsMsg`
  - support for combinations including Shift+Enter in compatible terminals
  - known terminal support list (Ghostty, Kitty, Alacritty, iTerm2, Foot, WezTerm, Rio, Contour)
- tmux caveat:
  - PR #1579 (merged February 9, 2026 into `v2-exp`) adds `modifyOtherKeys` handling for tmux modified keys.
  - This landed after `v2.0.0-rc.2`; therefore rc.2 may still require tmux-side configuration for modified keys.
- Toolchain constraint:
  - Bubble Tea `v1.3.10`, Bubble Tea `v2.0.0-rc.2`, Bubbles `v1.0.0`, and Bubbles `v2.0.0-rc.1` all declare `go 1.24.x` in upstream `go.mod`.
  - `localclaw` is currently pinned to Go `1.17`.

## Scope

- Define migration from current Bubble Tea/Bubbles/Lip Gloss stack to v2-era stack for TUI.
- Document exact API migration points relevant to `internal/tui`.
- Define target behavior contract for `Shift+Enter` newline vs `Enter` submit.
- Define package/test/doc updates required for implementation.
- Define rollout validation matrix (terminals, tmux, local CLI paths).

## Out of Scope

- Implementing the migration in this spec.
- Introducing HTTP/gateway/server runtime changes.
- Redesigning non-keyboard TUI UX beyond what API migration requires.
- Reworking runtime/session/memory architecture unrelated to TUI input handling.

## Behavior Contract

Expected keyboard behavior after migration:

- `Enter` submits composer input.
- `Shift+Enter` inserts newline when terminal path supports key disambiguation.
- Existing explicit multiline keys remain available:
  - `Alt+Enter` inserts newline.
  - `Ctrl+J` inserts newline.
- If the terminal cannot disambiguate Shift+Enter, behavior degrades gracefully:
  - `Shift+Enter` may be indistinguishable from `Enter`.
  - documented fallback keys (`Alt+Enter`, `Ctrl+J`) remain functional.

Non-keyboard behavior that must remain unchanged:

- Status lifecycle semantics (`sending -> waiting -> streaming -> idle/error`).
- Slash commands and history semantics.
- Mouse toggle behavior contract (`Ctrl+Y`) from user perspective.

## API Change Inventory (Current Stack -> Bubble Tea v2)

| Area | Current usage in localclaw | v2 target |
| --- | --- | --- |
| Module path | `github.com/charmbracelet/bubbletea` | `charm.land/bubbletea/v2` |
| Bubbles imports | `github.com/charmbracelet/bubbles/...` | `charm.land/bubbles/v2/...` |
| Lip Gloss imports | `github.com/charmbracelet/lipgloss` | `charm.land/lipgloss/v2` |
| Model view signature | `View() string` | `View() tea.View` |
| Program startup | `p.Start()` | `_, err := p.Run()` |
| Alt screen enable | `tea.WithAltScreen()` | `view.AltScreen = true` |
| Mouse mode enable | `tea.WithMouseCellMotion()` and `tea.EnableMouseCellMotion`/`tea.DisableMouse` cmds | `view.MouseMode = tea.MouseModeCellMotion` or `tea.MouseModeNone` |
| Key message type | `tea.KeyMsg` struct with `msg.Type/msg.Alt/msg.Runes` | `tea.KeyPressMsg` (and optional `tea.KeyReleaseMsg`), with `msg.String()/msg.Keystroke()/msg.Code/msg.Text/msg.Mod` |
| Keyboard enhancements | none in current stack | v2 progressive key disambiguation + `KeyboardEnhancementsMsg` detection |
| Tests relying on internals | `startupOptions` reflection, command concrete type names | assert emitted view state and functional behavior instead |

## Migration Plan

### Phase 0: Decision Gates (Must Pass Before Coding)

1. Approve toolchain uplift from Go `1.17` to Go `1.24+`.
2. Choose v2 target:
   - Option A (recommended): wait for Bubble Tea v2 GA (or RC containing tmux modified-key fix lineage from #1579).
   - Option B: adopt `v2.0.0-rc.2` with documented tmux caveat and manual tmux config guidance.
3. Approve dependency family move to Charm vanity imports (`charm.land/.../v2`).

### Phase 1: Toolchain and Dependency Baseline

1. Update repo Go version and module requirements.
2. Upgrade TUI dependency set together:
   - Bubble Tea -> v2 target
   - Bubbles -> v2 target
   - Lip Gloss -> v2 target
3. Run compile-only pass to identify first-order API breaks.

Primary files:
- `go.mod`
- `go.sum`
- TUI package files under `internal/tui`

### Phase 2: Bubble Tea Core API Migration

1. Convert model `View() string` to `View() tea.View`.
2. Move startup/view flags to declarative view fields:
   - `AltScreen`
   - `MouseMode`
3. Replace `p.Start()` with `p.Run()` pattern in TUI runner.
4. Remove old imperative mouse commands in update handlers.

Primary files:
- `internal/tui/app.go`
- `internal/tui/slash.go`
- `internal/tui/update_keys.go`

### Phase 3: Key Event Model Migration (Shift+Enter Target)

1. Update key handling switches:
   - `case tea.KeyMsg` struct usage -> `case tea.KeyPressMsg`
2. Rewrite key checks from `msg.Type/msg.Alt/msg.Runes` to v2 equivalents:
   - `msg.String()` for binding matching
   - `msg.Text` for text payload/multiline paste handling
   - `msg.Mod.Contains(tea.ModAlt)` where modifier checks are required
3. Add explicit `shift+enter` newline handling in composer path.
4. Keep existing newline alternatives (`alt+enter`, `ctrl+j`) as explicit supported gestures.
5. Optional capability-awareness:
   - record `KeyboardEnhancementsMsg` support and expose it in verbose diagnostics.

Primary files:
- `internal/tui/update_keys.go`
- `internal/tui/model.go`
- `internal/tui/app.go`

### Phase 4: Test Migration and Coverage Expansion

1. Replace synthetic v1 key events in tests with v2 press event equivalents.
2. Remove tests that assert Bubble Tea v1 internals (`startupOptions` bitfield, internal command type names).
3. Add/adjust focused tests:
   - Shift+Enter inserts newline
   - Enter submits
   - Alt+Enter and Ctrl+J still insert newline
   - Multiline paste normalization still works
   - Mouse toggle behavior still changes effective mode

Primary files:
- `internal/tui/app_test.go`
- `internal/tui/app_stream_test.go`

### Phase 5: Docs and Operator Guidance

1. Update TUI docs for final keyboard behavior and compatibility notes.
2. Add terminal/tmux troubleshooting note for modified keys.
3. Update README shortcuts table as needed.

Primary files:
- `README.md`
- `docs/TUI.md`

### Phase 6: Validation Matrix and Sign-off

Required validation:

1. Unit and package test commands:
   - `go test ./internal/tui`
   - `go test ./internal/runtime`
   - `go test ./...`
2. Runtime smoke:
   - `go run ./cmd/localclaw tui`
3. Manual keyboard checks:
   - Terminal with known enhancement support (e.g. Ghostty/Kitty/WezTerm).
   - tmux path:
     - with tmux extended keys configured
     - without tmux extended keys configured (document expected degradation)

## File-Level Impact Plan

- `go.mod`: Go version bump; charm v2 dependency set.
- `go.sum`: dependency graph refresh.
- `internal/tui/app.go`: `Run()` usage; `View() tea.View` integration.
- `internal/tui/model.go`: style/type adjustments for v2 dependency types.
- `internal/tui/update_keys.go`: key event API conversion, Shift+Enter handling.
- `internal/tui/slash.go`: mouse mode toggling via model/view state, not legacy commands.
- `internal/tui/app_test.go`: key message fixture conversion; remove v1 internal assertions.
- `internal/tui/app_stream_test.go`: startup option assertion rewrite for declarative view semantics.
- `docs/TUI.md`: updated key behavior and compatibility notes.
- `README.md`: user-facing shortcut/help text updates.

## Risks and Mitigations

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Go 1.24+ prerequisite conflicts with current Go 1.17 policy | Blocks migration start | explicit phase-0 approval gate before coding |
| Bubble Tea v2 prerelease churn | rework cost | target GA or latest RC only; freeze versions per implementation branch |
| tmux modified keys may still fail on rc.2 | Shift+Enter still unreliable in tmux by default | either target release containing #1579 lineage or require/document tmux extended-key config |
| Large test surface tied to v1 internals | noisy failures | refactor tests to behavior assertions instead of internal Bubble Tea fields |
| Cross-package import churn (`charm.land/...`) | compile break in many files | migrate TUI package in one focused branch with tight compile/test loop |

## Test Plan

Red/Green workflow during implementation:

1. Add failing tests for v2 key handling contract in `internal/tui/app_test.go`:
   - Shift+Enter newline
   - Enter submit
2. Run focused TUI tests and confirm red:
   - `go test ./internal/tui -run Test.*(Shift|Enter|Multiline|Mouse).*`
3. Implement minimal migration increments and get green:
   - `go test ./internal/tui`
4. Run full suite:
   - `go test ./...`
5. Run smoke checks:
   - `go run ./cmd/localclaw tui`

## Acceptance Criteria

- [ ] Repository builds and tests pass on approved Go toolchain version.
- [ ] `internal/tui` migrated to Bubble Tea v2-era APIs with no Bubble Tea v1-only API usage.
- [ ] In enhancement-capable terminal paths, `Shift+Enter` inserts newline and `Enter` submits.
- [ ] `Alt+Enter` and `Ctrl+J` still insert newline.
- [ ] Mouse toggle and slash/history behavior remain functionally equivalent to pre-migration behavior.
- [ ] Docs (`README.md`, `docs/TUI.md`) updated with accurate key behavior and compatibility notes.

## Sources

- Bubble Tea releases: https://github.com/charmbracelet/bubbletea/releases
- Bubble Tea v2 beta 1 release: https://github.com/charmbracelet/bubbletea/releases/tag/v2.0.0-beta.1
- Bubble Tea v2 beta 3 release: https://github.com/charmbracelet/bubbletea/releases/tag/v2.0.0-beta.3
- Bubble Tea v2 beta 5 release: https://github.com/charmbracelet/bubbletea/releases/tag/v2.0.0-beta.5
- Bubble Tea v2 rc.1 release: https://github.com/charmbracelet/bubbletea/releases/tag/v2.0.0-rc.1
- Bubble Tea v2 rc.2 release: https://github.com/charmbracelet/bubbletea/releases/tag/v2.0.0-rc.2
- Bubble Tea v2 announcement/discussion (`Keyboard Enhancements`): https://github.com/charmbracelet/bubbletea/discussions/1374
- Shift+Enter maintainer response thread (#1385): https://github.com/charmbracelet/bubbletea/pull/1385
- tmux modified-key fix PR (#1579): https://github.com/charmbracelet/bubbletea/pull/1579
- Bubble Tea v2 upgrade guide (`v2-exp`): https://raw.githubusercontent.com/charmbracelet/bubbletea/v2-exp/UPGRADE_GUIDE_V2.md
- Bubble Tea v2 `go.mod` (Go requirement): https://raw.githubusercontent.com/charmbracelet/bubbletea/v2.0.0-rc.2/go.mod
- Bubbles v2 `go.mod` (Go requirement): https://raw.githubusercontent.com/charmbracelet/bubbles/v2.0.0-rc.1/go.mod
