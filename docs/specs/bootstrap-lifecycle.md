# BOOTSTRAP.md Sentinel Lifecycle

## Status

Draft

## V1 Build Policy

`localclaw` is still in v1 build-out. This spec intentionally avoids rollback plans, fallback execution paths, and legacy compatibility requirements. Implementation should be clean and forward-only.

## Problem

`localclaw` currently creates `BOOTSTRAP.md` only when the workspace directory itself is created in the same run (`WorkspaceInfo.Created`).

That diverges from the OpenClaw pattern where bootstrap state is file-driven:
- `BOOTSTRAP.md` exists => bootstrap is still pending.
- `BOOTSTRAP.md` deleted => bootstrap is complete.

Current gap in `localclaw`:
- If a workspace directory already exists but is otherwise uninitialized (for example pre-created empty dir), `BOOTSTRAP.md` is not created, so first-run setup guidance is skipped.
- The lifecycle contract is tied to directory creation timing instead of workspace initialization state.

## Scope

- Make `BOOTSTRAP.md` a true sentinel file in `internal/workspace` lifecycle.
- Align bootstrap creation semantics with OpenClaw-style "brand new workspace" detection.
- Keep `BOOTSTRAP.md` as one-time setup guidance that should be deleted after setup.
- Define bootstrap-pending state purely by file presence (`BOOTSTRAP.md` exists).
- Add/adjust tests in `internal/workspace/manager_test.go`.
- Update docs that describe workspace bootstrap behavior.

## Out of Scope

- Adding a new onboarding wizard or interactive setup flow.
- Adding HTTP/gateway/listener behaviors.
- Adding config toggles for alternate bootstrap state storage.
- Changing existing template content beyond wording needed to clarify sentinel semantics.

## Behavior Contract

Define expected behavior in concrete terms:

- inputs
  - `EnsureWorkspace(ctx, agentID, ensureBootstrap=true)` call.
  - Resolved workspace path.
  - Existing workspace files at call time.

- outputs
  - Non-bootstrap templates are still seeded when missing:
    - `AGENTS.md`, `SOUL.md`, `TOOLS.md`, `IDENTITY.md`, `USER.md`, `HEARTBEAT.md`, `WELCOME.md`.
  - `BOOTSTRAP.md` is created only when workspace is "brand new".
  - "Brand new" is defined as: none of the core setup files exist at call time:
    - `AGENTS.md`, `SOUL.md`, `TOOLS.md`, `IDENTITY.md`, `USER.md`, `HEARTBEAT.md`.
  - If `BOOTSTRAP.md` exists, bootstrap is pending.
  - If `BOOTSTRAP.md` does not exist, bootstrap is not pending (completed or intentionally skipped).
  - Deleting `BOOTSTRAP.md` after setup is the completion action; normal subsequent `EnsureWorkspace` calls must not recreate it.

- error paths
  - Existing workspace/file stat and write failures remain fail-fast with wrapped errors.
  - No fallback bootstrap-state storage (db/session/config) is introduced.

- unchanged behavior
  - Runtime prompt injection remains file-based: `BOOTSTRAP.md` content is injected only when the file exists.
  - Workspace bootstrap files still do not overwrite existing files.
  - Local-only, single-process runtime boundaries remain unchanged.

## Implementation Notes

- `internal/workspace/manager.go`
  - Replace `BOOTSTRAP.md` creation gate from "workspace directory was just created" to "workspace is brand new based on missing core setup files".
  - Evaluate brand-new state before writing new files.
  - Keep existing write-if-missing behavior for all templates.

- `internal/workspace/manager_test.go`
  - Add targeted tests for:
    - pre-existing empty workspace directory still gets `BOOTSTRAP.md`.
    - pre-existing workspace with any core setup file does not get `BOOTSTRAP.md`.
    - deleting `BOOTSTRAP.md` and re-running ensure does not recreate it when core files remain.

- Docs updates expected
  - `docs/ARCHITECTURE.md`: update bootstrap creation rule.
  - `docs/RUNTIME.md`: clarify sentinel semantics for bootstrap context.
  - `README.md`: keep operator-facing statement consistent (exists => pending, delete => complete).
  - `docs/TESTING.md`: include new focused workspace tests if commands/examples change.

## Test Plan

- unit tests to add/update
  - `internal/workspace/manager_test.go`
    - `EnsureWorkspace` creates `BOOTSTRAP.md` when directory exists but no core setup files exist.
    - `EnsureWorkspace` does not create `BOOTSTRAP.md` when at least one core setup file already exists.
    - `EnsureWorkspace` does not recreate `BOOTSTRAP.md` after user deletion if core setup files are present.

- package-level focused commands for Red/Green loops
  - `go test ./internal/workspace -run TestEnsureWorkspaceCreatesWorkspaceAndBootstrapFiles`
  - `go test ./internal/workspace -run TestEnsureWorkspaceCreatesBootstrapWhenWorkspaceExistsButCoreFilesMissing`
  - `go test ./internal/workspace -run TestEnsureWorkspaceDoesNotRecreateBootstrapAfterDeletion`

- full validation command(s)
  - `go test ./internal/workspace`
  - `go test ./...`

## Acceptance Criteria

- [ ] `BOOTSTRAP.md` creation is based on workspace initialization state, not only directory creation timing.
- [ ] A pre-existing but uninitialized workspace directory gets `BOOTSTRAP.md`.
- [ ] Deleting `BOOTSTRAP.md` after setup prevents normal re-creation on later startup/ensure calls.
- [ ] Presence of `BOOTSTRAP.md` is the single source of truth for "bootstrap pending".
- [ ] Existing non-overwrite guarantees for workspace files remain intact.
- [ ] Docs and tests are updated to reflect the sentinel lifecycle contract.
