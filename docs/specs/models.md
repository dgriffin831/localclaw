# Provider-Driven Model Selection

## Status

Draft

## Problem

`localclaw` currently has a limited model-selection UX:

- `/model <name>` only acts as a Codex model override in the current TUI session.
- there is no `/models` command to show what model choices are actually available.
- provider selection is effectively fixed by `llm.provider` at startup.
- model options are user-typed strings, not validated against provider-reported availability.
- reasoning level cannot be selected as part of a provider/model selector.

This makes model selection brittle across environments where Codex CLI and Claude Code may expose different model sets based on provider-side configuration/auth context.

## Scope

- Add `/models` slash command to list available models grouped by configured provider.
- Redefine `/model` slash command to accept a canonical selector and set active provider/model for the current TUI session.
- Support optional reasoning level in selector form for reasoning-capable providers/models.
- Discover model availability from providers directly (Codex CLI / Claude Code), not from hard-coded model lists.
- Discover reasoning capability/levels from providers when available and surface it in `/models`.
- Add provider config defaults for reasoning where supported (initially Codex).
- Route prompt execution through the selected provider/model/reasoning after `/model` changes.
- Keep provider/model/reasoning state visible in header/footer and `/status`.
- Add tests and docs updates for the new command and selection contract.

## Out of Scope

- Persisting provider/model selections back to config files.
- Adding new providers beyond `codex` and `claudecode`.
- Adding network listeners, gateway modes, or non-local execution paths.
- Reworking memory/session/cron behavior beyond what is required for provider/model routing.

## Behavior Contract

Define expected behavior in concrete terms:
- inputs
  - configured provider blocks under `llm` (`codex`, `claude_code`) with runnable binaries.
  - selector grammar for `/model`: `<provider>/<model>[/<reasoning>]`.
  - examples:
    - `codex/gpt-5.3-codex/medium`
    - `claudecode/claude-opus-4-6`
  - initial support expectation: Codex models may expose reasoning levels; Claude Code models are treated as no-reasoning unless provider metadata later states otherwise.
  - new slash command: `/models` (with optional `/models refresh` to force re-discovery).
  - updated slash command: `/model <selector>`.
  - compatibility shorthand: `/model <model>` keeps current provider and changes only model; reasoning is cleared or defaulted per provider rules.
  - `/model default` or `/model off` resets to configured defaults (`llm.provider` + configured/default model + configured default reasoning where supported).
  - provider config adds default reasoning for reasoning-capable providers (initially `llm.codex.reasoning_default`).
- outputs
  - `/models` prints providers grouped deterministically (sorted by provider name), each with discovered models sorted and deduplicated.
  - each model line includes reasoning capability when available:
    - supports reasoning: shows available levels (if discoverable) and the provider default level.
    - no reasoning support: explicitly marked as `reasoning: n/a`.
  - active selector is clearly marked in canonical form:
    - with reasoning: `codex/gpt-5.3-codex/medium`
    - without reasoning: `claudecode/claude-opus-4-6`
  - discovered model lists come from provider discovery at runtime; no baked-in model-name allowlist is used.
  - `/model` updates active provider/model/reasoning used for subsequent prompts and metadata probes (`/tools`, header display, `/status` effective selector fields).
  - when selector omits reasoning and the selected model supports reasoning, runtime uses provider default reasoning from config.
  - selection is TUI-session-local and is cleared on `/new`, `/reset`, and `/resume` (reverts to configured defaults).
- error paths
  - invalid selector format returns deterministic usage error.
  - `/model` with unknown or unavailable provider returns deterministic error and does not change active selection.
  - when a provider model catalog is available, `/model` rejects models not present in that catalog.
  - when reasoning metadata is available, `/model` rejects unsupported reasoning values for the selected provider/model.
  - providing reasoning for a provider/model that does not support reasoning returns deterministic error and does not change active selection.
  - if reasoning is required for selected provider/model and no reasoning is provided, runtime applies configured default; if default is missing/invalid, startup validation fails.
  - when catalog discovery fails for a provider, `/models` reports discovery failure for that provider but still shows results for others.
  - `/model` against a provider with no discoverable catalog can still set explicit model/reasoning, but response must state it was not provider-validated.
  - selector changes while a run is active are rejected with explicit guidance to abort first.
- unchanged behavior
  - runtime remains single-process and local-only.
  - command modes remain `doctor`, `tui`, `memory`, `mcp`.
  - existing transcript/session storage structure remains compatible.

## Implementation Notes

Call out touched packages/files and key design decisions.

- `internal/llm/contracts.go`
  - extend request options with provider override support.
  - add reasoning override in request options.
  - add provider model-catalog result types that include reasoning metadata per model when available.
- `internal/runtime/app.go`
  - move from single active LLM client to provider-keyed client registry for configured providers.
  - keep configured `llm.provider` as default, with runtime override per request/session.
- `internal/runtime/provider_models.go` (new)
  - add provider-model discovery orchestration with timeout, normalization, and optional cache/refresh behavior.
  - include reasoning capability/level discovery per model when provider exposes it.
  - discovery should use provider adapters directly and avoid hard-coded model names.
- `internal/runtime/provider_tools.go`
  - make metadata discovery selector-aware (uses currently selected provider/model/reasoning, not only configured default provider).
- `internal/llm/codex/client.go`
  - add model-catalog discovery path sourced from Codex CLI runtime environment/configuration.
  - add reasoning-level support in request arg construction and discovery metadata.
  - ensure selected model and reasoning are injected into request args after `/model` change.
- `internal/llm/claudecode/client.go`
  - add model-catalog discovery path sourced from Claude Code runtime environment/configuration.
  - ensure selected model can be applied through provider-supported selection args/options.
  - explicitly report reasoning as unsupported unless provider later exposes support.
- `internal/config/config.go`
  - add `llm.codex.reasoning_default` for Codex default reasoning level.
  - validate default reasoning when provider exposes an allowlist or local validation rules.
- `internal/tui/model.go`, `internal/tui/slash.go`
  - add `/models`; update `/model` parsing for selector grammar.
  - replace simple model override state with selector state (`provider`, `model`, optional `reasoning`).
  - keep status/footer/header rendering in sync with active provider/model/reasoning state.
- docs updates:
  - `README.md`
  - `docs/TUI.md`
  - `docs/CONFIGURATION.md` (new default reasoning fields and selector semantics).

Key design decisions:

- Provider model discovery is adapter-owned with a fallback chain:
  - provider-native machine-readable model listing when available
  - provider-native reasoning metadata (per model) when available
  - provider metadata events when available
  - constrained JSON probe prompt in an isolated probe session (models and, when possible, reasoning levels)
  - final fallback to currently active/configured provider model and provider default reasoning only (flagged as partial discovery)
- no static model allowlist is introduced in `localclaw`.
- selector state (`provider/model[/reasoning]`) remains local runtime state, not config mutation.
- default reasoning comes from provider config, not from hard-coded runtime defaults.

## Test Plan

- unit tests to add/update
  - `internal/tui/app_test.go`
    - `/models` output grouping/marking behavior.
    - `/model <provider>/<model>[/<reasoning>]` changes active selector.
    - shorthand `/model <model>` behavior on current provider.
    - `/model` validation behavior for unknown provider/model/reasoning and discovery-unavailable paths.
    - `/new`, `/reset`, `/resume` clear selection to defaults.
  - `internal/runtime`
    - prompt routing uses selected provider client.
    - provider-aware discovery returns grouped model catalogs with reasoning metadata.
    - provider discovery failure is isolated (one provider failing does not break others).
    - omitted reasoning on reasoning-capable model applies configured provider default.
  - `internal/llm/codex`
    - model catalog extraction/normalization from provider output.
    - reasoning-capability extraction/normalization from provider output.
    - selected model/reasoning argument application for requests.
  - `internal/llm/claudecode`
    - model catalog extraction/normalization from provider output.
    - reasoning unsupported behavior in catalog + selection flow.
    - selected model argument application for requests.
  - `internal/config` (only if config schema changes)
    - validation/default coverage for provider default reasoning fields.
- package-level focused commands for Red/Green loops
  - `go test ./internal/tui`
  - `go test ./internal/runtime`
  - `go test ./internal/llm/codex`
  - `go test ./internal/llm/claudecode`
  - `go test ./internal/config` (if changed)
- full validation command(s)
  - `go test ./...`

## Acceptance Criteria

- [ ] `/models` lists discovered models grouped by provider with active selection markers.
- [ ] `/models` shows per-model reasoning capability (`levels` when available, otherwise `reasoning: n/a`).
- [ ] `/model <provider>/<model>[/<reasoning>]` sets active selector for subsequent prompts.
- [ ] `/model <model>` compatibility shorthand keeps current provider and updates model using provider default reasoning rules.
- [ ] Canonical selector rendering follows the requested approach (`codex/gpt-5.3-codex/medium`, `claudecode/claude-opus-4-6`).
- [ ] Model lists are discovered from provider/runtime environment, with no hard-coded model catalog.
- [ ] Reasoning defaults are configurable per reasoning-capable provider (initially `llm.codex.reasoning_default`) and are applied when reasoning is omitted.
- [ ] Model validation is enforced when provider catalog exists.
- [ ] Reasoning validation is enforced when reasoning metadata is available.
- [ ] Discovery/validation failures are explicit and non-destructive to existing active selection.
- [ ] `/status`, header, and footer reflect active provider/model/reasoning accurately.
- [ ] Documentation and tests are updated to the new command behavior.

## Rollback / Risk Notes

Describe fallback or rollback strategy if needed.

- Primary risks:
  - provider discovery output drift (schema/text changes) causing missing catalogs.
  - provider discovery output drift causing missing/inaccurate reasoning capability metadata.
  - added complexity from multi-provider runtime routing.
  - slower slash-command UX if discovery blocks on provider CLI latency.
- Mitigations:
  - adapter-level parsing with normalization + tolerant fallback chain.
  - per-provider discovery isolation and partial results.
  - cached discovery results with explicit `/models refresh`.
- Rollback:
  - revert to configured-provider-only routing.
  - keep `/model` as provider-local model override behavior without reasoning selector.
  - disable catalog validation and allow explicit manual model string while preserving existing prompt execution.
