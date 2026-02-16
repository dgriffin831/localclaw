# Memory Retrieval v2 (FTS + Grep, No Embeddings)

## Status

Completed (Phase 1-4)

## Problem / Motivation

Current `localclaw` memory retrieval includes optional embedding/vector paths (provider resolution, local embedding runtime, vector scoring, embedding cache, and related config/docs/tests). This increases operational and code complexity for a local-only CLI where primary recall needs are:

- reliable keyword retrieval from markdown memory
- exact-token retrieval (IDs, symbols, paths, error strings)
- safe file-scoped reads for citation/context

The goal of v2 is to simplify memory retrieval to a local-first, deterministic model:

- BM25/FTS keyword search
- grep-style exact/regex search
- no embeddings/vector dependencies or config

## Scope

In scope:

- Remove embedding/vector retrieval from memory indexing/search runtime.
- Make `memory_search` BM25/FTS-first (with deterministic non-FTS fallback).
- Add a new grep-style memory retrieval tool exposed to runtime and MCP:
  - runtime tool: `memory_grep`
  - MCP tools: `localclaw_memory_grep` and `memory_grep` alias
- Add CLI command:
  - `localclaw memory grep ...`
- Simplify `memorySearch` config by removing embedding/vector/hybrid/cache/local-embedding fields.
- Update runtime prompt policy text to reflect keyword + grep workflow.
- Update docs/tests to match new contracts.

## Out of Scope

- Reintroducing any semantic/vector retrieval.
- External search services or network retrieval.
- Changing `memory_get` scope semantics.
- New HTTP/gateway/server runtime surfaces.
- Monthly/yearly indexing jobs or additional background daemons.

## Behavior Contract

### 1) Memory index model (v2)

Memory index remains local SQLite and markdown-source-of-truth:

- Indexed sources unchanged:
  - `MEMORY.md` / `memory.md`
  - `memory/**/*.md`
  - optional `memorySearch.extraPaths` markdown paths
  - optional `sessions` source if configured
- Chunking remains line-aware (`chunkTokens`, `chunkOverlap`).
- Indexed chunk payload removes embedding fields.

Search-relevant schema (conceptual):

- `files`
- `chunks` (`id`, `path`, `source`, `start_line`, `end_line`, `hash`, `text`, `updated_at`)
- `chunks_fts` (FTS5 virtual table + triggers) when available
- `meta`

Removed schema elements:

- `chunks.embedding`
- `chunks.model`
- `embedding_cache` table and metadata

### 2) `memory_search` behavior (BM25/FTS only)

`memory_search` continues to return:

- `path`
- `startLine`
- `endLine`
- `score`
- `snippet`
- `source`

Search logic:

1. Validate non-empty query.
2. Retrieve candidates via FTS5 BM25 when available.
3. If FTS unavailable or query yields no rows, fallback to deterministic LIKE/token scoring over `chunks.text`.
4. Rank results by keyword score only.
5. Apply `min_score` and `max_results`.

No vector scoring path exists in v2.
No embedding provider/runtime initialization occurs during search.

### 3) New `memory_grep` behavior (rg-style retrieval)

`memory_grep` is a second retrieval primitive for exact/regex discovery.

Purpose:

- find exact literals/tokens quickly
- support regex search for structured strings
- enable scoped/path-filtered retrieval for agents (Claude Code/Codex CLI via MCP)

Initial request schema:

- `query` (required string)
- `mode` (optional: `literal` | `regex`, default `literal`)
- `case_sensitive` (optional bool, default `false`)
- `word` (optional bool, default `false`, literal mode only)
- `max_matches` (optional int, default `50`, hard cap `500`)
- `context_lines` (optional int, default `0`, hard cap `5`)
- `path_glob` (optional string or list, workspace-relative filters)
- `source` (optional: `memory` | `sessions` | `all`, default `all`)

Response:

- `count`
- `matches[]` with:
  - `path`
  - `line`
  - `start` / `end` (byte offsets within line, optional for regex)
  - `text` (matching line)
  - `before[]` / `after[]` (context)
  - `source`

Safety and boundaries:

- Restrict search to discovered allowed memory/session files.
- Reject traversal/out-of-scope paths in `path_glob` filters.
- Use Go RE2 (`regexp`) for regex mode.
- Do not shell out to external `rg` binary.

### 4) Runtime + MCP tool exposure

Runtime tools:

- keep `memory_search`
- keep `memory_get`
- add `memory_grep`

MCP tools:

- keep `localclaw_memory_search` + alias `memory_search`
- keep `localclaw_memory_get` + alias `memory_get`
- add `localclaw_memory_grep` + alias `memory_grep`

Tool policy behavior:

- unchanged allow/deny evaluation model
- `memory_grep` must be policy-addressable by exact name/wildcard rules

### 5) CLI surface

Existing:

- `memory status`
- `memory index`
- `memory search`

New:

- `memory grep <query> [flags]`

`memory status` output changes:

- remove provider/fallback/vector/cache reporting
- retain index/source/fts/dirty observability

### 6) Configuration contract (v2)

`memorySearch` retains:

- `enabled`
- `sources`
- `extraPaths`
- `store.path`
- `chunking.tokens`
- `chunking.overlap`
- `query.maxResults`
- `query.minScore`
- `sync.onSearch`
- `sync.sessions.deltaBytes`
- `sync.sessions.deltaMessages`

Removed from config structs/defaults/docs:

- `provider`
- `fallback`
- `model`
- `store.vector`
- `query.hybrid.*`
- `cache.*`
- `local.*`

Compatibility behavior for existing JSON config files:

- removed keys are ignored by Go JSON unmarshal (no startup failure).
- runtime behavior uses v2 defaults and no embedding/vector paths.

## Implementation Notes

### A) Memory package simplification

Target packages/files:

- `internal/memory/manager.go`
- `internal/memory/sqlite.go`
- `internal/memory/search.go`
- `internal/memory/schema.go`

Required changes:

- remove embedding provider field/state from `SQLiteIndexManager`
- remove vector/hybrid/cache controls from `IndexManagerConfig`
- remove vector resolution path from `Search`
- keep FTS + LIKE fallback ranking
- ensure sync/index writes only text chunks (no embedding/model columns)
- introduce/upgrade schema migration path for v2 (safe reindex on force or schema mismatch)

### B) Remove embedding runtime code paths

Remove usage and references:

- `internal/memory/embeddings.go`
- `internal/memory/embeddings_local.go`
- embedding-specific tests
- `docs/EMBEDDINGS.md` (delete or archive with explicit deprecation note)

### C) Add grep retrieval implementation

Add:

- `internal/memory/grep.go` (or extend `search.go`)
- `GrepOptions`, `GrepMatch`, `GrepResult` types
- `(*SQLiteIndexManager).Grep(ctx, query, opts)` API

Implementation approach:

- iterate allowed files by source/path filters
- stream file lines via buffered scanner
- evaluate match predicate per line (literal/regex, case/word options)
- collect bounded results + context lines
- stable result ordering: path asc, line asc

### D) Runtime tooling changes

Update:

- `internal/skills/registry.go` (register `memory_grep`)
- `internal/runtime/tools.go`
  - add `executeMemoryGrepTool`
  - include tool definition in prompt assembly
  - update memory recall policy text:
    - use `memory_search` for ranked keyword recall
    - use `memory_grep` for exact token/regex follow-up

### E) MCP tool wiring

Update:

- `internal/mcp/tools/memory.go` (new `MemoryGrepTool`)
- `internal/cli/mcp.go` (register definitions + alias + policy wiring)
- `internal/runtime/mcp_support.go` (add `MCPMemoryGrep`)

### F) CLI updates

Update:

- `internal/cli/memory.go`
  - add `grep` subcommand parser/output
  - remove provider/fallback/vector/cache fields from status payload
  - ensure per-agent memorySearch resolution remains consistent

### G) Config and docs updates

Update:

- `internal/config/config.go`
  - remove embedding/vector-related fields from structs/defaults/validation
- docs:
  - `docs/CONFIGURATION.md`
  - `docs/RUNTIME.md`
  - `docs/ARCHITECTURE.md`
  - `docs/TESTING.md`
  - `docs/SECURITY.md` (grep scope guarantees)

## Implementation Plan (Phased)

### Phase 1: Spec + failing tests for v2 contracts

Add failing tests for:

- no embedding provider initialization in memory search path
- `memory_search` uses FTS/LIKE keyword ranking only
- `memory_grep` runtime tool and MCP tool schemas/handlers
- CLI `memory grep` behavior and status output changes
- config defaults/validation after field removals

Focused Red commands:

- `go test ./internal/memory -run TestSearch`
- `go test ./internal/runtime -run Test`
- `go test ./internal/mcp -run Test`
- `go test ./internal/cli -run TestRunMemory`
- `go test ./internal/config -run TestValidate`

### Phase 2: Remove embeddings/vector internals

- strip embedding/vector/cache code and config plumbing
- simplify schema + sync write paths
- keep FTS install and LIKE fallback behavior

Green validation:

- `go test ./internal/memory`

### Phase 3: Implement grep retrieval + tools

- implement memory grep API
- wire runtime tool + MCP definitions/handlers + policy integration
- wire CLI `memory grep`

Green validation:

- `go test ./internal/runtime`
- `go test ./internal/mcp`
- `go test ./internal/cli`

### Phase 4: Docs + migration hardening + full suite

- update docs to remove embedding/vector instructions
- add migration notes for existing memory DB/config keys
- run full suite and smoke commands

Completion notes:

- Added retrieval-v2 primary doc (`docs/MEMORY_RETRIEVAL.md`) and updated docs map links.
- Hardened SQLite schema install to normalize legacy `chunks` embedding/model columns and purge legacy embedding/vector meta keys.
- Confirmed full validation matrix passes (using `/usr/local/go/bin/go` in this environment).

Validation:

- `go test ./...`
- `go run ./cmd/localclaw check`
- `go run ./cmd/localclaw memory status`
- `go run ./cmd/localclaw memory search "test"`
- `go run ./cmd/localclaw memory grep "test"`

## Test Plan

Unit/integration updates required:

- `internal/memory/search_test.go`
  - remove vector tests
  - add BM25/LIKE-only ranking and edge cases
- `internal/memory/schema*test.go`
  - v2 schema installation and migration behavior
- `internal/runtime/tools_test.go`
  - `memory_grep` execution and prompt schema inclusion
- `internal/mcp/tools/*test.go`, `internal/mcp/server_test.go`
  - grep tool definitions/alias/call behavior
- `internal/cli/memory_test.go`
  - grep command output and JSON payload
  - status payload no longer includes embedding/vector fields
- `internal/config/config_test.go`
  - defaults and validation no longer reference local embeddings/vector settings

Regression checks:

- `memory_get` scope restrictions unchanged
- path traversal protections unchanged
- session-source indexing behavior unchanged

## Acceptance Criteria

- [x] `memory_search` has no embedding/vector dependency and never initializes embedding providers.
- [x] No embedding/vector/cache config fields remain in runtime structs/defaults/docs.
- [x] `memory_grep` is available as runtime tool and MCP tool (with alias), with strict local scope limits.
- [x] CLI exposes `memory grep` with JSON and human output modes.
- [x] `go test ./...` passes with embedding/vector tests removed or replaced.
- [x] Docs are updated to describe BM25/FTS + grep retrieval model only.

## Rollback / Risk Notes

Primary risks:

- relevance regression for paraphrase-heavy queries previously handled by vectors
- schema migration mistakes during forced reindex/swap
- tool-call prompt habits still expecting semantic search wording

Mitigations:

- preserve robust FTS + LIKE fallback behavior with deterministic ranking
- keep safe reindex swap strategy and add migration tests
- update prompt policy/tool descriptions to explicit keyword + grep workflow

Rollback strategy:

- revert v2 branch and restore previous memory package/config/docs in one commit if unacceptable recall regressions are observed in operator workflows.
