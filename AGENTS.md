# Repository Guidelines

This file is the source of truth for agentic coding practices in `localclaw`.
Keep it current when architecture, tooling, or workflows change.

## Layered Context Specifications

- `README.md` gives product goals, quick start, and operator-facing usage.
- `ARCHITECTURE.md` is the short architecture snapshot.
- `docs/ARCHITECTURE.md` is the implementation-detail architecture reference.
- `docs/RUNTIME.md` documents startup wiring and runtime lifecycle.
- `docs/CONFIGURATION.md` documents config schema, defaults, and validation guardrails.
- `docs/TOOLS.md` documents MCP/provider tools architecture and implementation details.
- `docs/HEARTBEATS.md` documents heartbeat scheduling behavior and `HEARTBEAT.md` usage.
- `docs/SLACK.md` documents Slack channel setup and outbound delivery behavior.
- `docs/SIGNAL.md` documents Signal channel setup and `signal-cli` runtime behavior.
- `docs/TUI.md` documents full-screen TUI behavior and controls.
- `docs/CLAUDE_CODE.md` documents local Claude Code CLI integration details.
- `docs/TESTING.md` is the comprehensive testing guide.
- `docs/SECURITY.md` defines security boundaries and local-only posture.
- `docs/specs/**` contains feature specs and design notes.

## Current Stack (Canonical)

- Language/runtime: Go `1.17`.
- Process model: single-process CLI only (`cmd/localclaw`).
- TUI stack: Bubble Tea + Bubbles + Lip Gloss + Glamour.
- LLM adapter: local Claude Code CLI invocation (`internal/llm/claudecode`).
- Channel boundaries: Slack and Signal local adapters only.
- No HTTP/gateway/server runtime is allowed.

## Project Structure & Module Organization

- Entrypoint:
  - `cmd/localclaw/main.go` (`doctor`, `tui`, `memory`, `channels`, `mcp` command modes).
- Core orchestration:
  - `internal/runtime/app.go`
  - `internal/runtime/tools.go`
- Policy and configuration:
  - `internal/config/config.go`
- LLM integration:
  - `internal/llm/claudecode/client.go`
- User interface:
  - `internal/tui/app.go`
- Capability modules (in-process boundaries):
  - `internal/memory`
  - `internal/workspace`
  - `internal/session`
  - `internal/hooks`
  - `internal/skills`
  - `internal/cron`
  - `internal/heartbeat`
  - `internal/channels/slack`
  - `internal/channels/signal`
  - `internal/cli` (command mode helpers)

## Build, Test, and Development Commands

- Full test suite:
  - `go test ./...`
- Focused package tests:
  - `go test ./internal/config`
  - `go test ./internal/runtime`
  - `go test ./internal/tui`
  - `go test ./internal/workspace`
  - `go test ./internal/session`
  - `go test ./internal/memory`
  - `go test ./internal/cli`
  - `go test ./internal/hooks`
- Run startup checks:
  - `go run ./cmd/localclaw`
  - `go run ./cmd/localclaw doctor`
  - `go run ./cmd/localclaw doctor --deep`
- Run TUI:
  - `go run ./cmd/localclaw tui`
- Run memory command mode:
  - `go run ./cmd/localclaw memory status`
  - `go run ./cmd/localclaw memory index --force`
  - `go run ./cmd/localclaw memory search "incident summary"`
- Run channels command mode:
  - `go run ./cmd/localclaw channels serve`
  - `go run ./cmd/localclaw channels serve --once`
- Run with explicit config file:
  - `go run ./cmd/localclaw -config ./localclaw.json doctor`
  - `go run ./cmd/localclaw -config ./localclaw.json tui`
  - `go run ./cmd/localclaw -config ./localclaw.json memory status`
  - `go run ./cmd/localclaw -config ./localclaw.json channels serve`
- Formatting:
  - `go fmt ./...`

## Agentic Workflow (TDD Default)

Behavior changes should follow Red -> Green -> Validate -> Deliver.

1. Understand and scope:
   - Define expected inputs, outputs, errors, and unchanged behavior.
2. Write failing test first (Red):
   - Add the smallest targeted test in the most relevant package.
3. Validate Red:
   - Run a focused test command and confirm failure for the intended reason.
4. Implement minimum fix (Green):
   - Change only what is required to satisfy the failing behavior.
5. Validate Green:
   - Re-run focused tests, then run `go test ./...`.
6. Quality pass:
   - Run `go fmt ./...` when Go files changed.
7. Deliver:
   - Summarize behavior change, commands run, and outcomes.

## Validation Commands Quick Reference

| Layer | Command | When to Use |
| --- | --- | --- |
| Config validation | `go test ./internal/config -run TestValidate` | During Red/Green on config/policy changes |
| Runtime startup boundary behavior | `go test ./internal/runtime -run TestNewFailsWhenClaudeMCPWiringInvalid` | During Red/Green on startup boundary changes |
| Runtime tool prompt behavior | `go test ./internal/runtime -run TestPromptIncludesMemoryRecallPolicyWhenToolsEnabled` | During Red/Green on tool/prompt assembly changes |
| TUI slash logic | `go test ./internal/tui -run TestParseSlash` | During Red/Green on slash/UX behavior |
| Workspace bootstrap behavior | `go test ./internal/workspace -run TestEnsureWorkspaceCreatesWorkspaceAndBootstrapFiles` | During Red/Green on workspace changes |
| Session store safety | `go test ./internal/session -run TestUpdateConcurrentDoesNotCorruptSessionsFile` | During Red/Green on session persistence |
| Memory indexing/search | `go test ./internal/memory` | During Red/Green on memory changes |
| Full Go suite | `go test ./...` | Before completion |
| CLI startup smoke | `go run ./cmd/localclaw` | Before completion for startup-related changes |
| TUI smoke | `go run ./cmd/localclaw tui` | Before completion for TUI-related changes |

## Runtime Rules (`cmd/` and `internal/runtime`)

### Process and boundary constraints

- Keep `localclaw` single-process and local-only.
- Do not introduce HTTP/gRPC servers, gateway mode, or listeners.
- Keep runtime wiring centralized in `runtime.New`.
- Preserve startup order in `App.Run`:
  - workspace init
  - bootstrap config file
  - memory init
  - session init
  - skills load
  - cron start
  - heartbeat ping

### Command-mode behavior

- Supported command modes are `doctor`, `tui`, `memory`, `channels`, and `mcp`.
- If adding a new mode:
  - wire it in `cmd/localclaw/main.go`
  - add mode-specific tests
  - document it in `README.md` and docs under `docs/`

## Configuration Rules (`internal/config`)

- Any new config field must be reflected in:
  - `Config` structs
  - `Default()` values
  - `Validate()` checks when relevant
- Do not add legacy/fallback compatibility shims unless explicitly requested for a concrete migration.
- Prefer fail-fast behavior for removed or deprecated config keys.
- Preserve strict local-only boundaries (no gateway/listener config surface).
- If channel or auth-mode allowlists change, update:
  - code allowlists
  - tests
  - `docs/CONFIGURATION.md`
  - `docs/SECURITY.md` if trust boundaries change

## LLM Adapter Rules (`internal/llm/claudecode`)

- Keep execution local via subprocess (`exec.CommandContext`).
- Do not add direct network model clients in `localclaw`.
- Preserve both APIs:
  - `Prompt` for synchronous consumption
  - `PromptStream` for incremental output
- Handle cancellation and stream shutdown cleanly.
- Surface stderr context in returned errors when command execution fails.

## TUI Rules (`internal/tui`)

- Keep keyboard controls consistent unless explicitly changing UX contract.
- If keybindings or slash commands change, update:
  - `README.md`
  - `docs/TUI.md`
  - `internal/tui/app_test.go`
- Maintain status lifecycle semantics: sending -> waiting -> streaming -> idle/error.
- Avoid regressions in multiline input, history navigation, and run-abort behavior.

## Docs and Specs Rules

- Non-trivial behavior or architecture changes should start with a spec in `docs/specs/`.
- Keep specs implementation-linked:
  - expected behavior
  - test plan
  - acceptance criteria
- Keep `docs/TESTING.md` aligned with real test locations and commands.

## Git Hygiene Rules

- Use Conventional Commit prefixes (`feat:`, `fix:`, `docs:`, `chore:`, etc.).
- Stage intentionally; avoid blind `git add .` when preventable.
- Never commit:
  - secrets or local env files
  - editor caches and temporary files
  - compiled binaries or build artifacts
- Before commit:
  - `git status`
  - `git diff --staged`
  - verify only intended files are staged

## Pull Request / Handoff Expectations

- Keep changes focused and reviewable.
- Include:
  - concise behavior summary
  - commands run and outcomes
  - explicit note when tests were skipped and why
- For behavior changes, include corresponding tests whenever practical.

## Quality Checklist (Before Marking Done)

- Relevant tests pass for changed areas.
- `go test ./...` has been run when code changed.
- `go fmt ./...` has been run when Go files changed.
- Specs/docs were updated for behavior or workflow changes.
- Local-only boundary constraints remain intact.
