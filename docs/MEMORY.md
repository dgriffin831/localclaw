# Memory

LocalClaw memory is markdown source-of-truth plus a local SQLite keyword/vector index.

## Sources

Supported configured source:

- `memory`

Indexed files:

- `MEMORY.md`
- `memory.md`
- `memory/**/*.md`
- configured `memory.extraPaths`

Session transcript indexing is not part of the configured LocalClaw product surface after the MCP-only refactor.

## MCP Tools

- `localclaw_memory_create`
  - Creates a markdown note under `memory/`.
  - Inputs: `title`, `content`, optional `tags`, optional `agent_id`.
  - Returns the created path, timestamp, and index status.
- `localclaw_memory_search`
  - Ranked retrieval from indexed chunks.
  - Uses hybrid keyword + local vector ranking by default when vectors are ready.
  - Falls back to keyword ranking with a warning when embeddings are unavailable.
- `localclaw_memory_get`
  - Safe file read for known memory markdown paths.
  - Supports `from_line` and `lines`.
- `localclaw_memory_grep`
  - Literal or regex matching across in-scope memory markdown files.
  - Supports bounded matches and context lines.

## CLI

```bash
localclaw memory status
localclaw memory status --deep --index --json
localclaw memory index --force
localclaw memory search "incident summary"
localclaw memory search --mode vector "root cause"
localclaw memory grep "token-123"
localclaw memory model status
localclaw memory model install
```

The CLI is intended for local maintenance and diagnostics. Coding agents should normally use MCP tools.

## Index Model

Primary tables/features:

- `files`
- `chunks`
- optional `chunks_fts`
- `chunk_vectors`
- `meta`

Chunk content remains text source-of-truth. Vectors are cached in SQLite as normalized float32 BLOBs keyed by chunk and model id. Vector search uses exact local cosine scan in Go; no external vector database service or SQLite extension is required.

## Local Embeddings

Vector memory uses `embeddinggemma-300M-Q8_0.gguf` with managed `llama-server --embeddings`.

Default model metadata:

- File: `embeddinggemma-300M-Q8_0.gguf`
- Default path: `~/.localclaw/models/embeddinggemma-300M-Q8_0.gguf`
- SHA-256: `b5ce9d77a3fc4b3b39ccb5643c36777911cc4eb46a66962eadfa3f5f60490d63`
- Primary source: Hugging Face `ggml-org/embeddinggemma-300M-GGUF`
- Fallback mirror: `https://github.com/dgriffin831/embeddinggemma-gguf-mirror/raw/main/embeddinggemma-300M-Q8_0.gguf`

Install explicitly:

```bash
localclaw memory model install
```

If Hugging Face is unavailable:

```bash
localclaw memory model install --source mirror
```

Verify readiness:

```bash
localclaw memory model status
localclaw memory status --json
```

## Freshness

- `memory_create` refreshes the index after writing.
- `memory_search` auto-syncs only when `memory.sync.onSearch=true`.
- Use `localclaw memory index --force` to rebuild manually.
- No command downloads the embedding model unless `memory model install` is run explicitly.
