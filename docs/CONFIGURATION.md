# Configuration Reference

`localclaw` loads JSON configuration through `internal/config/config.go`.

## Loading rules

- If `-config` is omitted, loader behavior is:
  - if `~/.localclaw/localclaw.json` exists, load it;
  - otherwise use `config.Default()`.
- If `-config` is provided, file JSON is decoded into defaults (merge-by-field behavior).
- Config decoding is strict: unknown/removed keys fail parsing.
- Config always passes `Validate()` before runtime startup.
- On startup, `App.Run` creates `~/.localclaw/localclaw.json` if missing.
  - On later runs, this file is auto-loaded when `-config` is omitted.

## Top-level schema

- `app`
- `security`
- `llm`
- `channels`
- `agents`
- `session`
- `backup`
- `cron`
- `heartbeat`

## Default configuration

```json
{
  "app": {
    "name": "localclaw",
    "root": "~/.localclaw",
    "default": {
      "verbose": false,
      "mouse": false,
      "tools": false
    }
  },
  "security": {
    "mode": "sandbox-write"
  },
  "llm": {
    "provider": "claudecode",
    "claude_code": {
      "binary_path": "claude",
      "profile": "default",
      "extra_args": [
        "--allowed-tools",
        "mcp__localclaw__localclaw_memory_search,mcp__localclaw__localclaw_memory_get,mcp__localclaw__localclaw_memory_grep,mcp__localclaw__localclaw_workspace_status,mcp__localclaw__localclaw_cron_list,mcp__localclaw__localclaw_cron_add,mcp__localclaw__localclaw_cron_remove,mcp__localclaw__localclaw_cron_run,mcp__localclaw__localclaw_sessions_list,mcp__localclaw__localclaw_sessions_history,mcp__localclaw__localclaw_sessions_delete,mcp__localclaw__localclaw_session_status,mcp__localclaw__localclaw_slack_send,mcp__localclaw__localclaw_signal_send"
      ],
      "session_mode": "always",
      "session_arg": "--session-id",
      "resume_args": ["--resume", "{sessionId}"],
      "session_id_fields": ["session_id", "sessionId", "conversation_id", "conversationId"]
    },
    "codex": {
      "binary_path": "codex",
      "profile": "",
      "model": "",
      "reasoning_default": "medium",
      "extra_args": ["--skip-git-repo-check"],
      "session_mode": "existing",
      "resume_args": ["resume", "{sessionId}"],
      "session_id_fields": ["thread_id", "threadId", "session_id", "sessionId"],
      "resume_output": "json",
      "mcp": {
        "config_path": "",
        "server_name": "localclaw"
      }
    }
  },
  "channels": {
    "enabled": ["slack", "signal"],
    "slack": {
      "bot_token_env": "SLACK_BOT_TOKEN",
      "default_channel": "",
      "api_base_url": "https://slack.com/api",
      "timeout_seconds": 10
    },
    "signal": {
      "cli_path": "signal-cli",
      "account": "+10000000000",
      "default_recipient": "",
      "timeout_seconds": 10,
      "inbound": {
        "enabled": false,
        "allow_from": [],
        "agent_by_sender": {},
        "default_agent": "default",
        "send_typing": true,
        "typing_interval_seconds": 5,
        "send_read_receipts": true,
        "poll_timeout_seconds": 5,
        "max_messages_per_poll": 10
      }
    }
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
          "maxResults": 8
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
  "backup": {
    "auto_save": true,
    "auto_clean": true,
    "interval": "1d",
    "retain_count": 3
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
- `app.default` controls TUI startup flags:
  - `verbose`: initial verbose diagnostics (`false` default)
  - `mouse`: initial mouse capture (`false` default)
  - `tools`: initial tool-card expansion (`false` default)
- `app.thinking_messages` entries must be non-blank when provided.
- `llm.provider` must be `claudecode` or `codex`.
- `security.mode` must be one of:
  - `full-access`
  - `sandbox-write`
  - `read-only`
- `llm.claude_code.binary_path` is required when `llm.provider` is `claudecode`.
- `llm.codex.binary_path` is required when `llm.provider` is `codex`.
- `llm.claude_code.session_mode` and `llm.codex.session_mode` must be `always`, `existing`, or `none`.
- for `session_mode=existing`, configured `resume_args` must include `{sessionId}`.
- `llm.claude_code.session_id_fields[]` and `llm.codex.session_id_fields[]` entries cannot be blank.
- `llm.codex.resume_output` must be one of `json`, `jsonl`, or `text` when set.
- `llm.codex.reasoning_default` is required and must be one of `xlow`, `low`, `medium`, `high`, `xhigh`.
- provider `extra_args` cannot contain security-managed flags.
  - configure execution mode using `security.mode` instead.
  - rejected Codex flags in `llm.codex.extra_args`: `--dangerously-bypass-approvals-and-sandbox`, `--yolo`, `--sandbox`, `--add-dir`.
  - rejected Claude Code flags in `llm.claude_code.extra_args`: `--dangerously-skip-permissions`, `--permission-mode`, `--add-dir`.
- `channels.enabled` must contain at least one value.
- `channels.enabled` allowlist: `slack`, `signal`.
- duplicate channel names are rejected.
- when `slack` is enabled:
  - `channels.slack.bot_token_env` is required.
  - `channels.slack.api_base_url` is required.
  - `channels.slack.timeout_seconds` must be `> 0`.
- when `signal` is enabled:
  - `channels.signal.cli_path` is required.
  - `channels.signal.account` is required.
  - `channels.signal.timeout_seconds` must be `> 0`.
- when `channels.signal.inbound.enabled=true`:
  - `channels.signal.inbound.allow_from` must contain at least one E.164 sender.
  - `channels.signal.inbound.default_agent` must be `default` or a configured `agents.list[].id`.
  - `channels.signal.inbound.agent_by_sender` senders must also appear in `allow_from`.
  - `channels.signal.inbound.agent_by_sender` agents must be valid agent ids.
  - `channels.signal.inbound.typing_interval_seconds` must be `> 0`.
  - `channels.signal.inbound.poll_timeout_seconds` must be `> 0`.
  - `channels.signal.inbound.max_messages_per_poll` must be `> 0`.
- `agents.defaults.workspace` and `session.store` are required.
- `backup.interval` must be a valid positive duration (for example `30m`, `12h`, `1d`).
- `backup.retain_count` must be `> 0`.
- each `agents.list[].id` is required and unique.
- `agents.list[].workspace` cannot be blank-whitespace.
- memory flush numeric fields must be non-negative:
  - `thresholdTokens`
  - `triggerWindowTokens`
  - `timeoutSeconds`
- `cron.enabled` toggles scheduler startup and MCP cron methods.
- if heartbeat is enabled, `heartbeat.interval_seconds` must be `> 0`.

Local-only boundary:

- `localclaw` does not expose config flags for gateway/listener behavior.
- Removed/deprecated config keys are rejected instead of silently accepted.
- Runtime remains single-process and local-only by architecture.

Security mode behavior:

- `security.mode` is the canonical execution-safety contract.
- mode translation is deterministic per provider:
  - `full-access`
    - Codex: `--dangerously-bypass-approvals-and-sandbox`
    - Claude Code: `--dangerously-skip-permissions`
  - `sandbox-write`
    - Codex: `--sandbox workspace-write --add-dir <resolved-workspace-path>`
    - Claude Code: `--permission-mode acceptEdits --add-dir <resolved-workspace-path>`
  - `read-only`
    - Codex: `--sandbox read-only --add-dir <resolved-workspace-path>`
    - Claude Code: `--permission-mode plan --add-dir <resolved-workspace-path>`
- workspace allowlisting uses the resolved runtime workspace path for the active agent/session.
- if workspace resolution fails, prompt execution fails with wrapped context.

Codex-specific fields:

- `llm.codex.profile` optionally sets Codex profile (`-p`).
- `llm.codex.model` sets the default Codex model (`-m`) when no runtime override is present.
- `llm.codex.reasoning_default` sets the default Codex reasoning level used when selector input omits reasoning.
- `llm.codex.extra_args` appends provider-specific flags directly to `codex exec` arguments.
  - default includes `--skip-git-repo-check` so Codex runs in non-git/trust-unregistered directories.
  - security-managed flags are rejected here; use `security.mode` for sandbox/permission posture.
- `llm.codex.session_mode` controls continuation behavior:
  - `existing`: resume when a persisted provider session exists
  - `always`: same as `existing` for resume, otherwise start new
  - `none`: disable provider session continuation flags
- `llm.codex.resume_args` controls resume argument templates and supports `{sessionId}` placeholder.
- `llm.codex.session_id_fields` controls JSON fields scanned for provider session IDs.
- `llm.codex.resume_output` controls resume parsing mode (`json` default).
  - use `text` only as a compatibility fallback when your Codex CLI/version cannot stream JSON on resume; note this disables structured tool-call/result events on resumed turns.
- `llm.codex.mcp.config_path` optionally points to a specific Codex `config.toml`; otherwise Codex defaults are used (`$CODEX_HOME/config.toml` when set, else `~/.codex/config.toml`).

Claude Code-specific continuation fields:

- `llm.claude_code.extra_args` appends provider-specific flags directly to `claude` arguments.
  - default includes `--allowed-tools` with LocalClaw MCP tools so memory/workspace/session/cron/channel tools work without first-run permission prompts.
  - security-managed flags are rejected here; use `security.mode` for sandbox/permission posture.
- `llm.claude_code.session_mode` controls continuation behavior (`always` default).
- `llm.claude_code.session_arg` controls new-session flag (default `--session-id`).
- `llm.claude_code.resume_args` controls resume argument templates and supports `{sessionId}` placeholder.
- `llm.claude_code.session_id_fields` controls JSON fields scanned for provider session IDs.

## Channel configuration notes

Slack (`channels.slack`):

- `bot_token_env`: env var name for Slack bot token lookup at send time.
- `default_channel`: fallback destination when `localclaw_slack_send` omits `channel`.
- `api_base_url`: Slack Web API base URL (default `https://slack.com/api`).
- `timeout_seconds`: request timeout for `chat.postMessage`.

Signal (`channels.signal`):

- `cli_path`: executable path for `signal-cli`.
- `account`: sender account passed to `signal-cli -a`.
- `default_recipient`: fallback destination when `localclaw_signal_send` omits `recipient`.
- `timeout_seconds`: subprocess timeout for send calls.
- `inbound.enabled`: enables inbound Signal polling worker mode (`channels serve`).
- `inbound.allow_from`: allowlist of direct sender numbers permitted to trigger agent runs.
- `inbound.agent_by_sender`: optional sender -> agent routing map.
- `inbound.default_agent`: fallback agent for allowlisted senders without explicit mapping.
- `inbound.send_typing`: send Signal typing indicators while localclaw prepares a reply.
- `inbound.typing_interval_seconds`: typing refresh cadence while a reply is running.
- `inbound.send_read_receipts`: send Signal read receipts for accepted inbound direct messages.
- `inbound.poll_timeout_seconds`: `signal-cli receive` timeout per poll.
- `inbound.max_messages_per_poll`: max messages consumed per receive poll.
- group messages are always dropped in inbound mode.

MCP channel tools:

- `localclaw_slack_send` (required `text`; optional `channel`, `thread_id`, `agent_id`, `session_id`)
- `localclaw_signal_send` (required `text`; optional `recipient`, `agent_id`, `session_id`)

## Cron configuration notes

- `cron.enabled=true` starts the in-process scheduler during `App.Run`.
- persisted store path is `<app.root>/cron/jobs.json`.
- supported schedules:
  - 5-field cron (`minute hour day-of-month month day-of-week`) with `*`, `,`, `-`, `/`, and integer values
  - macros: `@yearly`, `@annually`, `@monthly`, `@weekly`, `@daily`, `@hourly`, `@reboot`
- jobs run local agent prompts using `message` + optional `timeout_seconds`.
- cron `session_target` values are `default` and `isolated` (default is `isolated`).
- jobs run only while runtime is active; missed windows while offline are not backfilled.

## Backup configuration notes

- `backup.auto_save=true` enables periodic archive creation in long-running command modes.
- `backup.auto_clean=true` enables periodic retention cleanup in long-running command modes.
- `backup.interval` sets the cadence for auto-save and auto-clean loops.
  - supported formats include Go durations (`30m`, `12h`) and day shorthand (`1d`).
- `backup.retain_count` keeps the newest matching archives in `<app.root>/backups`.
- archive filenames use `localclaw-backup-YYYYMMDD-HHMMSSZ.tar.gz`.
- each archive contains normalized paths for:
  - `localclaw.json`
  - `cron/`
  - `workspace/`
  - `workspace-<agentId>/` for configured agents

## Heartbeat configuration notes

- `heartbeat.enabled=true` starts recurring heartbeat ticks during `App.Run`.
- `heartbeat.interval_seconds` sets the tick cadence in seconds.
- each tick runs a local prompt in the default agent/session and references workspace `HEARTBEAT.md`.
- missing or unreadable `HEARTBEAT.md` skips that tick (logged) and later ticks continue.

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
  - `memory.query.maxResults`
  - `memory.sync.onSearch`
  - `memory.sync.sessions.{deltaBytes,deltaMessages}`
- Runtime memory config resolution uses additive merge semantics for per-agent overrides.
  - `memory.enabled` and `memory.tools.{get,search,grep}` support explicit `true/false` overrides.
  - Non-bool scalar fields still merge using non-empty/non-zero rules.
  - `memory.sync.onSearch` currently supports explicit enable only (cannot force-disable a true inherited value).
- Memory CLI (`internal/cli/memory.go`) currently uses `agents.defaults.memory` settings for index/search behavior.

Compatibility behavior:

- Removed/deprecated config keys are not supported.
- Removed app defaults key:
  - `app.default.thinking`
- Removed Codex MCP isolated-home keys:
  - `llm.codex.mcp.use_isolated_home`
  - `llm.codex.mcp.home_path`
- Removed Codex continuation key:
  - `llm.codex.session_arg`
- Removed memory query key:
  - `agents.defaults.memory.query.minScore`
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

## Optional TUI startup defaults

You can set startup defaults for common TUI toggles under `app.default`.

Example:

```json
{
  "app": {
    "default": {
      "verbose": false,
      "mouse": false,
      "tools": false
    }
  }
}
```

## Change checklist for new config fields

1. Add field to config structs.
2. Add default in `Default()`.
3. Add validation in `Validate()` where relevant.
4. Add/update tests in `internal/config/config_test.go`.
5. Update this doc and any affected runtime/security docs.
