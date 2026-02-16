# Structured Tool Calls Safe Migration Spec

## Status

Draft v1.0

## Problem / Motivation

`localclaw` currently parses provider stream tool events, but that is not yet a complete "host-managed tools" contract for safely enabling `StructuredToolCalls=true` by default.

We need a strict migration contract that guarantees:

- one-run bidirectional tool call/result flow
- explicit ownership separation between provider-native tools and localclaw-managed tools
- runtime routing that executes only localclaw-owned tools
- provider support (Claude and Codex) for custom/local tools, not only built-ins

This spec is the safety contract for flipping structured tool calling on.

## Scope

- Define adapter and runtime contracts for structured tool calls.
- Define ownership/source model for provider-native vs localclaw-managed tools.
- Define pass-through behavior for provider-native calls.
- Define provider capability requirements for Claude and Codex custom tool support.
- Define rollout gates, observability, and rollback for `StructuredToolCalls=true`.

## Out of Scope

- Adding new localclaw business tools beyond existing runtime tools.
- HTTP/gateway/listener runtime in `localclaw`.
- Remote MCP bridging services outside local process/subprocess boundaries.
- Multi-provider fallback orchestration in one request.

## Terminology

- `localclaw_local`: tool defined by `localclaw` and executed by `localclaw` runtime.
- `provider_native`: provider-owned built-in tool executed by provider runtime.
- `delegated_external`: non-localclaw external tool path (policy-gated; not part of default rollout).
- `pass-through`: runtime records/forwards events but does not execute the tool.

## Behavior Contract

### Inputs

- Runtime request (`llm.Request`) with:
  - user input
  - system/skills context
  - localclaw tool definitions
  - session metadata
- Provider capabilities for:
  - structured tool calling
  - host-managed custom tool support
  - provider-native tool inventory support

### Outputs

- Stream events include:
  - assistant deltas/final output
  - structured `tool_call` events with ownership/source classification
  - structured `tool_result` events for every localclaw-executed call
  - provider metadata including provider-native tool inventory (when supported)

### Error Paths

- Missing/invalid tool call payload -> structured error result + continue run.
- Unknown tool source classification -> blocked error result + continue run.
- Tool execution failure -> structured `{ok:false,error:...}` result + continue run.
- Adapter callback/response injection failure -> surfaced error; run cancellation path remains safe.

### Unchanged Behavior

- Fallback prompt-injection path remains available when structured contract is not supported.
- Local-only, single-process, subprocess-only runtime constraints remain unchanged.

## 1) Bidirectional Adapter Contract (Single Run)

For each structured run:

1. Adapter emits `tool_call` with non-empty `call_id`, `name`, parsed args, and `source`.
2. If `source=localclaw_local`, adapter must provide a response path back into the same provider run (`Respond` callback or equivalent).
3. Runtime sends exactly one `tool_result` for each `localclaw_local` call ID.
4. Adapter injects that tool result into the same run before final completion.
5. Final assistant output is emitted only after pending host-managed tool calls resolve or fail.

Contract constraints:

- `call_id` must be stable and unique within a run.
- Duplicate `call_id` values are adapter errors.
- Result `call_id` must match an outstanding host-managed call.
- No cross-run tool result injection is allowed.

## 2) Tool Ownership Separation

Every tool call must carry explicit source/ownership classification:

- `localclaw_local`: present in runtime-provided tool definitions for the run.
- `provider_native`: present in provider metadata inventory (built-ins).
- `delegated_external`: explicit future path only, policy-gated.

Separation rules:

- `/tools` must render provider-native inventory and localclaw inventory as separate sections.
- Runtime policy applies to localclaw and delegated paths.
- Provider-native tools are never executed by `localclaw` runtime.

Collision rule:

- If names collide, runtime keying uses `(source,name)`; source wins over bare name matching.

## 3) Runtime Routing Rules

Routing for structured events:

- `source=localclaw_local`:
  - run local policy checks
  - execute via `ExecuteTool(...)`
  - emit `tool_result`
  - respond to provider in-run
- `source=provider_native`:
  - pass-through only (transcript/UI/telemetry)
  - no local execution, no delegated executor call
- `source=delegated_external`:
  - blocked by default for this migration
  - only enabled later via explicit policy and executor wiring

Safety invariant:

- During this migration, runtime executes only `localclaw_local` tools.

## 4) Provider Capability Requirements (Claude and Codex)

`StructuredToolCalls=true` is allowed only when provider adapter proves:

1. Host-managed custom/local tools can be advertised to provider for a run.
2. Provider can emit host-runnable `tool_call` events for those tools.
3. Adapter can inject `tool_result` responses back into the same run.
4. Provider-native inventory is discoverable for ownership separation in `/tools`.

Required adapter capability flags (or equivalent):

- `StructuredToolCalls`
- `HostManagedTools`
- `ProviderNativeToolInventory`

Provider-specific requirement notes:

- Claude adapter must support calling local custom tools (not only built-ins), using CLI-supported custom tool/MCP mechanisms that remain local-process-safe.
- Codex adapter must support calling local custom tools (not only built-ins), using CLI-supported custom tool/MCP mechanisms that remain local-process-safe.
- If either adapter cannot satisfy host-managed custom tool round-trip, it must keep `StructuredToolCalls=false` and use fallback mode.

## 5) Rollout and Safety Gates

Introduce a guarded rollout switch:

- global flag: `llm.structured_tools.enabled` (default `false`)
- optional per-provider override for canary rollout

Rollout phases:

1. Observe-only: parse and render tool events; no host-managed execution.
2. Canary provider: enable host-managed localclaw tools for one provider behind flag.
3. Dual-provider parity: both Claude and Codex pass contract tests.
4. Default-on: set `StructuredToolCalls=true` only after parity + soak.

Kill switch:

- one config flip disables structured loop and returns to fallback prompt mode.

## 6) Implementation Notes

Primary touch points:

- `internal/llm/contracts.go`
  - extend capabilities/source metadata as needed
- `internal/llm/claudecode/client.go`
  - host-managed call/result injection contract
- `internal/llm/codex/client.go` (when added)
  - same contract for Codex JSON stream
- `internal/runtime/llm_runtime.go`
  - strict routing by tool source
- `internal/runtime/tools.go`
  - execute only localclaw-local tools for this migration
- `internal/tui/app.go`
  - `/tools` ownership-separated rendering and structured event visibility

## 7) Test Plan

Unit/contract tests to add or harden:

- `internal/llm/claudecode/client_test.go`
  - host-managed tool call -> tool result response -> final output in one run
  - provider-native tool event classification
- `internal/llm/codex/client_test.go`
  - same contract coverage for Codex stream parser/injector
- `internal/runtime/tools_test.go`
  - localclaw_local calls execute
  - provider_native calls never execute locally (pass-through only)
  - delegated_external blocked by default
  - exactly-one-result-per-call-id behavior
- `internal/tui/app_test.go`
  - `/tools` prints provider-native and localclaw sections distinctly
  - tool call/result logging preserves source labels

Focused commands:

- `go test ./internal/llm/claudecode -run Test`
- `go test ./internal/llm/codex -run Test`
- `go test ./internal/runtime -run TestPromptStreamStructured`
- `go test ./internal/tui -run TestHandleSlash`

Full validation:

- `go test ./...`
- `go fmt ./...`

## 8) Acceptance Criteria

- [ ] A real bidirectional adapter contract exists for tool calls and tool-result responses within one run.
- [ ] Tool ownership is explicit and separated between provider-native and localclaw-managed tools.
- [ ] Runtime executes only localclaw-managed tools and pass-throughs provider-native tool events.
- [ ] Claude adapter supports custom/local tools (not only built-ins) before enabling `StructuredToolCalls=true`.
- [ ] Codex adapter supports custom/local tools (not only built-ins) before enabling `StructuredToolCalls=true`.
- [ ] `/tools` output clearly separates provider-native inventory from localclaw tools.
- [ ] A kill switch cleanly reverts to fallback prompt mode.

## 9) Rollback / Risk Notes

Primary risks:

- Provider stream schema drift for tool events.
- Misclassification of tool source leading to unsafe execution routing.
- Adapter-level response injection failures causing stuck runs.

Mitigations:

- Contract fixture tests per provider.
- Runtime invariant tests for "local-only execution" and call-id matching.
- Feature-flag rollout with provider-scoped canaries and kill switch.
