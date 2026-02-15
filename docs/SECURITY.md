# Security

## Default posture

`localclaw` is deny-by-default for non-local runtime behavior.

- Local-only enforcement is enabled by default.
- Any config enabling gateway/server/listener behavior fails startup.
- Channels are restricted to `slack` and `signal` identifiers.
- LLM provider is constrained to local Claude Code CLI subprocess invocation.

## Enforced guardrails

Validated at startup (`ValidateLocalOnlyPolicy`):

- `security.enforce_local_only` must be `true`
- `security.enable_gateway` must be `false`
- `security.enable_http_server` must be `false`
- `security.listen_address` must be empty

Guardrail violations fail startup before runtime initialization.

## Process and network boundary

- Runtime is a single CLI process.
- No HTTP/gRPC server mode.
- No open ports/listeners.
- No browser-hosted execution surface.
- No Node host/gateway process.

## Filesystem and state controls

- State defaults under `~/.localclaw`.
- Session store files are written with hardened permissions where supported (`0600` files, `0700` session dirs).
- Session writes use lock files plus atomic replace behavior.
- Memory file reads through `memory_get` are restricted to allowed markdown sources.

## LLM execution boundary

- Claude Code integration is local subprocess only (`exec.CommandContext`).
- No direct model HTTP client is implemented in `localclaw`.
- AWS/GovCloud values are passed as environment variables to the subprocess.

## Explicitly out of scope

- Hosted gateway/server runtime.
- Remote tool bridges requiring inbound listeners.
- Embedded browser automation surfaces.

## Security controls roadmap

- Signed config and immutable policy profiles.
- Least-privilege subprocess execution profile for Claude Code CLI.
- Filesystem ACL hardening and audit trails.
