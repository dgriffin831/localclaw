# Security

## Default posture

Deny by default for non-local runtime behavior.

- Local-only enforcement is enabled by default.
- Any config enabling server/gateway/listen behavior fails startup.
- Channels are restricted to `slack` and `signal` identifiers.
- LLM provider is constrained to `claudecode` in MVP.

## Explicitly out of scope

- No HTTP/gRPC server mode.
- No open ports/listeners.
- No browser-hosted execution surface.
- No node host/gateway process.

## AWS GovCloud Bedrock

GovCloud support is handled through Claude Code CLI-compatible configuration and environment handling. `localclaw` does not implement a custom network server for Bedrock access.

## Security controls roadmap

- Signed config and immutable policy profile.
- Least-privilege subprocess execution profile for Claude Code CLI.
- Filesystem ACL hardening and audit trails.
