# Configuration Reference

`localclaw` loads JSON configuration through `internal/config/config.go`.

## Loading rules

- If `-config` is omitted, `config.Default()` is used.
- If `-config` is provided, file JSON is decoded into defaults (merge-by-field behavior).
- Config decoding is strict: unknown/removed keys fail parsing.
- Config always passes `Validate()` before runtime startup.
- On startup, `App.Run` creates `~/.localclaw/localclaw.json` if missing.
  - This scaffold file is not auto-loaded unless passed via `-config`.

## Top-level schema

- `app`
- `llm`
- `channels`
- `agents`
- `session`
- `cron`
- `heartbeat`

## Default configuration

```json
{
  "app": {
    "name": "localclaw",
    "root": "~/.localclaw"
  },
  "llm": {
    "provider": "claudecode",
    "claude_code": {
      "binary_path": "claude",
      "profile": "default"
    },
    "codex": {
      "binary_path": "codex",
      "profile": "",
      "model": "",
      "extra_args": [],
      "mcp": {
        "config_path": "",
        "use_isolated_home": true,
        "home_path": "",
        "server_name": "localclaw"
      }
    }
  },
  "channels": {
    "enabled": ["slack", "signal"]
  },
  "agents": {
    "defaults": {
      "workspace": ".",
      "memory": {
        "enabled": true,
        "tools": {
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
          "maxResults": 8,
          "minScore": 0
        },
        "sync": {
          "onSearch": false,
          "sessions": {
            "deltaBytes": 32768,
            "deltaMessages": 20
          }
        }
      },
      "compaction": {
        "memoryFlush": {
          "enabled": true,
          "thresholdTokens": 28000,
          "triggerWindowTokens": 4000,
          "prompt": "",
          "timeoutSeconds": 20
        }
      }
    },
    "list": []
  },
  "session": {
    "store": "~/.localclaw/agents/{agentId}/sessions/sessions.json"
  },
  "cron": {
    "enabled": true
  },
  "heartbeat": {
    "enabled": true,
    "interval_seconds": 30
  }
}
```

## Validation rules

General:

- `app.name` is required.
- `app.root` is required.
- `app.thinking_messages` entries must be non-blank when provided.
- `llm.provider` must be `claudecode` or `codex`.
- `llm.claude_code.binary_path` is required when `llm.provider` is `claudecode`.
- `llm.codex.binary_path` is required when `llm.provider` is `codex`.
- `channels.enabled` must contain at least one value.
- `channels.enabled` allowlist: `slack`, `signal`.
- duplicate channel names are rejected.
- `agents.defaults.workspace` and `session.store` are required.
- each `agents.list[].id` is required and unique.
- `agents.list[].workspace` cannot be blank-whitespace.
- memory flush numeric fields must be non-negative:
  - `thresholdTokens`
  - `triggerWindowTokens`
  - `timeoutSeconds`
- if heartbeat is enabled, `heartbeat.interval_seconds` must be `> 0`.

Local-only boundary:

- `localclaw` does not expose config flags for gateway/listener behavior.
- Removed/deprecated config keys are rejected instead of silently accepted.
- Runtime remains single-process and local-only by architecture.

Codex-specific fields:

- `llm.codex.profile` optionally sets Codex profile (`-p`).
- `llm.codex.model` sets the default Codex model (`-m`) when no runtime override is present.
- `llm.codex.extra_args` appends provider-specific flags directly to `codex exec` arguments.

## Memory configuration notes

`memory` is defined on `agents.defaults` with optional per-agent overrides under `agents.list[].memory`.

Implementation details to be aware of:

- Runtime memory tool availability is controlled by `agents.defaults.memory` with optional per-agent overrides under `agents.list[].memory`.
  - `memory.enabled` gates all runtime/MCP memory tools.
  - `memory.tools.get`, `memory.tools.search`, and `memory.tools.grep` gate each memory tool individually.
  - All memory flags default to enabled.
  - Per-agent memory flags support explicit true/false overrides.
- Runtime memory indexing/search settings (`internal/runtime/tools.go`) are also read from `memory`:
  - `memory.sources`
  - `memory.extraPaths`
  - `memory.store.path`
  - `memory.chunking.{tokens,overlap}`
  - `memory.query.{maxResults,minScore}`
  - `memory.sync.onSearch`
  - `memory.sync.sessions.{deltaBytes,deltaMessages}`
- Runtime memory config resolution uses additive merge semantics for per-agent overrides.
  - Practical effect: override fields are applied when they are non-empty/non-zero/true.
  - Fields are not currently "explicitly unset" per-agent (for example, setting a bool to false does not force-disable a true default).
- Memory CLI (`internal/cli/memory.go`) currently uses `agents.defaults.memory` settings for index/search behavior.

Compatibility behavior:

- Removed/deprecated config keys are not supported.
- Removed runtime tool-policy keys:
  - top-level `tools`
  - top-level `skills`
  - `agents.defaults.tools`
  - `agents.defaults.skills`
  - `agents.list[].tools`
  - `agents.list[].skills`
- Update config files to the current schema before startup.

## Optional TUI waiting text

You can customize waiting text while status is `waiting` and no stream delta has arrived using `app.thinking_messages`.
If unset, fallback is `thinking`.

Example:

```json
{
  "app": {
    "thinking_messages": ["thinking", "checking memory", "drafting response"]
  }
}
```

## Change checklist for new config fields

1. Add field to config structs.
2. Add default in `Default()`.
3. Add validation in `Validate()` where relevant.
4. Add/update tests in `internal/config/config_test.go`.
5. Update this doc and any affected runtime/security docs.
