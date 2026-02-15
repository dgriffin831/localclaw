# localclaw Architecture (Implementation Detail)

This document describes the architecture as implemented in code with file-level anchors.

## 1. Scope and Source of Truth

Primary implementation anchors:
- Entrypoint: `cmd/localclaw/main.go`
- Runtime composition: `internal/runtime/app.go`
- Config + policy: `internal/config/config.go`
- TUI runtime: `internal/tui/app.go`
- LLM adapter: `internal/llm/claudecode/client.go`
- Security boundary summary: `SECURITY.md`

## 2. System Context

```text
Operator (terminal)
      |
      v
localclaw binary (single process)
  |- config load + local-only validation
  |- runtime wiring
  |   |- workspace manager
  |   |- memory store
  |   |- skills registry
  |   |- cron scheduler
  |   |- heartbeat monitor
  |   |- slack/signal adapters
  |   `- Claude Code client (subprocess)
  `- command modes
      |- check (startup validation run)
      `- tui (interactive terminal UI)
```

No server, gateway, or listener process exists in the architecture.

## 3. Repository Component Map

```text
localclaw/
|- cmd/localclaw/main.go
|- internal/
|  |- runtime/                 app composition and startup flow
|  |- config/                  defaults + validation + local-only policy
|  |- llm/claudecode/          local Claude CLI invocation + streaming
|  |- tui/                     Bubble Tea model/update/view and controls
|  |- memory/                  local persistence boundary
|  |- workspace/               local workspace boundary
|  |- skills/                  local skill registry boundary
|  |- cron/                    in-process scheduler boundary
|  |- heartbeat/               local liveness boundary
|  `- channels/{slack,signal}/ channel adapter boundaries
`- docs/
   |- adr/
   `- specs/
```

## 4. Startup and Run Modes

`cmd/localclaw/main.go` flow:
1. Parse flags (`-config`).
2. Load config (`config.Load`).
3. Validate and construct runtime (`runtime.New`).
4. Create cancellable context from SIGINT/SIGTERM.
5. Execute mode:
   - `check` (default): `app.Run(...)` then success message.
   - `tui`: `app.Run(...)` then `tui.Run(...)`.

## 5. Runtime Composition

`runtime.New` creates all module instances in-process:
- `memory.NewLocalStore`
- `workspace.NewLocalManager`
- `skills.NewLocalRegistry`
- `cron.NewInProcessScheduler`
- `heartbeat.NewLocalMonitor`
- `slack.NewLocalAdapter`
- `signal.NewLocalAdapter`
- `claudecode.NewClient`

`App.Run` executes startup sequence:
1. workspace init
2. memory init
3. skills load
4. scheduler start
5. startup heartbeat ping

## 6. Local-Only Boundary

The local-only boundary is enforced by config validation:
- `security.enforce_local_only` must remain `true`.
- `security.enable_gateway` must remain `false`.
- `security.enable_http_server` must remain `false`.
- `security.listen_address` must remain empty.

Any violation fails startup.

## 7. Current Maturity Snapshot

Implemented behavior:
- Config defaults and validation (including GovCloud auth constraints).
- Runtime startup policy checks and module wiring.
- Claude Code subprocess streaming path.
- Full-screen TUI with slash commands, streaming updates, and run controls.

Scaffolded boundaries (placeholder implementations today):
- memory persistence behavior
- workspace initialization behavior
- skills discovery/loading behavior
- cron scheduling behavior
- heartbeat emission behavior
- slack/signal channel transport behavior
