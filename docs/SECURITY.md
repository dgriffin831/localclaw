# Security

LocalClaw is local-only and stdio-only.

## Boundaries

- No HTTP, gRPC, gateway, or listener runtime.
- No direct model API clients.
- No LocalClaw-owned chat execution.
- No channel adapters or outbound messaging integrations.
- MCP transport is stdin/stdout through `localclaw mcp serve`.

Claude, Codex, and opencode own their own model/network behavior when they invoke LocalClaw through MCP.

## Filesystem Scope

- Workspace paths are resolved from config and created locally.
- Workspace read tools allow only known control markdown files and `memory/**/*.md`.
- Memory tools are scoped to markdown memory files and configured extra paths.
- Path traversal and out-of-workspace reads are rejected.
- Memory writes are restricted to `memory/` or `memory.md`.

## Setup

`localclaw setup <harness>` invokes the selected harness with a prompt asking it to configure LocalClaw MCP. LocalClaw does not parse or edit harness config files directly.

## Secrets

Do not store secrets in workspace control files or memory files. These files are intended to be read by coding agents.
