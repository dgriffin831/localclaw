# Security

## Default posture

`localclaw` is deny-by-default for non-local runtime behavior.

- Local-only enforcement is enabled by default.
- Gateway/server/listener behavior is not configurable.
- Channels are restricted to `slack` and `signal` identifiers.
- LLM providers are constrained to local CLI subprocess invocation (`claudecode` or `codex`).
- Runtime uses MCP-first provider execution; host no longer runs a legacy local tool-call execution loop.
- Signal inbound processing is local subprocess polling only (no inbound network listeners/webhooks).

## Enforced guardrails

- Runtime remains single-process CLI only.
- No HTTP/gRPC server mode exists.
- No gateway/listener config surface exists.
- LLM access remains local subprocess execution only.
- Signal delivery remains local subprocess execution via `signal-cli`.
- Inbound Signal execution requires explicit sender allowlisting.
- Inbound Signal group messages are always denied.

## Process and network boundary

- Runtime is a single CLI process.
- No HTTP/gRPC server mode.
- No open ports/listeners.
- No browser-hosted execution surface.
- No Node host/gateway process.
- Slack delivery performs outbound HTTPS calls to Slack Web API only.
- Signal inbound polling uses local `signal-cli receive` subprocess calls only.

## Filesystem and state controls

- State defaults under `~/.localclaw`.
- Session store files are written with hardened permissions where supported (`0600` files, `0700` session dirs).
- Session writes use lock files plus atomic replace behavior.
- Memory file reads through `memory_get` are restricted to allowed markdown sources.
- `memory_grep` scans only discovered in-scope memory/session files and rejects out-of-scope traversal globs.

## LLM execution boundary

- Claude Code and Codex integrations are local subprocess only (`exec.CommandContext`).
- No direct model HTTP client is implemented in `localclaw`.
- Claude subprocess environment inherits parent process variables (with optional profile override).
- Prompt streaming requires request-capable provider adapters; no compatibility fallback path is used.

## Tool boundary

- Local capabilities are exposed via the MCP server (`localclaw mcp serve`).
- Provider-native tools remain provider-owned (`provider_native` in UI/event ownership).
- localclaw does not execute a legacy host-managed structured tool loop in the prompt stream path.

## Explicitly out of scope

- Hosted gateway/server runtime.
- Remote tool bridges requiring inbound listeners.
- Embedded browser automation surfaces.

## Security controls roadmap

- Signed config and immutable policy profiles.
- Least-privilege subprocess execution profile for Claude Code CLI.
- Filesystem ACL hardening and audit trails.
