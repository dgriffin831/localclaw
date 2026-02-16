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
- `tools`
- `skills`
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
      "profile": "default"
    },
    "codex": {
      "binary_path": "codex",
      "profile": "",
      "model": "",
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
  "state": {
    "root": "~/.localclaw"
  },
  "agents": {
    "defaults": {
      "workspace": ".",
      "tools": {
        "delegated": {
          "enabled": false
        }
      },
      "skills": {},
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
          "onSearch": false,
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
          "runtimePath": "",
          "modelPath": "",
          "modelCacheDir": "",
          "queryTimeoutSeconds": 0,
          "batchTimeoutSeconds": 0
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
  "tools": {
    "delegated": {
      "enabled": false
    }
  },
  "skills": {},
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
- `app.thinking_messages` entries must be non-blank when provided.
- `llm.provider` must be `claudecode` or `codex`.
- `llm.claude_code.binary_path` is required.
- `llm.codex.binary_path` is required when `llm.provider` is `codex`.
- `channels.enabled` must contain at least one value.
- `channels.enabled` allowlist: `slack`, `signal`.
- duplicate channel names are rejected.
- `state.root`, `agents.defaults.workspace`, and `session.store` are required.
- tool/skill policy name lists reject blank and duplicate entries:
  - `tools.allow`, `tools.deny`
  - `tools.delegated.allow`, `tools.delegated.deny`
  - `skills.enabled`, `skills.disabled`
  - same validations also apply under `agents.defaults.*` and `agents.list[].*` overrides
- each `agents.list[].id` is required and unique.
- `agents.list[].workspace` cannot be blank-whitespace.
- memory flush numeric fields must be non-negative:
  - `thresholdTokens`
  - `triggerWindowTokens`
  - `timeoutSeconds`
- local embedding timeouts must be non-negative:
  - `agents.defaults.memorySearch.local.queryTimeoutSeconds`
  - `agents.defaults.memorySearch.local.batchTimeoutSeconds`
  - same constraint applies to `agents.list[].memorySearch.local.*` overrides
- `memorySearch.local.runtimePath` cannot be whitespace-only when set.
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
- When `provider=local` and fallback also requires local embeddings (for example `fallback=local`), `memorySearch.local.runtimePath` must point to an executable embedding runtime.
- `memorySearch.local.modelPath` is currently validated as a file path (directory values are rejected).
- See `docs/EMBEDDINGS.md` for local runtime + model setup.

## Tool policy configuration notes

`tools` can be configured globally, under `agents.defaults.tools`, and per agent under `agents.list[].tools`.

- Policy precedence: global -> agent defaults -> specific agent.
- Evaluation order:
  - normalize tool name
  - deny match blocks
  - allowlist applies when non-empty
- Delegated tools (`class=delegated`) are disabled by default.
- Delegated tools must pass both:
  - `tools.delegated.enabled=true`
  - delegated allowlist match (`tools.delegated.allow`)

Supported list semantics:

- exact tool name matches (`memory_search`)
- wildcard match (`*`)

## Skills configuration notes

`skills` can be configured globally, under `agents.defaults.skills`, and per agent under `agents.list[].skills`.

- Workspace skills are discovered from `skills/<name>/SKILL.md`.
- Frontmatter fields currently parsed:
  - `name`
  - `description`
  - `user-invocable` (default `true`)
  - `disable-model-invocation` (default `false`)
- Eligibility filters:
  - `skills.disabled` always excludes a skill
  - when `skills.enabled` is non-empty, only those names are eligible
- Skills with `disable-model-invocation=true` are excluded from the model-facing skills prompt block.

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
