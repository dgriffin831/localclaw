# Workspace + Memory Parity Spec (Go-Native)

## Status

Implemented (historical delivery spec)

## Goal

Implement a production-grade, Go-native workspace and memory system in `localclaw` that closes the current parity gaps versus `openclaw` for:

- Workspace lifecycle and bootstrap behavior
- Durable memory files and semantic retrieval
- Session transcript memory indexing
- Memory lifecycle automation (flush + session snapshot hook)
- Operational visibility (status/index/search tooling)

This spec was implementation-focused and drove end-to-end delivery in `localclaw`.
It is retained as a historical record of the parity rollout and task breakdown.

For current behavior and contracts, use:

- `README.md`
- `ARCHITECTURE.md`
- `docs/ARCHITECTURE.md`
- `docs/RUNTIME.md`
- `docs/CONFIGURATION.md`

## Constraints

- Go only.
- Single process.
- Local-only policy remains enforced (no listener/gateway surfaces).
- Storage is local filesystem + local SQLite only.
- No Node/TypeScript runtime dependency.

## Current State Summary

Parity milestones described in this document are implemented in the current line:

- workspace resolve/create/bootstrap lifecycle
- session metadata store + transcript files
- SQLite memory index/search/get APIs
- memory CLI command mode (`status`, `index`, `search`)
- runtime memory tool integration (`memory_search`, `memory_get`)
- session reset/new snapshot hooks and compaction-adjacent flush plumbing

The original pre-parity gap notes are preserved below as design history.

## Parity Scope (Must Close)

The following capabilities are required for parity completion:

1. Workspace create/resolve/bootstrap lifecycle.
2. Per-agent workspace resolution with default + overrides.
3. Workspace bootstrap file loading and session-aware filtering.
4. Durable memory files (`MEMORY.md`, `memory/*.md`, optional extra paths).
5. Per-agent SQLite memory index with chunking and optional local embeddings (disabled by default).
6. Hybrid retrieval (vector + keyword where available), snippet results with line ranges.
7. Optional session transcript indexing as memory source.
8. Safe memory file read API (`memory_get` equivalent behavior).
9. Pre-compaction memory flush workflow with persisted metadata.
10. Session reset snapshot hook (save recent session context to `memory/YYYY-MM-DD-*.md`).
11. Operator CLI: memory status/index/search.
12. Full tests for behavior, migrations, and failure handling.

## Target Architecture

## High-Level Components

- `internal/workspace`
  - Workspace resolution, bootstrap templates, bootstrap file loading/filtering.
- `internal/session`
  - Session keying, session metadata store (`sessions.json`), transcript paths (`*.jsonl`), update helpers.
- `internal/memory`
  - Memory file discovery/chunking, embedding provider abstraction, SQLite index, search/get, sync/watch.
- `internal/runtime`
  - Wires workspace/session/memory services into app lifecycle.
- `cmd/localclaw`
  - Adds `memory` command group (`status`, `index`, `search`) and keeps `check`/`tui`.

## State Layout

Default state root: `~/.localclaw` (configurable).

Proposed layout:

```text
~/.localclaw/
  memory/
    <agentId>.sqlite
  agents/
    <agentId>/
      agent/
      sessions/
        sessions.json
        <sessionId>.jsonl
  workspace/                  # default main workspace
  workspace-<agentId>/        # non-default agents
```

## Config Model Changes

Add agent-aware and memory-search configuration while preserving backward compatibility.

### Backward-compatible mapping

- Existing `workspace.root` maps to `agents.defaults.workspace` for default agent.
- Existing `memory.path` is deprecated; used only as legacy import path.

### New config sections

- `state.root`
- `agents.defaults.workspace`
- `agents.list[].workspace` (optional override)
- `session.store` (supports `{agentId}`)
- `memorySearch` under `agents.defaults` and per-agent override:
  - `enabled`
  - `sources` (`memory`, `sessions`)
  - `extraPaths`
  - `provider` (`none`, `local`)
  - `fallback` (`none`, `local`)
  - `model` (default local embedding model: `google/embeddinggemma-3-small-v1`)
  - `store.path` (supports `{agentId}`)
  - `store.vector.enabled`
  - `chunking.tokens`, `chunking.overlap`
  - `query.maxResults`, `query.minScore`, `query.hybrid.*`
  - `sync.onSessionStart`, `sync.onSearch`, `sync.watch`, `sync.watchDebounceMs`, `sync.intervalMinutes`
  - `sync.sessions.deltaBytes`, `sync.sessions.deltaMessages`
  - `cache.enabled`, `cache.maxEntries`
  - `local.modelPath`, `local.modelCacheDir`
- `agents.defaults.compaction.memoryFlush.*`
- `hooks.sessionMemory.*`

## Workspace System Specification

## Responsibilities

1. Resolve workspace dir per agent.
2. Create workspace if missing.
3. Materialize bootstrap files from embedded templates when missing:
   - `AGENTS.md`
   - `SOUL.md`
   - `TOOLS.md`
   - `IDENTITY.md`
   - `USER.md`
   - `HEARTBEAT.md`
   - `WELCOME.md`
   - `BOOTSTRAP.md` (new workspace only)
4. Preserve existing files (never overwrite by default).
5. Optionally initialize git repository for brand-new workspace if git exists.
6. Load bootstrap files for runtime context; include `MEMORY.md`/`memory.md` when present.
   - `WELCOME.md` is for operator UX and is not injected into prompt bootstrap context.
7. Filter bootstrap files for subagent sessions (allowlist behavior).

## Go API (target)

```go
type Manager interface {
    Init(ctx context.Context) error
    ResolveWorkspace(agentID string) (string, error)
    EnsureWorkspace(ctx context.Context, agentID string, ensureBootstrap bool) (WorkspaceInfo, error)
    LoadBootstrapFiles(ctx context.Context, agentID, sessionKey string) ([]BootstrapFile, error)
}
```

## Template Strategy

- Store templates in `internal/workspace/templates/*.md`.
- Use `embed.FS` for deterministic packaging.
- Strip frontmatter before writing.

## Session System Specification

## Responsibilities

1. Define stable session keys and default session scope.
2. Maintain per-agent `sessions.json` metadata store.
3. Maintain per-session transcript file (`jsonl`).
4. Support atomic writes + lock-based concurrent safety.
5. Store memory-related metadata:
   - `totalTokens`
   - `compactionCount`
   - `memoryFlushAt`
   - `memoryFlushCompactionCount`
6. Provide event hooks for transcript updates to drive session-memory sync.

## Write Safety Requirements

- Session store writes are atomic (`.tmp` + rename on non-Windows).
- Per-store lock file with timeout + stale lock cleanup.
- File mode hardened to `0600` for state files where supported.

## Memory System Specification

## Memory Sources

- Workspace memory files:
  - `MEMORY.md` or `memory.md`
  - `memory/**/*.md`
  - optional `memorySearch.extraPaths` Markdown files/dirs
- Optional session transcript source:
  - extracted text from `agents/<agentId>/sessions/*.jsonl`

## File Safety

- Ignore symlinked files and symlinked directories while indexing.
- Restrict `memory_get` reads to:
  - workspace memory files
  - explicitly configured `extraPaths`
- Reject non-Markdown reads for memory-get path.

## Index Storage (SQLite)

Tables:

- `meta`
- `files` (path, source, hash, mtime, size)
- `chunks` (id, path, source, line range, hash, model, text, embedding, updatedAt)
- `chunks_fts` (optional FTS5)
- `embedding_cache` (provider/model/providerKey/hash)
- `chunks_vec` (optional vector extension table when available)

## Search Behavior

- Query embedding always computed.
- Vector search:
  - preferred: vector table + cosine distance function if available
  - fallback: in-process cosine over stored embeddings
- Keyword search:
  - FTS5 when available
  - disabled gracefully when unavailable
- Hybrid merge:
  - weighted vector + keyword score
  - candidate multiplier + max result cap
- Result shape:
  - `path`
  - `startLine`, `endLine`
  - `score`
  - `snippet` (truncated)
  - `source`

## Chunking Behavior

- Approx token -> char conversion (`tokens * 4`).
- Overlap support.
- Deterministic chunk hash (`sha256(text)`).
- Empty chunk filtering.

## Embedding Providers

Providers:

- `none` (default; keyword/metadata retrieval only)
- `local` (optional; local model only)

Requirements:

- Provider abstraction in Go with no direct cloud AI provider integrations.
- Query and batch embedding methods for local provider only.
- Default local embedding model is `google/embeddinggemma-3-small-v1` (target runtime footprint ~1GB RAM).
- Embeddings must remain optional; core memory search works without embeddings.
- Timeout controls for local query and batch operations.
- Fallback between `local` and `none` only.
- Provider-keyed cache isolation.

## Sync Model

Trigger types:

- `session-start`
- `search`
- `watch`
- `interval`
- `session-delta`
- `cli`

Behavior:

- Non-blocking background sync on lazy triggers.
- Force reindex on model/provider/chunking/schema drift.
- Safe reindex via temp DB + atomic swap.
- Watch memory files with debounce.
- Session transcript delta thresholds (`deltaBytes`, `deltaMessages`) before reindexing transcript source.

## Memory APIs (Parity Surface)

Define service methods equivalent to `memory_search` and `memory_get` semantics:

```go
type SearchOptions struct {
    MaxResults int
    MinScore   float64
    SessionKey string
}

type GetOptions struct {
    FromLine int
    Lines    int
}
```

These are consumed by:

- agent/tool runtime (when tool calling is enabled)
- CLI commands
- potential future TUI slash commands

## Memory Lifecycle Automation

## Pre-Compaction Memory Flush

When session token usage nears compaction threshold:

1. Run silent memory-flush turn with configurable prompts.
2. Instruct model to persist durable notes to `memory/YYYY-MM-DD.md`.
3. Skip flush for non-writable workspace contexts.
4. Persist `memoryFlushAt` and `memoryFlushCompactionCount`.

## Session Memory Hook on Reset

On `/new` or `/reset`:

1. Capture recent user/assistant turns from transcript.
2. Generate filename slug (LLM-assisted; deterministic fallback to timestamp).
3. Write snapshot file:
   - `memory/YYYY-MM-DD-<slug>.md`
4. Include metadata (session key/id/source/timestamp) + summary text.

## CLI Specification

Add command group:

- `localclaw memory status [--agent <id>] [--deep] [--index] [--json]`
- `localclaw memory index [--agent <id>] [--force]`
- `localclaw memory search <query> [--agent <id>] [--max-results <n>] [--min-score <v>] [--json]`

Status output includes:

- provider mode (`none` or `local`)
- fallback info (`local`/`none`)
- indexed files/chunks totals
- source breakdown (`memory`, `sessions`)
- dirty state
- vector/fts/cache/batch status
- DB/store/workspace paths
- source scan issues

## Implementation Plan

## Phase 1: Foundations (workspace + session store)

- Add state root + agent workspace resolution.
- Implement workspace ensure/bootstrap/load/filter.
- Add session package with metadata store + transcript file paths + locking.
- Migrate runtime wiring from stubs to real services.

Acceptance:

- Workspace is created and bootstrapped on startup.
- Session store read/write safe under concurrent writes.

## Phase 2: Core memory indexing and retrieval

- Implement memory file discovery, chunking, SQLite schema, indexing.
- Implement search/get with keyword/FTS-first behavior; vector path is optional local-only when enabled.
- Add memory manager status/probe/sync/close lifecycle.

Acceptance:

- `memory search` returns relevant snippets with paths/line ranges.
- `memory get` safely reads allowed memory files only.

## Phase 3: Provider + sync maturity

- Add provider abstraction (`none`, `local`) with `none` as default.
- Add fallback logic, timeout, cache, and batch embedding flows for local-only mode.
- Add watch/interval/session-delta sync scheduling.
- Add safe reindex swap.

Acceptance:

- Index auto-refreshes on memory file changes.
- Session transcript changes become searchable when enabled.

## Phase 4: Lifecycle automation parity

- Add pre-compaction memory flush flow.
- Add reset hook for session-memory snapshot files.
- Persist flush/session metadata.

Acceptance:

- Flush runs only when threshold and workspace mode allow.
- `/new` creates dated session snapshot file in `memory/`.

## Phase 5: Tool/runtime integration + hardening

- Expose memory search/get through runtime tool layer.
- Inject memory recall policy into system prompt when tools enabled.
- Add full integration tests and failure-path tests.

Acceptance:

- Agent runtime can call memory search/get without crashing on provider/index errors.
- Failures degrade gracefully (empty results + explicit error payload).

## Concrete Implementation Task List (PR-Sized Milestones)

Each milestone below is intended to be merged as an independent PR with green tests.

### Tracking Checklist

- [x] PR-01 Config + Path Foundation
- [x] PR-02 Workspace Manager Real Implementation
- [x] PR-03 Session Store + Transcript Subsystem
- [x] PR-04 Runtime Integration of Workspace + Session
- [x] PR-05 Memory File Discovery + Chunking Core
- [x] PR-06 SQLite Memory Schema + Index Manager Skeleton
- [x] PR-07 Embedding Provider Interface (Local + None Only)
- [x] PR-08 Search + Read APIs (`memory_search`/`memory_get` semantics)
- [x] PR-09 CLI `memory` Commands
- [x] PR-10 Watch/Interval Sync + Safe Reindex Swap
- [x] PR-11 Session Transcript Source Indexing + Delta Triggers
- [x] PR-12 Pre-Compaction Memory Flush
- [x] PR-13 Session Memory Snapshot Hook (`/new`/`/reset`)
- [x] PR-14 Tool Runtime Integration + Prompt Policy
- [x] PR-15 Hardening + Migration + Release Cut

### PR-01: Config + Path Foundation

Scope:

- Add `state.root` and agent-aware config scaffolding.
- Add `agents.defaults.workspace`, `agents.list[].workspace`.
- Add `session.store` (with `{agentId}` support).
- Add memory-search config structures (disabled in runtime for now).
- Add backward-compatible mapping from legacy `workspace.root` and `memory.path`.

Files:

- `internal/config/config.go`
- `internal/config/config_test.go`
- `docs/specs/mvp.md` (if defaults/flags need mention)

Acceptance:

- `go test ./...` passes.
- Default config remains valid.
- Legacy fields still load and map correctly.

### PR-02: Workspace Manager Real Implementation

Scope:

- Replace workspace no-op with real manager.
- Resolve default/per-agent workspace directories.
- Ensure workspace creation and bootstrap file materialization.
- Add embedded workspace templates and frontmatter stripping.
- Add bootstrap loader + subagent bootstrap allowlist filter.

Files:

- `internal/workspace/manager.go` (or split into `manager.go`, `bootstrap.go`, `templates.go`)
- `internal/workspace/templates/*`
- `internal/runtime/app.go`
- tests under `internal/workspace/*_test.go`

Acceptance:

- Fresh run creates workspace and bootstrap files.
- Existing files are not overwritten.
- Bootstrap files load into structured list with `missing` flags.

### PR-03: Session Store + Transcript Subsystem

Scope:

- Add `internal/session` package:
  - types (`SessionEntry`, origins, delivery metadata)
  - per-agent sessions path resolution
  - lock-safe session store load/save/update
  - transcript path helpers (`*.jsonl`)
- Atomic writes + lock file behavior + stale lock cleanup.

Files:

- `internal/session/*`
- `internal/runtime/app.go` wiring
- tests under `internal/session/*_test.go`

Acceptance:

- Concurrent updates do not corrupt `sessions.json`.
- Lock timeout/stale lock behavior is deterministic in tests.
- Session store files are created with hardened perms where supported.

### PR-04: Runtime Integration of Workspace + Session

Scope:

- Runtime initializes workspace + session services.
- Add shared agent/session resolution helpers used by runtime/TUI/CLI.
- Replace direct use of stubbed workspace path in status with resolved workspace path.

Files:

- `internal/runtime/app.go`
- `internal/tui/app.go`
- `cmd/localclaw/main.go` (if command wiring needs shared runtime services)

Acceptance:

- `localclaw check` initializes workspace + session services cleanly.
- No behavior regression in existing TUI startup.

### PR-05: Memory File Discovery + Chunking Core

Scope:

- Implement memory file scanner:
  - `MEMORY.md`/`memory.md`
  - `memory/**/*.md`
  - optional extra paths
  - symlink-ignore logic
- Implement chunking + hash helpers.
- Implement safe relative path normalization utilities.

Files:

- `internal/memory/internal.go` (or split modules)
- tests under `internal/memory/internal_test.go`

Acceptance:

- Scanner returns deterministic set across path forms.
- Symlink files/dirs are ignored.
- Chunking overlap behavior is covered by tests.

### PR-06: SQLite Memory Schema + Index Manager Skeleton

Scope:

- Introduce SQLite-backed index manager:
  - DB open/close
  - schema install (`meta`, `files`, `chunks`, optional `fts`, optional `embedding_cache`)
  - status snapshot
  - memory sync of files (hash-based skip, stale cleanup)
- No provider-specific embeddings yet; use pluggable interface.

Files:

- `internal/memory/manager.go`
- `internal/memory/schema.go`
- `internal/memory/sqlite.go`
- tests under `internal/memory/*_test.go`

Acceptance:

- `sync(force=true)` builds file/chunk index from memory files.
- Re-running sync without changes is no-op-ish (hash skip).
- Status returns file/chunk counts.

### PR-07: Embedding Provider Interface (Local + None Only)

Scope:

- Add provider interface and selection logic (`none`, `local`) with `none` default.
- Add local-only activation/error messaging (no cloud providers).
- Set `google/embeddinggemma-3-small-v1` as the explicit default model when `provider=local` and no model override is supplied.
- Add query/batch timeout controls for local embedding execution.

Files:

- `internal/memory/embeddings.go`
- `internal/memory/embeddings_local.go`
- tests under `internal/memory/embeddings*_test.go`

Acceptance:

- Provider resolution honors `none`/`local` config + fallback rules.
- Missing local model/runtime setup errors are explicit and actionable.
- `provider=local` defaults to `google/embeddinggemma-3-small-v1` when model is unset.
- Default mode does not require embeddings and remains fully functional.

### PR-08: Search + Read APIs (`memory_search`/`memory_get` Semantics)

Scope:

- Implement keyword search (FTS when available) as primary path.
- Implement optional vector search path for local embeddings only.
- Implement merge weighting/thresholds that work with and without vectors.
- Implement safe read-file API with path restrictions and line slicing.

Files:

- `internal/memory/search.go`
- `internal/memory/manager.go`
- tests under `internal/memory/search*_test.go`

Acceptance:

- Search returns snippets with path + line ranges + score + source.
- `memory_get` equivalent rejects out-of-scope paths.
- Ranking behavior is covered for both keyword-only and local-vector modes.

### PR-09: CLI `memory` Commands

Scope:

- Add `localclaw memory status/index/search`.
- Add `--deep`, `--index`, `--force`, `--json` behavior.
- Add source scan diagnostics in status output.

Files:

- `cmd/localclaw/main.go` command routing
- `internal/cli/memory.go` (new)
- tests under `internal/cli/*_test.go`

Acceptance:

- Commands execute against real index manager.
- JSON output stable and parseable.
- Failures set non-zero exit codes.

### PR-10: Watch/Interval Sync + Safe Reindex Swap

Scope:

- Add memory file watcher with debounce.
- Add interval sync scheduler.
- Add safe reindex logic (temp DB + atomic swap).
- Add embedding cache pruning and provider-key segregation.

Files:

- `internal/memory/manager.go`
- tests under `internal/memory/manager_sync*_test.go`

Acceptance:

- File changes mark index dirty and trigger sync.
- Reindex swap survives simulated failure without index loss.
- No unhandled goroutine errors on watch-triggered sync failures.

### PR-11: Session Transcript Source Indexing + Delta Triggers

Scope:

- Parse session JSONL transcripts into normalized text.
- Add `sessions` memory source indexing.
- Add session-delta thresholds (`deltaBytes`, `deltaMessages`) and debounce.
- Add transcript update event hook from session writer to memory manager.

Files:

- `internal/session/transcript.go`
- `internal/session/events.go`
- `internal/memory/session_files.go`
- `internal/memory/manager.go`
- tests under `internal/memory/session*_test.go`

Acceptance:

- Session updates become searchable when `sources` includes `sessions`.
- Small transcript churn below threshold does not trigger full sync.

### PR-12: Pre-Compaction Memory Flush

Scope:

- Add compaction-aware memory flush settings.
- Add `shouldRunMemoryFlush` logic.
- Execute silent memory flush turn via existing LLM path.
- Persist `memoryFlushAt` + `memoryFlushCompactionCount`.

Files:

- `internal/memory/flush.go`
- runtime/session integration points
- tests under `internal/memory/flush*_test.go`

Acceptance:

- Flush triggers near threshold and skips otherwise.
- Read-only workspace mode (when applicable) suppresses flush.
- Session metadata updates are persisted.

### PR-13: Session Memory Snapshot Hook (`/new`/`/reset`)

Scope:

- Add hook runner on session reset.
- Read recent conversation from transcript.
- Generate slug (LLM-assisted optional; deterministic fallback required).
- Write `memory/YYYY-MM-DD-<slug>.md` snapshot with metadata and summary.

Files:

- `internal/hooks/session_memory.go`
- integration with TUI/reset command path and runtime reset path
- tests under `internal/hooks/*_test.go`

Acceptance:

- Reset command creates dated memory snapshot files.
- Hook failures are logged but non-fatal.

### PR-14: Tool Runtime Integration + Prompt Policy

Scope:

- Expose memory search/get in tool registry/runtime.
- Add system prompt guidance to enforce memory recall step when tools are available.
- Ensure tool failures degrade gracefully.

Files:

- `internal/skills/registry.go` or tool registry package
- `internal/runtime/*`
- prompt construction files (if present)
- tests for tool execution/error behavior

Acceptance:

- Runtime can invoke memory search/get end-to-end.
- Prompt includes memory recall policy when memory tools are enabled.

### PR-15: Hardening + Migration + Release Cut

Scope:

- Implement one-time legacy memory import (`memory.path` JSON -> `MEMORY.md`).
- Finalize migrations and backwards compatibility tests.
- Add performance and concurrency stress tests.
- Update docs (`README.md`, `ARCHITECTURE.md`, `HANDOFF.md`, `ROADMAP.md`).

Files:

- migration helpers in `internal/memory` or `internal/config`
- docs updates
- integration test suites

Acceptance:

- Legacy installs upgrade without data loss.
- Full suite passes with race detector where feasible (`go test -race ./...`).
- Spec parity checklist is fully satisfied.

## Milestone Gate

Before starting each PR:

1. Confirm prior PR acceptance criteria are green.
2. Keep scope constrained to the stated milestone.
3. Include tests in the same PR.
4. Update this spec checkbox status for milestone completion.

## Migration Plan

1. Introduce new config fields with defaults; keep old fields readable.
2. On startup, map legacy `workspace.root` and `memory.path`.
3. If legacy memory JSON exists, import records into `MEMORY.md` once (idempotent marker file).
4. Create state dirs/workspace dirs lazily.
5. Keep all operations local and backward compatible for existing users.

## Test Plan

## Unit Tests

- Workspace resolve/bootstrap/template logic.
- Session store lock/atomic write/stale lock cleanup.
- Memory file scanner symlink/path restrictions.
- Chunking and hash determinism.
- Search ranking and hybrid merge.
- Local embedding enable/disable, fallback (`local`/`none`), and cache behavior.
- Flush threshold logic.
- Session hook filename/contents.

## Integration Tests

- End-to-end index build over workspace memory files.
- Session transcript indexing and delta-triggered sync.
- Reindex swap safety and restart continuity.
- CLI status/index/search flows.
- Corrupt or missing DB/session store recovery.

## Concurrency and Reliability Tests

- Concurrent session store writes.
- Concurrent memory sync trigger races.
- Watch-triggered sync error handling (no panic/unhandled goroutine crash).

## Security Requirements

- Enforce local-only constraints (no listeners added by this work).
- Reject path traversal and disallowed file reads in memory-get.
- Ignore symlink traversal in memory scans.
- Restrict state file permissions where platform supports it.
- Do not block message delivery/runtime on best-effort metadata write failures.

## Observability Requirements

- Structured logs for:
  - sync triggers/reasons
  - provider mode selection and fallback activation (`local`/`none`)
  - vector/fts availability
  - index/reindex durations and counts
- Memory status snapshot callable by CLI and runtime diagnostics.

## Definition of Done

This spec is complete when:

1. All parity scope items are implemented.
2. Stubbed workspace/memory components are removed from runtime-critical paths.
3. CLI memory commands operate successfully on real data.
4. Memory lifecycle automation (flush + reset snapshot hook) is active and tested.
5. Test suite covers normal, degraded, and concurrent failure paths.
