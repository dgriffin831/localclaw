# Testing Guide

This document describes test coverage, commands, and Red/Green workflow for `localclaw`.

## Goals

- Catch regressions in local-only policy and startup behavior.
- Keep config contract and runtime wiring aligned.
- Keep workspace/session/memory behavior stable under edge and concurrency scenarios.
- Keep TUI command/input behavior stable.

## Coverage map

| Layer | Primary Locations | What It Validates |
| --- | --- | --- |
| Config and policy | `internal/config/config_test.go` | defaults, strict config parsing, validation allowlists, local-only guardrails |
| Runtime lifecycle and hooks | `internal/runtime/local_only_test.go`, `internal/runtime/tools_test.go`, `internal/runtime/cron_runtime_test.go` | startup boundaries, request-path prompt assembly, reset/session behavior, cron runtime session-target mapping |
| LLM contract/adapter compatibility | `internal/runtime/tools_test.go`, `internal/llm/claudecode/client_test.go`, `internal/llm/codex/client_test.go` | runtime provider contract behavior plus Claude Code/Codex subprocess request and stream handling |
| Skills loader/snapshot prompts | `internal/skills/registry_test.go`, `internal/runtime/tools_test.go` | workspace skill discovery, eligibility filtering, prompt-block injection/cache refresh |
| TUI behavior | `internal/tui/app_test.go` | slash commands, autocomplete, waiting/status UX, welcome rendering, history/keybindings |
| Workspace lifecycle | `internal/workspace/manager_test.go` | workspace resolution/bootstrap, `BOOTSTRAP.md` sentinel lifecycle, bootstrap loading/filtering, subagent allowlist |
| Session store/transcripts | `internal/session/store_test.go` | path resolution, lock behavior, metadata preservation, write safety |
| Cron scheduler | `internal/cron/scheduler_test.go` | schedule validation, recurring execution, persistence/reload, manual run/remove/error paths |
| Cron runtime executor | `internal/runtime/cron_runtime_test.go` | cron entry execution mapping (`default` vs `isolated`) and skipped invalid targets |
| Heartbeat monitor | `internal/heartbeat/monitor_test.go` | enabled/disabled start, interval ticks, cancellation lifecycle, overlap guard, non-fatal errors |
| Backup lifecycle | `internal/backup/manager_test.go`, `internal/cli/backup_test.go` | archive creation, retention cleanup, overlap guards, manual `backup` command behavior |
| Channel adapters | `internal/channels/slack/adapter_test.go`, `internal/channels/signal/adapter_test.go` | outbound delivery behavior, timeout/cancellation, and failure-path error mapping |
| Signal inbound receive/runtime | `internal/channels/signal/receive_test.go`, `internal/runtime/channels_inbound_test.go` | `signal-cli receive` parsing, allowlist enforcement, group-message denial, sender-to-agent routing |
| Memory CLI | `internal/cli/memory_test.go` | `memory status/index/search/grep` JSON/text output and argument handling |
| Channels CLI | `internal/cli/channels_test.go` | `channels serve` subcommand parsing and command-mode validation |
| MCP runtime/tools | `internal/mcp/server_test.go`, `internal/mcp/tools_test.go`, `internal/mcp/tools/*_test.go`, `internal/cli/mcp_test.go` | stdio JSON-RPC loop, tool discovery/calls, tool policy behavior, `mcp serve` routing |
| Session snapshot hook | `internal/hooks/session_memory_test.go` | snapshot generation, slug/summary fallback, transcript handling |
| Memory indexing/search/grep/flush | `internal/memory/*_test.go` | discovery/chunking, SQLite sync/search/get/grep, autosync, flush logic, migration helpers |

## Command reference

### Full suite

```bash
go test ./...
```

### Focused package runs

```bash
go test ./internal/config
go test ./internal/channels/slack
go test ./internal/channels/signal
go test ./internal/runtime
go test ./internal/skills
go test ./internal/tui
go test ./internal/workspace
go test ./internal/session
go test ./internal/cron
go test ./internal/heartbeat
go test ./internal/backup
go test ./internal/cli
go test ./internal/hooks
go test ./internal/memory
go test ./internal/mcp/...
go test ./internal/llm/...
```

### Focused Red/Green examples

```bash
go test ./internal/config -run TestValidate -v
go test ./internal/channels/slack -run TestLocalAdapterSendUsesDefaultChannelAndReturnsDeliveryMetadata -v
go test ./internal/channels/signal -run TestLocalAdapterSendBuildsGroupCommandUsingDefaultRecipient -v
go test ./internal/channels/signal -run TestReceiveBatchParsesDirectAndGroupMessages -v
go test ./internal/runtime -run TestPromptIncludesBootstrapContextOnFirstMessageOnly -v
go test ./internal/runtime -run TestPromptStreamForSessionPassesThroughProviderToolEvents -v
go test ./internal/runtime -run TestRunSignalInboundRoutesSenderToMappedAgent -v
go test ./internal/runtime -run TestRunCronEntryDefaultUsesDefaultSessionPrompt -v
go test ./internal/skills -run TestSnapshotContainsEligibleSkillsOnly -v
go test ./internal/tui -run TestParseSlash -v
go test ./internal/workspace -run TestEnsureWorkspaceCreatesWorkspaceAndBootstrapFiles -v
go test ./internal/workspace -run TestEnsureWorkspaceCreatesBootstrapWhenWorkspaceExistsButCoreFilesMissing -v
go test ./internal/workspace -run TestEnsureWorkspaceDoesNotRecreateBootstrapAfterDeletion -v
go test ./internal/cron -run TestInProcessSchedulerRecurringExecutionFiresForDueJobs -v
go test ./internal/heartbeat -run TestLocalMonitorStartSkipsOverlappingTicks -v
go test ./internal/backup -run TestCreateBackupWritesTarGzWithNormalizedEntries -v
go test ./internal/cli -run TestRunChannelsCommandRequiresSubcommand -v
go test ./internal/cli -run TestRunBackupCommandCreatesArchive -v
go test ./internal/memory -run TestSQLiteIndexManagerSyncForceBuildsIndexAndStatus -v
```

### Runtime smoke checks

```bash
go run ./cmd/localclaw
go run ./cmd/localclaw doctor
go run ./cmd/localclaw doctor --deep
go run ./cmd/localclaw tui
go run ./cmd/localclaw backup
go run ./cmd/localclaw memory status
go run ./cmd/localclaw channels serve --once
go run ./cmd/localclaw mcp serve
```

- `go run ./cmd/localclaw` only prints help/usage; it does not initialize runtime.
- `go run ./cmd/localclaw mcp serve` starts a stdio server and typically waits for input until EOF or interrupt.

## Red/Green workflow (default)

1. Define behavior contract for the change.
2. Add the smallest failing test in the relevant package.
3. Run focused tests and confirm intentional failure.
4. Implement minimum code to satisfy behavior.
5. Re-run focused tests until green.
6. Run `go test ./...`.
7. Run `go fmt ./...` when Go files changed.
8. Update docs/specs when behavior or workflow changed.

## Test writing standards

- Assert observable outcomes and boundary behavior.
- Include meaningful failure-path tests for policy/security constraints.
- Keep tests deterministic and local-only (no network dependencies).
- Co-locate tests with changed package behavior.

## Common issues

- Startup policy test failures:
  - ensure tests do not assume configurable gateway/listener flags.
- Memory index test flakiness:
  - ensure temporary workspace/session paths are isolated per test.
- TUI behavior drift:
  - update tests together with slash keybinding/status changes.
- Missing provider binary in manual smoke runs:
  - if using Claude Code, configure `llm.claude_code.binary_path`.
  - if using Codex, configure `llm.codex.binary_path`.

## Recommended validation before handoff

```bash
go test ./...
```

For TUI/runtime behavior changes, also run:

```bash
go run ./cmd/localclaw
go run ./cmd/localclaw tui
```
