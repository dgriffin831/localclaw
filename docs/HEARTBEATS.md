# Heartbeats

This guide explains LocalClaw heartbeat runs: what they do, how they are scheduled, and how to author `HEARTBEAT.md` effectively.

## What Heartbeat Does

When enabled, LocalClaw starts a background heartbeat loop after runtime startup.

On each tick it:

1. Resolves the default workspace.
2. Checks that `HEARTBEAT.md` is readable in that workspace.
3. Sends a local LLM prompt in session `default/main` that explicitly references `HEARTBEAT.md`.

Default heartbeat prompt body:

`Read HEARTBEAT.md if it exists (workspace context). Follow it strictly. Do not infer or repeat old tasks from prior chats. If nothing needs attention, reply HEARTBEAT_OK.`

## Scheduling and Lifecycle

Heartbeat scheduling uses existing config:

- `heartbeat.enabled`
- `heartbeat.interval_seconds`

Behavior contract:

- If `heartbeat.enabled=false`, no loop starts.
- If enabled, ticks run every `heartbeat.interval_seconds`.
- If a previous heartbeat run is still active, the next tick is skipped (no overlap).
- If a tick fails, the error is logged and future ticks continue.
- The loop stops automatically when runtime context is canceled.

## Configuration

Example:

```json
{
  "heartbeat": {
    "enabled": true,
    "interval_seconds": 60
  }
}
```

Validation:

- If heartbeat is enabled, `heartbeat.interval_seconds` must be `> 0`.

## Runtime Modes

Heartbeats only run while runtime is alive.

Practical implications:

- Long-running modes (for example `tui`, `mcp serve`, `channels serve`) can execute recurring heartbeats.
- Short-lived modes (for example `doctor`) usually exit before interval ticks fire.

## Authoring `HEARTBEAT.md`

Use short, stable checklist items that are safe to run repeatedly.

Example:

```markdown
# HEARTBEAT.md

- Check for urgent Slack messages in #ops.
- Review today’s calendar and flag conflicts.
- Check open local cron jobs and alert on recent failures.
- If nothing needs attention, respond HEARTBEAT_OK.
```

Guidelines:

- Keep it concise to reduce token cost.
- Prefer explicit instructions over broad goals.
- Avoid secrets and credentials in workspace files.
- Batch periodic checks here rather than creating many tiny schedules.

## Observability and Troubleshooting

Useful logs:

- missing/unreadable file: `heartbeat: skipped tick; unable to read .../HEARTBEAT.md`
- overlap skip: `heartbeat: skipped tick while previous run is active`
- tick failure: `heartbeat: run failed: ...`

If heartbeat appears idle:

1. Confirm `heartbeat.enabled=true`.
2. Confirm `heartbeat.interval_seconds` is positive.
3. Confirm runtime is in a long-running mode.
4. Confirm `HEARTBEAT.md` exists and is readable in default workspace.
5. Check logs for skip/error messages.

## Related Docs

- `docs/RUNTIME.md`
- `docs/CONFIGURATION.md`
- `docs/specs/heartbeat.md`
