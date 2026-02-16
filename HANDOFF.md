# HANDOFF

## Branch

- `feature/agentic-loop-improvements`

## Scope Completed

Memory retrieval v2 stabilization and cleanup from in-progress working state:

- Removed embedding/vector runtime paths from memory config/runtime/indexing.
- Kept memory retrieval surface focused on:
  - `memory_search` (keyword ranking via FTS/LIKE fallback)
  - `memory_grep` (literal/regex exact matching)
  - `memory_get` (scoped markdown reads)
- Added schema migration coverage for legacy embedding artifacts:
  - drops legacy `embedding_cache` table if present
  - removes legacy `meta.embedding_cache_enabled`
- Aligned runtime/tool tests and CLI tests with v2 config shape.
- Updated docs to reflect v2 behavior and command surface.

## Commits (Newest First)

- `cf8e705` `feat(memory): finalize retrieval v2 keyword-only memory stack`
  - code + tests
  - deletes obsolete embedding provider/runtime implementation and tests
  - adds `internal/memory/schema_migration_test.go`

## Verification

Executed from repo root:

1. `/usr/local/go/bin/go fmt ./...`
   - result: success
2. `/usr/local/go/bin/go clean -testcache && /usr/local/go/bin/go test ./...`
   - result: success
   - key package checks:
     - `ok github.com/dgriffin831/localclaw/internal/memory`
     - `ok github.com/dgriffin831/localclaw/internal/runtime`
     - `ok github.com/dgriffin831/localclaw/internal/cli`
     - full suite green

## Remaining Risks / Follow-ups

1. Config compatibility intentionally ignores legacy embedding/vector keys at unmarshal time; malformed values under ignored keys will no longer be validated, by design for v2 compatibility.
2. `docs/EMBEDDINGS.md` is now a deprecated note; if you want to retire it fully, remove the file in a docs-only follow-up.
3. Consider adding a changelog/spec entry explicitly calling out removal of embedding/vector support for operator migration visibility.
