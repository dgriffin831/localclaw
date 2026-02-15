# Testing Guide

This document describes testing coverage, commands, and Red/Green workflow for `localclaw`.

## Goals

- Catch regressions in local-only policy and startup behavior.
- Keep config contract and runtime wiring aligned.
- Keep TUI command/input behavior stable.
- Prefer behavior assertions over implementation-detail assertions.

## Test Stack Overview

| Layer | Tooling | Primary Locations | What It Validates |
| --- | --- | --- | --- |
| Config and policy | Go `testing` | `internal/config/config_test.go` | default config validity, allowlists, local-only policy constraints, GovCloud validation |
| Runtime startup boundary | Go `testing` | `internal/runtime/local_only_test.go` | startup rejection when forbidden network flags are enabled |
| TUI logic | Go `testing` | `internal/tui/app_test.go` | slash parsing, elapsed formatting, input model behavior |

## Current Coverage Snapshot (as of February 15, 2026)

- Total test files: `3`
- Total package-level test focus:
  - config validation and policy checks
  - runtime local-only startup enforcement
  - foundational TUI behavior helpers

## Where Tests Live

- Config tests: `internal/config/config_test.go`
- Runtime tests: `internal/runtime/local_only_test.go`
- TUI tests: `internal/tui/app_test.go`

## Commands

### Full suite

```bash
go test ./...
```

### Focused package runs

```bash
go test ./internal/config
go test ./internal/runtime
go test ./internal/tui
```

### Focused test-by-name runs (Red/Green loops)

```bash
go test ./internal/config -run TestValidateRequiresGovCloudRegion -v
go test ./internal/runtime -run TestNewFailsWhenNetworkServerEnabled -v
go test ./internal/tui -run TestParseSlash -v
```

### Runtime smoke checks

```bash
go run ./cmd/localclaw
go run ./cmd/localclaw tui
```

## Red/Green Workflow (Default)

1. Define behavior contract for the change.
2. Add smallest failing test in relevant package.
3. Run focused command and confirm intentional failure.
4. Implement minimum code to satisfy test.
5. Re-run focused tests until green.
6. Run `go test ./...`.
7. Run `go fmt ./...` when Go files changed.
8. Update docs/specs when behavior or workflow changed.

## Test Writing Standards

- Assert observable outcomes and boundary behavior.
- Include meaningful failure-path tests for policy/security constraints.
- Keep tests deterministic (no external network dependency).
- Co-locate tests with changed package.

## Common Issues

- Failing startup tests from policy changes:
  - verify `security.*` local-only fields in fixture/config values.
- TUI test instability:
  - keep tests focused on pure behavior helpers and model transitions.
- Missing Claude binary during manual runs:
  - ensure `llm.claude_code.binary_path` points to installed CLI.

## Recommended Validation Before PR

```bash
go test ./...
```

For TUI/CLI behavior changes, also run:

```bash
go run ./cmd/localclaw
go run ./cmd/localclaw tui
```
