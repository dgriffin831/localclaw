# Configuration Reference

`localclaw` uses JSON configuration loaded by `internal/config/config.go`.

## Loading Rules

- If `-config` is omitted, `Default()` config is used.
- If `-config` is provided, file content is merged into defaults via JSON unmarshal.
- Config always passes through `Validate()` before runtime startup.

## Default Configuration

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

## Validation Rules

General:
- `app.name` is required.
- `llm.provider` must be `claudecode`.
- `llm.claude_code.binary_path` is required.
- `channels.enabled` must include at least one channel.
- `heartbeat.interval_seconds` must be `> 0` when heartbeat is enabled.

Allowlists:
- `channels.enabled`: `slack`, `signal`.
- `llm.claude_code.auth_mode`: `default`, `aws_profile`, `bedrock`.

GovCloud:
- If `llm.claude_code.use_govcloud` is `true`, `llm.claude_code.bedrock_region` is required.

Local-only policy (hard guardrails):
- `security.enforce_local_only` must be `true`.
- `security.enable_gateway` must be `false`.
- `security.enable_http_server` must be `false`.
- `security.listen_address` must be empty.

## Change Checklist for New Config Fields

1. Add field to config structs.
2. Add default value in `Default()`.
3. Add validation in `Validate()` where needed.
4. Add or update tests in `internal/config/config_test.go`.
5. Update this doc and `AGENTS.md` if workflow implications change.
