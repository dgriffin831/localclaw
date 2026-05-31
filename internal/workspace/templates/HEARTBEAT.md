---
title: HEARTBEAT
---
# HEARTBEAT.md

LocalClaw no longer runs heartbeat scheduling.
Use this file only if an external harness or agent workflow reads it.

## Default Rule

If there is nothing actionable, return `HEARTBEAT_OK`.

## Suggested Checks

- [ ] Review urgent TODO/FIXME items in active projects.
- [ ] Check recent test or lint failures that can be fixed safely.
- [ ] Refresh short-term notes in `memory/YYYY-MM-DD.md`.
- [ ] Promote high-value context to `MEMORY.md` when appropriate.
- [ ] Flag time-sensitive issues for proactive user follow-up.

## Guardrails

- Do only actions that are safe without extra confirmation.
- Avoid destructive or externally visible actions unless explicitly requested.
- Remove stale checklist items that no longer matter.
