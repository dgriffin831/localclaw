# Memory Retrieval v2

`localclaw` memory retrieval is keyword + grep based.

## Retrieval primitives

- `memory_search`: ranked keyword recall from indexed chunks (FTS5 BM25 when available, deterministic LIKE/token fallback otherwise).
- `memory_grep`: exact literal or regex matching across allowed memory/session files, with optional source and path-glob filters.
- `memory_get`: line-scoped markdown file reads for citation and precise context.

## CLI commands

- `go run ./cmd/localclaw memory status`
- `go run ./cmd/localclaw memory index --force`
- `go run ./cmd/localclaw memory search "incident summary"`
- `go run ./cmd/localclaw memory grep "incident-1234"`

## Migration behavior from legacy embedding/vector versions

Config compatibility:

- Legacy `memorySearch` embedding/vector keys are ignored during JSON unmarshal.
- Ignored keys do not fail startup validation.
- Supported retrieval-v2 keys continue to apply normally.

SQLite compatibility:

- Schema install drops legacy `embedding_cache` table artifacts.
- Legacy `chunks` schemas containing `embedding` / `model` columns are normalized to v2 chunk layout.
- Legacy `meta` keys prefixed with `embedding_` or `vector_` are removed.

Operational note:

- Memory index content is markdown-source-of-truth; if schema normalization occurs, run `memory index --force` to refresh chunks from workspace/session sources.
