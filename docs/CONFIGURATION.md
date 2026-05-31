# Configuration

Config is strict JSON. Unknown fields fail parsing, including removed legacy sections such as `llm`, `channels`, `session`, `backup`, `cron`, `heartbeat`, and `skills`.

## Default Path

If `-config` is omitted, LocalClaw loads:

```text
~/.localclaw/localclaw.json
```

When runtime initialization runs and that file is missing, LocalClaw writes the active default config.

## Schema

Top-level sections:

- `app`
- `agents`

Minimal full example:

```json
{
  "app": {
    "name": "localclaw",
    "root": "~/.localclaw"
  },
  "agents": {
    "defaults": {
      "workspace": ".",
      "memory": {
        "enabled": true,
        "tools": {
          "create": true,
          "get": true,
          "search": true,
          "grep": true
        },
        "sources": ["memory"],
        "extraPaths": [],
        "store": {
          "path": "~/.localclaw/memory/{agentId}.sqlite"
        },
        "chunking": {
          "tokens": 400,
          "overlap": 40
        },
        "query": {
          "maxResults": 8
        },
        "sync": {
          "onSearch": false
        },
        "vector": {
          "enabled": true,
          "provider": "llama.cpp",
          "searchMode": "hybrid",
          "model": {
            "id": "embeddinggemma-300M-Q8_0",
            "path": "~/.localclaw/models/embeddinggemma-300M-Q8_0.gguf",
            "primaryUrl": "https://huggingface.co/ggml-org/embeddinggemma-300M-GGUF/resolve/0f741b5a6585bd53aeb15cd1372c56f2a0f65e12/embeddinggemma-300M-Q8_0.gguf",
            "mirrorUrl": "https://github.com/dgriffin831/embeddinggemma-gguf-mirror/raw/main/embeddinggemma-300M-Q8_0.gguf",
            "sha256": "b5ce9d77a3fc4b3b39ccb5643c36777911cc4eb46a66962eadfa3f5f60490d63"
          },
          "server": {
            "managed": true,
            "binary": "llama-server",
            "host": "127.0.0.1",
            "port": 0,
            "startupTimeoutSeconds": 20
          }
        }
      }
    },
    "list": []
  }
}
```

## Agent Overrides

`agents.list[]` entries may override workspace and memory settings:

```json
{
  "id": "audit",
  "workspace": "~/work/audit",
  "memory": {
    "query": {"maxResults": 12}
  }
}
```

Agent ids must be unique. Blank ids are rejected.

## Workspace Paths

- `agents.defaults.workspace` is required.
- `.` resolves under `app.root` as `workspace` for the default agent.
- Non-default agents using `.` resolve as `workspace-<agentId>`.
- `{agentId}` placeholders are supported in workspace and memory store paths.

## Memory Validation

- `memory.store.path` is required.
- `memory.chunking.tokens` must be `> 0`.
- `memory.chunking.overlap` must be `>= 0` and less than `tokens`.
- `memory.query.maxResults` must be `> 0`.
- Supported `memory.sources`: `memory`.
- `memory.extraPaths` may add extra markdown paths inside the workspace scope.
- `memory.vector.provider` must be `llama.cpp`.
- `memory.vector.searchMode` must be `hybrid`, `keyword`, or `vector`.
- `memory.vector.server.host` should remain loopback-only for local-only operation.

## Vector Memory

Vector memory is optional and local. `memory.vector.enabled=true` allows semantic indexing/search, but LocalClaw does not download a model automatically.

Install the default model explicitly:

```bash
localclaw memory model install
```

The installer tries Hugging Face first and then the GitHub mirror. If Hugging Face is unavailable, use:

```bash
localclaw memory model install --source mirror
```

The downloaded GGUF is verified against `memory.vector.model.sha256` before it is moved into place.

## Removed Sections

These sections are no longer accepted:

- `llm`
- `security`
- `channels`
- `session`
- `backup`
- `cron`
- `heartbeat`
- `skills`

Claude, Codex, and opencode own model execution, sessions, scheduling, and skills.
