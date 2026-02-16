# HANDOFF

## Branch / Scope
- Branch: `feature/agentic-loop-improvements`
- Scope finalized: memory retrieval v2 keyword-only stack (FTS/LIKE search + grep), embedding/vector path removal, schema compatibility/migration cleanup.

## Commits
- `cf8e705` feat(memory): finalize retrieval v2 keyword-only memory stack
  - Removes embedding provider/runtime/cache implementation from memory indexing/search.
  - Simplifies `memorySearch` config surface to v2 fields (sources/path/chunking/query/sync).
  - Updates runtime + CLI manager wiring to keyword/grep-only retrieval.
  - Adds schema migration coverage to remove legacy `embedding_cache` artifacts.
  - Updates tests impacted by removed embedding knobs.

## Verification
Commands run from repo root:

1. `go test ./internal/config ./internal/memory`
- Result: pass

2. `/usr/local/go/bin/go test ./...`
- Result: pass

3. `/usr/local/go/bin/go fmt ./...`
- Result: pass (no formatting errors)

4. `/usr/local/go/bin/go test ./...` (post-format re-run)
- Result: pass

## Docs Updated
- `README.md`
- `docs/ARCHITECTURE.md`
- `docs/CONFIGURATION.md`
- `docs/EMBEDDINGS.md` (now deprecation note for embedding runtime)
- `docs/README.md`
- `docs/RUNTIME.md`
- `docs/TESTING.md`

## Risks / Follow-ups
1. Legacy JSON keys are silently ignored by Go unmarshalling; this preserves startup compatibility but does not produce explicit deprecation warnings.
2. `docs/specs/memory-retrieval-v2.md` still contains historical phase checklist items that may now lag implementation status; reconcile checklist items if strict spec parity is required.
3. If operators still rely on old embedding docs/workflows, consider adding a dedicated migration note in release notes/changelog.
