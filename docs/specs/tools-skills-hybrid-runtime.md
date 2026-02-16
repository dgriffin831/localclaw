# Tools + Skills Hybrid Runtime Spec (MCP-First)

## Status

Draft v3.1 (phase-5 implementation-spec refinement)

## Problem / Motivation

`localclaw` currently has partial host-managed structured tool-call plumbing, but the product direction is to extend existing provider runtimes instead of replacing them.

For Claude Code and Codex, the highest-leverage architecture is:

- keep provider inner loops as the primary reasoning/tool-selection loop
- expose localclaw-owned capabilities through a local MCP tool server
- preserve local-only policy and strong ownership boundaries

Re-owning full tool orchestration inside localclaw for these providers introduces unnecessary drift and maintenance cost.

## Decision Summary

Adopt an MCP-first hybrid runtime:

1. **Provider inner loop is primary** (Claude/Codex decides when and how to call tools).
2. **`localclaw` runs as a stdio MCP server** for localclaw-managed capabilities.
3. **Tool ownership is explicit and enforced**:
   - `provider_native`
   - `localclaw_mcp`
4. Runtime/TUI/telemetry must preserve this ownership split end-to-end.
5. Migration is a hard cutover: no legacy runtime fallback path is retained.

## Scope

- MCP-first runtime contract for Claude and Codex integrations.
- `localclaw mcp serve` command mode for stdio MCP serving.
- Localclaw MCP tools for memory/workspace/cron/orchestration.
- Provider wiring expectations:
  - Claude: MCP flags + `--append-system-prompt`
  - Codex: MCP config file path contract
- Config/schema updates required to support MCP runtime.
- Package/file touch points and rollout/test plan.

## Out of Scope

- Rebuilding a provider-independent inner agent loop for Claude/Codex.
- Executing provider-native tools inside localclaw.
- HTTP/gateway/listener transport for MCP serving in v1.
- Remote MCP bridging or multi-hop tool execution.

## Constraints

- Go `1.17`.
- Single-process local CLI architecture remains intact.
- Local-only security guardrails remain mandatory.
- MCP transport is stdio only in v1.

## Terminology

- `provider_native`: built-in provider tools (for example shell/web tools surfaced by provider runtime).
- `localclaw_mcp`: tools served by `localclaw mcp serve` and executed by localclaw handlers.
- **Provider inner loop**: provider-owned turn continuation after tool calls/results.
- **Hybrid runtime**: provider-managed loop + localclaw-managed state/policy/tool services.

## Behavior Contract

### Inputs

- Config:
  - provider selection (`claudecode` or `codex`)
  - MCP runtime config (`llm.mcp.*`, provider MCP wiring blocks)
  - existing tool/skill policies
- Prompt input plus resolved `agent/session/workspace` context.
- Local state roots (workspace, memory SQLite, sessions, cron state).

### Outputs

- Provider output stream (delta/final/metadata) remains primary response channel.
- MCP tool invocations execute localclaw handlers and return JSON-RPC tool results to provider.
- `/tools` and runtime telemetry show separate inventories for `provider_native` and `localclaw_mcp`.
- Session/transcript/memory side effects are persisted by existing localclaw modules.

### Error Paths

- MCP server start failure: run fails with provider-specific context.
- Provider MCP wiring invalid/missing: explicit startup/run error (hard fail).
- Tool policy denial: structured tool error returned to provider; run continues unless provider aborts.
- Handler validation/runtime failure: structured `{ok:false,error:...}` tool result; run continues.

### Unchanged Behavior

- Local-only enforcement remains required.
- Startup order in `App.Run` remains deterministic.
- Existing non-MCP modes (`check`, `tui`, `memory`) remain available.

## Architecture

### Ownership Split (Normative)

| Class | Owner | Executed By | Policy Authority |
| --- | --- | --- | --- |
| `provider_native` | Provider runtime | Provider runtime only | Provider + localclaw visibility only |
| `localclaw_mcp` | localclaw | localclaw MCP handlers | localclaw policy and schema validation |

Hard rules:

- localclaw never executes `provider_native` tools.
- provider never directly executes `localclaw_mcp` capabilities except through MCP calls.
- all tool events and logs include ownership class.

### Runtime Topology

For each run:

1. Resolve `agent/session/workspace` context.
2. Build provider wiring (MCP config + system guidance).
3. Start provider subprocess (non-interactive mode).
4. Provider runs inner loop and calls localclaw MCP tools as needed.
5. localclaw serves tool calls over stdio JSON-RPC.
6. localclaw captures provider stream + MCP telemetry and persists side effects.

### Skills in Hybrid Runtime

- Provider-native skills/tooling remain provider-owned.
- localclaw workspace skill snapshots remain localclaw-authored context.
- localclaw does not reimplement provider skill routing; it only supplies concise policy/tool guidance.

## `localclaw mcp serve` Command Mode

### Command Contract

Add command mode:

- `localclaw mcp serve`

Initial behavior:

- transport: stdio JSON-RPC only
- no HTTP bind/listen mode
- server lifecycle tied to stdio session
- tool handlers call existing internal modules (`memory`, `workspace`, `cron`, `session`, `runtime` policy helpers)

### Tool Surface (v1)

Memory:

- `localclaw_memory_search`
- `localclaw_memory_get`

Workspace:

- `localclaw_workspace_status`
- `localclaw_workspace_bootstrap_context`

Cron:

- `localclaw_cron_list`
- `localclaw_cron_add`
- `localclaw_cron_remove`
- `localclaw_cron_run`

Orchestration:

- `localclaw_sessions_list`
- `localclaw_sessions_history`
- `localclaw_sessions_send`
- `localclaw_session_status`

Naming rule:

- exported MCP tool names are `localclaw_*` prefixed to avoid collision with provider-native names.

## Provider Wiring Expectations

### Claude Code Wiring (Required Contract)

Localclaw must prepare a run-scoped MCP config JSON and launch Claude with explicit MCP flags.

Expected invocation shape:

```bash
claude -p "<prompt>" \
  --output-format stream-json \
  --verbose \
  --mcp-config "<run_scoped_mcp_config.json>" \
  --strict-mcp-config \
  --append-system-prompt "<localclaw_guidance>"
```

Minimum required flags for MCP-first mode:

- `--mcp-config`
- `--strict-mcp-config`
- `--append-system-prompt`

Example MCP config payload shape:

```json
{
  "mcpServers": {
    "localclaw": {
      "type": "stdio",
      "command": "localclaw",
      "args": ["mcp", "serve"],
      "env": {}
    }
  }
}
```

Notes:

- use run-scoped config files; avoid mutating user/global Claude settings.
- keep `--append-system-prompt` concise and tool-policy focused.

### Codex Wiring (Required Contract)

Codex does not expose a direct `--mcp-config` execution flag; MCP servers are read from config TOML.

Config path contract:

1. `llm.codex.mcp.config_path` when explicitly set.
2. `$CODEX_HOME/config.toml` when `CODEX_HOME` is set.
3. `~/.codex/config.toml` default path when no explicit override is set.

Localclaw default strategy:

- prefer isolated `CODEX_HOME` under localclaw state root (run/profile scoped).
- write/merge `mcp_servers.localclaw` entry in the effective config file.
- avoid unsafe mutation of user-global config unless explicitly configured.

Required MCP server entry shape:

```toml
[mcp_servers.localclaw]
command = "localclaw"
args = ["mcp", "serve"]
```

Expected non-interactive run shape:

```bash
codex exec --json -C "<workspace>" "<prompt>"
```

Optional flags (`-p`, `-m`, `-c`) continue to apply. MCP activation is driven by effective config path content.

## Prompting Strategy

System guidance remains localclaw-authored and concise:

- use `localclaw_memory_search` before finalizing responses when recall is needed
- use `localclaw_memory_get` for exact snippets/lines
- use localclaw tools for workspace/cron/session operations
- explicitly report when localclaw memory data is unavailable

Injection path:

- Claude: `--append-system-prompt`
- Codex: existing request/system-context composition in adapter/runtime

## Config / Schema Changes

### New MCP-Oriented Config Blocks

Extend `llm` config with MCP runtime settings:

```json
{
  "llm": {
    "provider": "claudecode",
    "mcp": {
      "transport": "stdio",
      "server": {
        "binary_path": "localclaw",
        "args": ["mcp", "serve"],
        "env": {},
        "startup_timeout_seconds": 10
      },
      "tools": {
        "allow": ["*"],
        "deny": []
      }
    },
    "claude_code": {
      "binary_path": "claude",
      "mcp": {
        "strict_config": true
      }
    },
    "codex": {
      "binary_path": "codex",
      "mcp": {
        "config_path": "",
        "use_isolated_home": true,
        "home_path": "",
        "server_name": "localclaw"
      }
    }
  }
}
```

### Validation Rules

- `llm.mcp.transport` allowlist: `stdio` only.
- `llm.mcp.server.binary_path` is required.
- `llm.mcp.server.args` must include command path to `mcp serve`.
- `llm.mcp.tools.allow/deny` reject blank and duplicate entries.
- provider-specific MCP blocks validate required fields for active provider wiring.
- existing local-only policy validation remains unchanged and mandatory.

### Policy Precedence

For `localclaw_mcp` tools:

1. global MCP tool policy (`llm.mcp.tools`)
2. existing runtime tool policy (`tools.*`, `agents.*.tools.*`)
3. deny overrides allow

`provider_native` tools are never re-routed into localclaw policy execution.

## Runtime / Package / File Touch Points

### Entrypoint and Command Wiring

- `cmd/localclaw/main.go`
  - add `mcp` mode
  - add `serve` subcommand dispatch
  - update unknown-command text to include `mcp`

### New MCP Package Surface

- `internal/mcp/server.go` (server lifecycle + stdio JSON-RPC loop)
- `internal/mcp/protocol/*.go` (MCP request/response/tool schemas)
- `internal/mcp/tools/memory.go`
- `internal/mcp/tools/workspace.go`
- `internal/mcp/tools/cron.go`
- `internal/mcp/tools/orchestration.go`
- `internal/mcp/tools/policy.go` (allow/deny + validation gate)

### Runtime Integration

- `internal/runtime/app.go`
  - add MCP runtime wiring helpers
  - expose service handles needed by MCP handlers
- `internal/runtime/tools.go`
  - maintain local policy logic reused by MCP handlers
- `internal/runtime/*_test.go`
  - ownership classification + hard-fail behavior for MCP wiring issues

### LLM Adapters

- `internal/llm/claudecode/client.go`
  - add Claude MCP flag wiring (`--mcp-config`, `--strict-mcp-config`)
  - preserve `--append-system-prompt`
- `internal/llm/codex/client.go` (new)
  - resolve effective Codex MCP config path
  - ensure/validate `mcp_servers.localclaw` entry
  - run non-interactive `codex exec` with JSON stream parsing

### Config and CLI Helpers

- `internal/config/config.go` + tests
  - add MCP schema/default/validation
- `internal/cli/*` (if command helper extraction is used for `mcp` mode)

### TUI and Observability

- `internal/tui/app.go`
  - `/tools` shows `provider_native` and `localclaw_mcp` sections separately
  - tool activity cards show ownership source labels

## Implementation Plan (5 Phases, TDD-First)

### Phase Checkpoints (Incremental + Testable)

| Phase | Primary Outcome | Minimum Test Gate |
| --- | --- | --- |
| 1 | MCP server skeleton + memory tools | `go test ./internal/mcp -run Test` |
| 2 | Claude MCP-first wiring | `go test ./internal/llm/claudecode -run Test` |
| 3 | Codex MCP config-path + adapter wiring | `go test ./internal/llm/codex -run Test` |
| 4 | Full v1 MCP tool surface | `go test ./internal/mcp -run Test` |
| 5 | TUI/telemetry ownership split + hard cutover | `go test ./internal/tui -run TestHandleSlash && go test ./...` |

### Phase 1: MCP Runtime Skeleton + Memory Tools

Objective:
- Stand up a production-safe stdio MCP server path and deliver the first usable `localclaw_mcp` tools (`memory_search`, `memory_get`).

Primary implementation tasks:
- Add `mcp` command mode and `serve` subcommand dispatch in `cmd/localclaw/main.go`.
- Create MCP server lifecycle scaffolding in `internal/mcp/server.go`:
  - stdio JSON-RPC read/write loop
  - graceful shutdown on stdin EOF/context cancel
  - structured error responses for malformed requests
- Define protocol structs in `internal/mcp/protocol/*.go` for:
  - initialize/list tools/call tool requests
  - tool result and error payloads
- Implement tool policy guard in `internal/mcp/tools/policy.go`:
  - allow/deny evaluation
  - duplicate/blank entry hard-fail checks
  - deny-overrides-allow behavior
- Implement memory tools in `internal/mcp/tools/memory.go`:
  - `localclaw_memory_search`
  - `localclaw_memory_get`
  - strict argument validation and normalized error payloads
- Extend runtime wiring (`internal/runtime/app.go`) to provide memory/session/config handles required by MCP handlers.

TDD gates (Red -> Green):
- Add failing tests for command routing (`mcp serve` recognized; bad subcommand rejected).
- Add failing tests for policy precedence and tool denial behavior.
- Add failing handler tests for missing/invalid args and storage errors.
- Implement minimal code to pass targeted tests, then broaden coverage.

Validation commands:
- `go test ./internal/mcp -run Test`
- `go test ./internal/runtime -run Test`
- `go test ./internal/config -run TestValidate`

Phase exit criteria:
- `localclaw mcp serve` starts a stdio MCP loop.
- Memory tools are discoverable and callable via MCP protocol tests.
- Policy gate is enforced for all MCP tool calls.

### Phase 2: Claude Code MCP Integration

Objective:
- Make Claude runs MCP-first by default with strict, run-scoped MCP wiring.

Primary implementation tasks:
- Add run-scoped MCP config file generation (runtime-owned temp/state path).
- Update `internal/llm/claudecode/client.go` invocation builder to include:
  - `--mcp-config <path>`
  - `--strict-mcp-config` when configured
  - `--append-system-prompt <guidance>`
- Ensure startup hard-fails when:
  - MCP config cannot be written
  - strict MCP flag is required but unsupported/invalid
  - configured MCP server binary/args are invalid
- Preserve stream parsing and stderr-rich error propagation.
- Keep guidance prompt concise and tool-policy focused.

TDD gates (Red -> Green):
- Add failing tests verifying composed Claude args include required MCP flags.
- Add failing tests for strict-config enabled/disabled behavior.
- Add failing tests for run-scoped config file generation and cleanup behavior.
- Add failing tests for hard-fail error messages with provider-specific context.

Validation commands:
- `go test ./internal/llm/claudecode -run Test`
- `go test ./internal/runtime -run TestPromptStream`

Phase exit criteria:
- Claude adapter always emits valid MCP-first invocation for configured runs.
- Missing/invalid MCP wiring fails fast with actionable errors.
- Existing stream behavior remains unchanged for successful runs.

### Phase 3: Codex MCP Integration + Config Path Strategy

Objective:
- Add a Codex adapter that reliably enables localclaw MCP via config TOML without unsafe global mutation.

Primary implementation tasks:
- Introduce `internal/llm/codex/client.go` with non-interactive `codex exec --json` integration.
- Implement effective config path resolution order:
  1. `llm.codex.mcp.config_path`
  2. `$CODEX_HOME/config.toml`
  3. `~/.codex/config.toml`
- Implement isolated-home default path strategy when configured:
  - create/use run/profile-scoped `CODEX_HOME`
  - write/merge `mcp_servers.localclaw` deterministically
- Add safe TOML read/merge/write behavior:
  - preserve unrelated existing settings
  - normalize localclaw server entry (`command`, `args`)
  - fail on malformed or non-writable config
- Wire adapter selection in runtime by `llm.provider=codex`.

TDD gates (Red -> Green):
- Add failing tests for each config path precedence branch.
- Add failing tests proving deterministic merge behavior for existing TOML.
- Add failing tests for malformed TOML, permission errors, and missing binary.
- Add failing tests for expected Codex command shape and JSON stream handling.

Validation commands:
- `go test ./internal/llm/codex -run Test`
- `go test ./internal/runtime -run Test`
- `go test ./internal/config -run TestValidate`

Phase exit criteria:
- Codex runs with the expected effective config path and MCP server entry.
- Isolated home strategy works end-to-end and is default-safe.
- No unintentional mutation of unrelated global user config in default flow.

### Phase 4: Complete v1 MCP Tool Surface (Workspace/Cron/Orchestration)

Objective:
- Deliver the rest of the v1 `localclaw_mcp` tools with strict safety and validation.

Primary implementation tasks:
- Implement workspace tools in `internal/mcp/tools/workspace.go`:
  - `localclaw_workspace_status`
  - `localclaw_workspace_bootstrap_context`
- Implement cron tools in `internal/mcp/tools/cron.go`:
  - `localclaw_cron_list`
  - `localclaw_cron_add`
  - `localclaw_cron_remove`
  - `localclaw_cron_run`
- Implement orchestration/session tools in `internal/mcp/tools/orchestration.go`:
  - `localclaw_sessions_list`
  - `localclaw_sessions_history`
  - `localclaw_sessions_send`
  - `localclaw_session_status`
- Reuse existing runtime/service modules; do not duplicate business logic.
- Apply strict guards:
  - path traversal and workspace boundary checks
  - cron schedule sanity checks
  - session ownership/existence checks
  - bounded payload sizes and pagination defaults

TDD gates (Red -> Green):
- Add failing tests per tool for valid call, validation failure, policy denial, and backend error mapping.
- Add failing tests for path/schedule/session security constraints.
- Add integration-style MCP tests for tool discovery and call dispatch by name.

Validation commands:
- `go test ./internal/mcp -run Test`
- `go test ./internal/workspace -run Test`
- `go test ./internal/session -run Test`
- `go test ./internal/cron -run Test`

Phase exit criteria:
- All v1 tool names are exposed and callable through MCP.
- Every tool enforces policy and schema validation consistently.
- Security guardrails are covered by dedicated tests.

### Phase 5: TUI/Telemetry Split + Hard Cutover Finalization

Objective:
- Make ownership boundaries visible in UX/telemetry and complete the MCP-first hard cutover.

Primary implementation tasks:
- Update `internal/tui/app.go` `/tools` view to render separate sections:
  - `provider_native`
  - `localclaw_mcp`
- Ensure tool activity/status UI includes ownership source labels.
- Update runtime/tool-event telemetry schema to carry ownership class on every event.
- Remove/retire legacy fallback paths for Claude/Codex runtime execution.
- Align docs and operator guidance:
  - `README.md`
  - `docs/TUI.md`
  - `docs/CONFIGURATION.md`
  - `docs/RUNTIME.md`
  - any superseded spec references

TDD gates (Red -> Green):
- Add failing TUI tests for `/tools` rendering split and ownership labels.
- Add failing runtime tests that assert no legacy fallback path is invoked.
- Add failing tests for telemetry event payload ownership fields.

Validation commands:
- `go test ./internal/tui -run TestHandleSlash`
- `go test ./internal/runtime -run Test`
- `go test ./...`
- `go fmt ./...`

Phase exit criteria:
- Ownership split is explicit and test-covered in TUI and telemetry.
- Claude/Codex runtime path is MCP-first only (no fallback retained).
- Full suite passes and documentation reflects final behavior.

## Assumptions

1. Provider CLIs support required MCP wiring surfaces described in this spec (Claude flags and Codex TOML MCP server definitions).
2. `localclaw` binary is resolvable for subprocess invocation in operator environments (PATH or explicit configured binary path).
3. Existing modules (`memory`, `workspace`, `cron`, `session`) expose stable service boundaries reusable by MCP handlers without redesign.
4. TUI and telemetry consumers can absorb additive ownership fields without backward-compatibility breakage.
5. v1 adoption prioritizes a hard cutover over dual-runtime support for Claude/Codex.

## Open Questions

1. Should high-risk mutating orchestration tools default to deny unless explicitly allowed by config?
2. For Codex, should isolated `CODEX_HOME` be mandatory default for all runs or only recommended/defaulted with opt-out?
3. What exact telemetry retention/schema migration strategy is required for ownership-field additions in phase 5?
4. Do we need a dedicated startup capability probe in `check` mode for provider MCP compatibility, or is runtime fail-fast sufficient?
5. Should `localclaw mcp serve` expose version/capability metadata for future multi-version compatibility?

## Test Plan

Unit and focused commands:

- `go test ./internal/mcp -run Test`
- `go test ./internal/llm/claudecode -run Test`
- `go test ./internal/llm/codex -run Test`
- `go test ./internal/runtime -run TestPromptStream`
- `go test ./internal/tui -run TestHandleSlash`
- `go test ./internal/config -run TestValidate`

Full validation:

- `go test ./...`
- `go fmt ./...`

Manual smoke:

- `go run ./cmd/localclaw mcp serve`
- Claude non-interactive run with `--mcp-config --strict-mcp-config --append-system-prompt`
- Codex non-interactive run using effective MCP config path

## Acceptance Criteria

- [ ] `localclaw mcp serve` provides stdio MCP serving for v1 tool set.
- [ ] Claude MCP wiring uses explicit MCP flags and append-system guidance.
- [ ] Codex MCP wiring resolves and uses the correct config path contract.
- [ ] Ownership split (`provider_native` vs `localclaw_mcp`) is explicit in runtime and TUI.
- [ ] localclaw policy enforcement applies to all `localclaw_mcp` tools.
- [ ] No listener/gateway/server transport is introduced.
- [ ] No fallback or legacy execution path exists for Claude/Codex runtime execution.

## Risks / Mitigations

Primary risks:

- provider CLI MCP flag/config drift
- accidental mutation of user-global Codex config
- ownership misclassification in tool event handling
- over-broad tool exposure

Mitigations:

- capability probes in `check` mode
- isolated/run-scoped config generation by default
- strict ownership-source assertions in tests
- default-deny policy for high-risk orchestration operations

## Relationship to Other Specs

- `docs/specs/openai-codex-model-support.md`: provider selection and Codex adapter baseline.
- `docs/specs/structured-tool-calls-safe-migration.md`: superseded by this MCP-first hard-cutover architecture for Claude/Codex extension mode.
