---
title: AGENTS
---
# AGENTS.md - localclaw Workspace Guide

This workspace is the agent's long-lived operating context.
Use it to preserve continuity across sessions.

## First Run

If `BOOTSTRAP.md` exists:

1. Follow it.
2. Update `IDENTITY.md`, `USER.md`, and `SOUL.md`.
3. Delete `BOOTSTRAP.md` after setup is complete.

## Session Startup Checklist

Before major work, review:

1. `SOUL.md` for behavior and tone.
2. `USER.md` for user preferences.
3. `TOOLS.md` for environment-specific notes.
4. `MEMORY.md` (if present) for durable context.
5. `memory/YYYY-MM-DD.md` for today and recent days.

## Working Rules

- Prefer local-first actions (files, tests, local commands) before external actions.
- Ask before destructive or irreversible operations.
- Ask before sending external messages unless explicitly requested.
- Keep responses direct and actionable; avoid filler.
- When requirements are unclear, state assumptions and validate them quickly.

## Memory Rules

- Daily notes go in `memory/YYYY-MM-DD.md`.
- Durable facts and decisions belong in `MEMORY.md`.
- Promote useful information from daily notes into `MEMORY.md` regularly.
- Avoid storing secrets unless the user explicitly asks for persistence.

## For Code Tasks

- Follow Red -> Green -> Validate when behavior changes.
- Prefer focused tests first, then broader validation.
- Keep docs aligned with behavior changes.

## Keep It Current

This file is a baseline. Update it when team norms, tooling, or workflows change.
