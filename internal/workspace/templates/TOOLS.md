---
title: TOOLS
---
# TOOLS.md - Local Tooling Notes

This file is for environment-specific notes.
It does not define tool availability; it records how tools are used in this workspace.

## Common Commands

```bash
go test ./...
go test ./internal/config
go test ./internal/runtime
go test ./internal/tui
go run ./cmd/localclaw check
go run ./cmd/localclaw tui
go fmt ./...
```

## Local Conventions

- Prefer `rg` for file/content search.
- Stage changes intentionally; avoid blind `git add .`.
- Keep commits focused and use conventional commit prefixes.

## Workspace-Specific Notes

Add practical details here, for example:

- Preferred shell aliases
- Non-default tool paths
- Channel account naming conventions (Slack/Signal)
- Repo-specific runbooks and shortcuts

## Safety Reminders

- Verify command impact before execution.
- Confirm before destructive operations.
- Avoid persisting secrets in workspace markdown files.
