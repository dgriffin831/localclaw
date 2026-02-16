# Embeddings Guide (Deprecated)

`localclaw` memory retrieval v2 removed embedding/vector runtime support.

Use:

- `memory_search` for keyword-ranked recall (FTS/LIKE fallback)
- `memory_grep` for exact literal/regex discovery

Legacy embedding-related config keys are ignored for compatibility and do not affect runtime behavior.
