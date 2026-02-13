# localclaw

`localclaw` is a private, Go-only, single-process CLI agent runtime for secure enterprise environments.

## Design goals

- Local-first and local-only execution model.
- No network listeners, no gateway/server mode, and no browser/node-distributed runtime.
- Primary LLM integration via local Claude Code CLI.
- Support Claude Code CLI-compatible AWS GovCloud Bedrock auth/model flows.
- Preserve OpenClaw-style capabilities: memory, workspace, skills, cron, heartbeat.
- Channels limited to Slack and Signal.

## Current status

This repository is initialized as a greenfield MVP kickoff with:

- Foundational architecture and security docs.
- Minimal runnable CLI skeleton.
- Strict startup policy checks for local-only mode.
- Initial tests for config validation and local-only enforcement.

## Quick start

```bash
/usr/local/go/bin/go test ./...
/usr/local/go/bin/go run ./cmd/localclaw
```

Optional config file:

```bash
/usr/local/go/bin/go run ./cmd/localclaw -config ./localclaw.json
```

## Scope guardrails

- Go only.
- Monolithic single-process CLI only.
- Local-only tools: filesystem, local process execution, local scheduler.
- No remote tool bridges, no browser automation, no web server surfaces.
