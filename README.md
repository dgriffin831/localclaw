# localclaw

`localclaw` is a local-only stdio MCP server for persistent coding-agent workspace and markdown memory.

It does not run a chat UI, call model APIs, or manage Claude/Codex/opencode sessions. Those coding agents remain the harnesses; `localclaw` gives them a durable workspace, behavior-control markdown files, and memory tools over MCP.

## Key Features

- Local-only, single-process CLI.
- Stdio MCP server: `localclaw mcp serve`.
- Persistent workspace bootstrap with files such as `AGENTS.md`, `SOUL.md`, `TOOLS.md`, and `MEMORY.md`.
- SQLite-backed markdown memory create/search/get/grep.
- Optional fully-local vector memory using EmbeddingGemma GGUF with managed `llama-server`.
- Harness-driven setup for Claude, Codex, and opencode.
- Strict config decoding so removed legacy sections fail fast.

## Quick Start

Prerequisites:

- Go `1.24.2+`
- Optional coding-agent harnesses on `PATH`: `claude`, `codex`, and/or `opencode`

Run from source:

```bash
go test ./...
go run ./cmd/localclaw doctor
go run ./cmd/localclaw mcp serve
```

Ask a harness to configure LocalClaw MCP:

```bash
go run ./cmd/localclaw setup claude
go run ./cmd/localclaw setup codex
go run ./cmd/localclaw setup opencode
```

Preview the setup prompt without invoking a harness:

```bash
go run ./cmd/localclaw setup opencode --dry-run
```

Memory examples:

```bash
go run ./cmd/localclaw memory status
go run ./cmd/localclaw memory index --force
go run ./cmd/localclaw memory search "incident summary"
go run ./cmd/localclaw memory search --mode vector "incident summary"
go run ./cmd/localclaw memory grep "token-123"
go run ./cmd/localclaw memory model status
go run ./cmd/localclaw memory model install
```

If `-config` is omitted, `localclaw` loads `~/.localclaw/localclaw.json` when present; otherwise defaults are used. Commands that initialize runtime state also create `~/.localclaw/localclaw.json` when missing.

## Configuration Basics

Top-level config sections:

- `app`
- `agents`

Minimal example:

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
        "tools": {"create": true, "get": true, "search": true, "grep": true},
        "sources": ["memory"],
        "extraPaths": [],
        "store": {"path": "~/.localclaw/memory/{agentId}.sqlite"},
        "chunking": {"tokens": 400, "overlap": 40},
        "query": {"maxResults": 8},
        "sync": {"onSearch": false},
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
          "server": {"managed": true, "binary": "llama-server", "host": "127.0.0.1", "port": 0, "startupTimeoutSeconds": 20}
        }
      }
    },
    "list": []
  }
}
```

## Command Modes

- `localclaw doctor` - startup and workspace diagnostics.
- `localclaw memory <status|index|search|grep|model>` - memory maintenance and retrieval.
- `localclaw memory model <status|install>` - local EmbeddingGemma GGUF management.
- `localclaw mcp serve` - stdio MCP server.
- `localclaw setup <claude|codex|opencode>` - ask the harness to configure LocalClaw MCP.

## Documentation

- [Documentation index](docs/README.md)
- [Upgrade guide](UPGRADE.md)
- [Runtime lifecycle and command behavior](docs/RUNTIME.md)
- [Configuration reference](docs/CONFIGURATION.md)
- [Installation guide](docs/INSTALL.md)
- [Tools and MCP architecture](docs/TOOLS.md)
- [Memory model and workflows](docs/MEMORY.md)
- [Security boundaries](docs/SECURITY.md)
- [Testing guide](docs/TESTING.md)

## Notes

- Local-only boundary is intentional: no HTTP/gRPC gateway mode.
- Setup commands invoke the selected harness with an instruction prompt; they do not edit harness config files directly.
- The embedding model is downloaded only by explicit `localclaw memory model install`; use `--source mirror` if Hugging Face is unavailable.
- See repository workflow/testing expectations in [`AGENTS.md`](AGENTS.md).
