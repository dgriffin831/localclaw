# Configuration Reference

`localclaw` loads JSON configuration through `internal/config/config.go`.

## Loading rules

- If `-config` is omitted, `config.Default()` is used.
- If `-config` is provided, file JSON is unmarshaled into defaults (merge-by-field behavior).
- Config always passes `Validate()` before runtime startup.
- On startup, `App.Run` creates `~/.localclaw/localclaw.json` if missing.
  - This scaffold file is not auto-loaded unless passed via `-config`.

## Top-level schema

- `app`
- `security`
- `llm`
- `channels`
- `state`
- `agents`
- `session`
- `memory`
- `workspace`
- `cron`
- `heartbeat`

## Default configuration

```json
{
  "app": {
    "name": "localclaw"
  },
  "security": {
    "enforce_local_only": true,
    "enable_gateway": false,
    "enable_http_server": false,
    "listen_address": ""
  },
  "llm": {
    "provider": "claudecode",
    "claude_code": {
      "binary_path": "claude",
      "profile": "default",
      "use_govcloud": false,
      "bedrock_region": "",
      "auth_mode": "default"
    }
  },
  "channels": {
    "enabled": ["slack", "signal"]
  },
  "state": {
    "root": "~/.localclaw"
  },
  "agents": {
    "defaults": {
      "workspace": ".",
      "memorySearch": {
        "enabled": false,
        "sources": ["memory"],
        "extraPaths": [],
        "provider": "auto",
        "fallback": "none",
        "model": "",
        "store": {
          "path": "~/.localclaw/memory/{agentId}.sqlite",
          "vector": {
            "enabled": true
          }
        },
        "chunking": {
          "tokens": 400,
          "overlap": 40
        },
        "query": {
          "maxResults": 8,
          "minScore": 0,
          "hybrid": {
            "enabled": true,
            "vectorWeight": 0.8,
            "keywordWeight": 0.2,
            "candidateMultiplier": 4
          }
        },
        "sync": {
          "onSessionStart": false,
          "onSearch": false,
          "watch": false,
          "watchDebounceMs": 500,
          "intervalMinutes": 0,
          "sessions": {
            "deltaBytes": 32768,
            "deltaMessages": 20
          }
        },
        "cache": {
          "enabled": true,
          "maxEntries": 1000
        },
        "local": {
          "modelPath": "",
          "modelCacheDir": ""
        },
        "remote": {
          "baseURL": "",
          "apiKey": "",
          "headers": {},
          "batch": {
            "enabled": false,
            "size": 16
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
  "memory": {
    "path": ".localclaw/memory.json"
  },
  "workspace": {
    "root": "."
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

## Compatibility mappings

`applyCompatibilityMappings()` currently performs:

- `workspace.root` -> `agents.defaults.workspace` when the new field is unset/defaulted.
- `agents.defaults.workspace` -> `workspace.root` when legacy field is empty.
- `memory.path` -> `agents.defaults.memorySearch.legacyImportPath` when unset.
- If `state.root` changes and derived defaults are still untouched:
  - rebase `session.store`
  - rebase `agents.defaults.memorySearch.store.path`

Notes:

- `memory.path` is legacy compatibility metadata only in current runtime.
- Startup does not automatically import legacy JSON memory into `MEMORY.md`.

## Validation rules

General:

- `app.name` is required.
- `app.thinking_messages` entries must be non-blank when provided.
- `llm.provider` must be `claudecode`.
- `llm.claude_code.binary_path` is required.
- `llm.claude_code.auth_mode` allowlist: `default`, `aws_profile`, `bedrock`.
- if `llm.claude_code.use_govcloud=true`, `llm.claude_code.bedrock_region` is required.
- `channels.enabled` must contain at least one value.
- `channels.enabled` allowlist: `slack`, `signal`.
- duplicate channel names are rejected.
- `state.root`, `agents.defaults.workspace`, and `session.store` are required.
- each `agents.list[].id` is required and unique.
- `agents.list[].workspace` cannot be blank-whitespace.
- memory flush numeric fields must be non-negative:
  - `thresholdTokens`
  - `triggerWindowTokens`
  - `timeoutSeconds`
- if heartbeat is enabled, `heartbeat.interval_seconds` must be `> 0`.

Local-only hard guardrails (`ValidateLocalOnlyPolicy`):

- `security.enforce_local_only` must stay `true`.
- `security.enable_gateway` must stay `false`.
- `security.enable_http_server` must stay `false`.
- `security.listen_address` must stay empty.

## Memory-search configuration notes

`memorySearch` is defined on `agents.defaults` with optional per-agent values under `agents.list[].memorySearch`.

Implementation details to be aware of:

- Runtime tooling (`internal/runtime/tools.go`) resolves per-agent overrides with additive merge semantics.
  - Practical effect: override fields are applied when they are non-empty/non-zero/true.
  - Fields are not currently "explicitly unset" per-agent (for example, setting a bool to false does not force-disable a true default).
- Memory CLI (`internal/cli/memory.go`) currently uses `agents.defaults.memorySearch` settings for index/search behavior.

Provider values:

- Config accepts provider/fallback strings without strict validation.
- Memory manager currently supports local-only embedding modes: `none` and `local`.
- Unsupported provider values fail when memory manager resolves embedding provider.

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
