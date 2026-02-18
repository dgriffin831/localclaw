# Queue Inputs During Active Runs

## Status

Draft

## V1 Build Policy

`localclaw` is still pre-v1. Favor clean implementations only:
- no rollback plans
- no fallback execution paths unless explicitly requested
- no legacy compatibility shims

## Problem

The TUI currently rejects submissions while a run is active with `run already in progress; press Esc to abort`. This blocks fast follow-up prompting, forces users to wait for completion, and makes multi-step interactions slower than necessary.

## Scope

- Replace active-run submission rejection for prompt inputs with in-memory queueing.
- Preserve FIFO execution order for queued prompt inputs.
- Automatically start the next queued prompt when the active run ends.
- Render queued prompt inputs above the composer input box.
- Render each queued item as a single-line preview and truncate to available width.
- Remove the `run already in progress; press Esc to abort` system message for queued prompt submissions.

## Out of Scope

- Persisting queued inputs across app restarts.
- Queue reordering, editing, or manual removal commands.
- New slash commands for queue inspection/management.
- Changing provider/runtime stream behavior outside TUI orchestration.

## Behavior Contract

Queue semantics:
- Queue applies to prompt submissions (non-slash text) when `m.running == true`.
- Queue order is FIFO; the oldest queued item runs first.
- Submitting while active run is in progress appends to queue, clears composer input, updates history, and does not append a rejection system message.

Dequeuing semantics:
- When a run transitions to a terminal state (`idle`, `error`, or `aborted`), the next queued prompt starts automatically if queue length > 0.
- Automatic dequeue continues until queue is empty, one run at a time.
- If a session boundary is reset/switched (`/new`, `/reset`, `/resume`), queued inputs are cleared.

Slash command semantics:
- Slash commands are not queued and continue to follow existing slash handling/guardrails.
- Existing command-specific active-run guards (for example selector changes that require abort first) remain unchanged.

Queued list rendering:
- Queue list is rendered in the composer panel above the input box.
- Display order is execution order (next-to-run first).
- Each queued input preview is exactly one rendered line.
- Multi-line queued text is flattened to a single line for preview (newline characters replaced with spaces).
- Long previews are truncated to available width using ellipsis so layout remains stable.

## Implementation Notes

Expected touch points:
- `internal/tui/model.go`
  - add queue state to model (for example `queuedInputs []string`)
- `internal/tui/run_state.go`
  - update `submitInput()` to enqueue prompt inputs during active runs
  - add dequeue/start-next helper(s)
- `internal/tui/update_stream.go`
  - trigger dequeue after run completion/error transitions
- `internal/tui/update_keys.go`
  - ensure abort path cooperates with dequeue contract
- `internal/tui/view_layout.go`
  - render queue list block above composer input with truncation
- `internal/tui/app_test.go` and/or `internal/tui/app_stream_test.go`
  - add queue behavior and rendering coverage
- `README.md` and `docs/TUI.md`
  - document queueing behavior and visible UI changes

Design notes:
- Keep queue state in-memory and session-local.
- Keep existing transcript behavior unchanged: user message is appended only when a queued item actually starts running, not when it is enqueued.
- Reuse existing truncation helpers (`truncateText`) for queue preview rendering.

## Test Plan

- Unit tests to add/update:
  - `submitInput` enqueues prompt input while run active and does not emit rejection system message.
  - queued prompt auto-starts after final stream event.
  - queued prompt auto-starts after run error and after manual abort.
  - queue is cleared on `/new`, `/reset`, and `/resume`.
  - queue list renders above composer as single-line truncated previews.
  - slash submissions are not enqueued.
- Package-focused Red/Green command:
  - `go test ./internal/tui -run 'Test(SubmitInputQueuesWhileRunActive|QueuedInputAutoRuns|QueueClearedOnSessionLifecycle|QueueListRendering|SlashNotQueued)'`
- Full validation command:
  - `go test ./...`

## Acceptance Criteria

- [ ] Pressing `Enter` on prompt text while a run is active queues the input instead of showing `run already in progress; press Esc to abort`.
- [ ] Queued prompts are shown above the input box as single-line truncated entries in FIFO order.
- [ ] When the active run ends, the next queued prompt starts automatically without additional user action.
- [ ] Session lifecycle actions (`/new`, `/reset`, `/resume`) do not carry queued prompts into the next/reset session.
