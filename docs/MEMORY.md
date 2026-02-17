# Memory

`localclaw` memory retrieval is local-only and deterministic. The implementation is keyword search + grep over workspace/session sources, backed by SQLite.

## Review status

Implementation review against the completed retrieval-v2 work confirms the core contract is in place:

- Embedding/vector retrieval paths are removed from runtime memory indexing and search.
- Retrieval primitives are `memory_search`, `memory_grep`, and `memory_get`.
- MCP memory tools are exposed as `localclaw_memory_search`, `localclaw_memory_grep`, and `localclaw_memory_get`.

Current caveat:

- Memory CLI command mode (`localclaw memory ...`) still resolves settings from `agents.defaults.memory` instead of the runtime's per-agent merged memory config.

## Retrieval primitives

- `memory_search`
  - Ranked keyword retrieval from indexed chunks.
  - Uses FTS5 BM25 when available.
  - Falls back to deterministic LIKE/token scoring when FTS is unavailable or returns no rows.
  - Returns `path`, `startLine`, `endLine`, `score`, `snippet`, and `source`.

- `memory_grep`
  - Exact/regex line matching across allowed memory/session files.
  - Supports `mode`, `case_sensitive`, `word` (literal mode), `max_matches` (capped at 500), `context_lines` (capped at 5), `path_glob`, and `source` (`memory`, `sessions`, `all`).
  - Match order is deterministic (`path`, then `line`).
  - `start`/`end` offsets are byte indexes and may be omitted in JSON when zero-valued.

- `memory_get`
  - Safe file-scoped reads for markdown memory files.
  - Enforces in-scope paths and optional line slicing (`from_line`, `lines`).

## Index model

Index storage is SQLite with markdown/session files as source of truth.

Primary tables/features:

- `files`
- `chunks` (`path`, `source`, line range, `hash`, `text`, `updated_at`)
- optional FTS5 `chunks_fts` + triggers
- `meta`

Indexed sources:

- `MEMORY.md` / `memory.md`
- `memory/**/*.md`
- optional `memory.extraPaths`
- optional sessions transcripts (`sessions` source)

Chunk content is text-only. No embedding/model/vector columns are used by the active schema.

## Runtime and MCP exposure

Runtime tool names:

- `memory_search`
- `memory_grep`
- `memory_get`

MCP server tool names:

- `localclaw_memory_search`
- `localclaw_memory_grep`
- `localclaw_memory_get`

Note: compatibility aliases without the `localclaw_` prefix are not currently registered.

## CLI surface

Memory command mode supports:

- `memory status`
- `memory index`
- `memory search`
- `memory grep`

`memory status` reports index counts, FTS availability, source diagnostics, and sync summaries. It no longer reports embedding/vector/cache fields.

## Configuration and migration notes

Memory settings live under `agents.defaults.memory` with optional per-agent overrides in `agents.list[].memory`.

Important behavior:

- Config decoding is strict (`DisallowUnknownFields`). Removed/legacy keys fail parsing.
- Legacy embedding/vector config keys are not accepted in current config files.
- Runtime/MCP memory behavior uses merged per-agent memory config.
- CLI memory command mode currently does not apply per-agent merged overrides.

SQLite migration note:

- Current startup/schema install does not perform legacy embedding-table cleanup.
- Recommended upgrade path for old indexes is `localclaw memory index --force` after moving to retrieval-v2 config.
