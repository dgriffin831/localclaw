# Security Modes and Workspace SECURITY.md

## Status

Draft

## V1 Build Policy

`localclaw` is still pre-v1. Favor clean implementations only:
- no rollback plans
- no fallback execution paths unless explicitly requested
- no legacy compatibility shims

## Problem

`localclaw` currently expresses execution-safety behavior through provider-specific `extra_args`, which creates inconsistent and fragile security posture:
- operators must know Codex and Claude flags to get equivalent behavior
- there is no single config contract for "how permissive should agent execution be"
- workspace-path sandbox allowances are not standardized by policy

Separately, workspace bootstrap content does not include a dedicated `SECURITY.md` baseline file. Safety guardrails exist in multiple places but are not centralized as explicit, durable workspace policy content.

## Scope

- Add a top-level `security` configuration section to `localclaw.json`.
- Add `security.mode` with three allowed values:
  - `full-access`
  - `sandbox-write`
  - `read-only`
- Define deterministic translation from `security.mode` to provider CLI flags for Codex and Claude Code.
- Ensure the resolved workspace path for the active agent/session is explicitly allowed in sandboxed modes.
- Add `SECURITY.md` to workspace templates.
- Include `SECURITY.md` in default bootstrap/system context injection.
- Add/update docs and tests for the new config and bootstrap behavior.

## Out of Scope

- Adding new execution modes beyond the three listed in this spec.
- Introducing remote gateway/server/listener runtime behavior.
- Replacing existing local-only trust boundaries.
- Secret-management vault features, encryption-at-rest changes, or DLP scanners.
- Migration shims for removed legacy `security.*` keys.

## Behavior Contract

Define expected behavior in concrete terms:
- inputs
  - `localclaw.json` includes:
    - `security.mode` (string)
  - accepted values:
    - `full-access`
    - `sandbox-write`
    - `read-only`
  - default value from `config.Default()`:
    - `sandbox-write`
  - active workspace path comes from resolved workspace for the active agent/session (not raw unresolved config text).
- outputs
  - mode-to-provider flag translation:
    - `full-access`
      - Codex: `--dangerously-bypass-approvals-and-sandbox` (behavior equivalent to `--yolo`)
      - Claude Code: `--dangerously-skip-permissions`
    - `sandbox-write`
      - Codex: `--sandbox workspace-write`
      - Claude Code: `--permission-mode acceptEdits`
      - both providers: include `--add-dir <resolved-workspace-path>`
    - `read-only`
      - Codex: `--sandbox read-only`
      - Claude Code: `--permission-mode plan`
      - both providers: include `--add-dir <resolved-workspace-path>`
  - `security.mode` is authoritative. Provider `extra_args` must not silently override mode-controlled security flags.
  - workspace bootstrap adds `SECURITY.md` when missing (same write-if-missing behavior as existing templates).
  - bootstrap/system context injection includes `SECURITY.md` by default.
  - subagent bootstrap context also includes `SECURITY.md` so guardrails are not dropped in delegated/subagent runs.
  - baseline `SECURITY.md` content includes clear guardrails such as:
    - never delete data without explicit user approval
    - never read secret-bearing files (for example `.env`) without explicit user approval
    - never exfiltrate local data externally unless explicitly authorized by the user
    - ask before irreversible or external side-effect actions
- error paths
  - invalid `security.mode` fails config validation with actionable error text.
  - conflicting security-managed flags in provider `extra_args` fail config validation (or are deterministically normalized with clear documented precedence; implementation should choose one behavior and test it).
  - unresolved workspace path for security flag derivation fails request execution with wrapped context.
  - unknown config keys under `security` continue to fail strict decoding.
- unchanged behavior
  - runtime remains single-process, local-only, and subprocess-based for providers.
  - no HTTP/gRPC/gateway/listener surface is introduced.
  - existing command modes (`doctor`, `tui`, `memory`, `channels`, `mcp`) remain intact.

## Implementation Notes

Call out touched packages/files and key design decisions.
- `internal/config/config.go`
  - add `SecurityConfig` and top-level `Config.Security`.
  - add default `security.mode = sandbox-write`.
  - validate allowlist for mode values.
  - enforce deterministic handling for conflicts between mode-managed security flags and provider `extra_args`.
- `internal/runtime/tools.go` and/or `internal/runtime/app.go`
  - ensure request construction has access to resolved workspace path for the active session.
- `internal/llm/contracts.go`
  - if needed, extend request/session metadata to carry resolved workspace path to provider clients.
- `internal/llm/codex/client.go`
  - apply mode-derived Codex flags and workspace `--add-dir` wiring.
- `internal/llm/claudecode/client.go`
  - apply mode-derived Claude flags and workspace `--add-dir` wiring.
- `internal/workspace/templates/SECURITY.md` (new)
  - add default workspace security guardrails content.
- `internal/workspace/manager.go`
  - include `SECURITY.md` in template creation order.
  - include `SECURITY.md` in bootstrap prompt order.
  - include `SECURITY.md` in subagent bootstrap allowlist.
- docs updates
  - `docs/CONFIGURATION.md`
  - `docs/SECURITY.md`
  - `docs/RUNTIME.md`
  - `docs/ARCHITECTURE.md`
  - any provider docs that describe `extra_args`-driven security behavior (`docs/CLAUDE_CODE.md` and Codex references).

Key design decisions:
- Security posture is provider-agnostic at config surface (`security.mode`) and provider-specific at execution layer (flag translation).
- Workspace path allowlisting is always derived from resolved runtime workspace path to avoid mismatches with relative/unexpanded config values.
- `SECURITY.md` is part of default injected bootstrap context so safety guardrails are explicit and persistent in agent context.

## Test Plan

- unit tests to add/update
  - `internal/config/config_test.go`
    - accepts `security.mode` values: `full-access`, `sandbox-write`, `read-only`
    - rejects invalid mode values
    - verifies default mode is `sandbox-write`
    - verifies strict rejection for unknown `security` keys
    - verifies deterministic handling for conflicting provider `extra_args`
  - `internal/runtime/local_only_test.go`
    - verifies generated provider args include expected mode-derived security flags
    - verifies resolved workspace path is included via `--add-dir` where required
  - `internal/llm/codex/client_test.go`
    - validates mode-to-flag translation and arg ordering/precedence
  - `internal/llm/claudecode/client_test.go`
    - validates mode-to-flag translation and arg ordering/precedence
  - `internal/workspace/manager_test.go`
    - `SECURITY.md` is created during workspace bootstrap when missing
    - `SECURITY.md` is included in prompt bootstrap files
    - subagent bootstrap allowlist includes `SECURITY.md`
- package-level focused commands for Red/Green loops
  - `go test ./internal/config`
  - `go test ./internal/runtime`
  - `go test ./internal/llm/codex`
  - `go test ./internal/llm/claudecode`
  - `go test ./internal/workspace`
- full validation command(s)
  - `go test ./...`

## Acceptance Criteria

- [ ] `localclaw.json` supports top-level `security.mode` with values `full-access`, `sandbox-write`, and `read-only`.
- [ ] Default config sets `security.mode` to `sandbox-write`.
- [ ] Runtime translates each mode into deterministic Codex and Claude CLI flags as defined in this spec.
- [ ] Resolved workspace path is explicitly allowlisted for sandboxed/read-only execution flows.
- [ ] Provider `extra_args` cannot silently defeat configured `security.mode`.
- [ ] Workspace templates include `SECURITY.md`, and bootstrap/system context injection includes it by default (including subagent sessions).
- [ ] Security-related docs are updated to reflect config contract and runtime behavior.
- [ ] Full test suite passes (`go test ./...`) for implementation work based on this spec.
