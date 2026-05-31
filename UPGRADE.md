# Upgrade Guide: MCP-Only LocalClaw

This guide migrates an older LocalClaw install to the MCP-only workspace and memory server.

The new runtime is intentionally smaller:

- Keeps: workspace bootstrap, markdown memory, `memory` CLI, `doctor`, `mcp serve`, and harness-driven `setup`.
- Removes: TUI, local prompt execution, LLM/provider adapters, sessions, skills, cron, heartbeat scheduling, channels, and backup loops.
- Config is strict. Old sections such as `llm`, `security`, `channels`, `session`, `backup`, `cron`, `heartbeat`, `skills`, and old `compaction` settings must be removed.
- Vector memory is optional and local. It requires explicit model install and does not download during normal startup/search.

## 1. Build And Install

From the repository checkout:

```bash
go test ./...
mkdir -p "$HOME/.local/bin"
go build -o "$HOME/.local/bin/localclaw" ./cmd/localclaw
```

Make sure the installed binary is the one your shell finds:

```bash
command -v localclaw
localclaw --help
```

If an older binary earlier on `PATH` shadows `~/.local/bin/localclaw`, replace that binary or move `~/.local/bin` earlier in `PATH`.

## 2. Preserve Legacy State

Before deleting or rewriting old runtime state, move stale directories into a dated backup folder.

```bash
stamp="$(date -u +%Y%m%d-%H%M%SZ)"
legacy="$HOME/.localclaw/legacy-mcp-only-$stamp"
mkdir -p "$legacy"

for item in localclaw.json backups cron logs runtime agents .localclaw; do
  if [ -e "$HOME/.localclaw/$item" ]; then
    mv "$HOME/.localclaw/$item" "$legacy/$item"
  fi
done

echo "$legacy"
```

Keep these in place unless you are certain there is no data you need:

- `$HOME/.localclaw/workspace`
- `$HOME/.localclaw/memory`

## 3. Bootstrap Current Config

Run doctor. If `~/.localclaw/localclaw.json` is missing, LocalClaw creates the new MCP-only config.

```bash
localclaw doctor
```

The generated config should contain only:

- `app`
- `agents`

Check memory state:

```bash
localclaw memory status
```

Optional vector memory setup:

```bash
localclaw memory model status
localclaw memory model install
localclaw memory index --force
```

If Hugging Face is unavailable:

```bash
localclaw memory model install --source mirror
```

The mirror is `https://github.com/dgriffin831/embeddinggemma-gguf-mirror`; verify the model SHA-256 is `b5ce9d77a3fc4b3b39ccb5643c36777911cc4eb46a66962eadfa3f5f60490d63`.

## 4. Clean Workspace Guidance

Existing workspace markdown is not overwritten. Review these files for stale references to removed LocalClaw features:

```bash
rg -n "cron|backup|channels|TUI|terminal UI|mcp__localclaw|local prompt|provider|session|skills" "$HOME/.localclaw/workspace" -S
```

Update wording so Claude, Codex, and opencode own chat execution, sessions, scheduling, and skills. LocalClaw should be described only as the stdio MCP workspace and memory server.

If `$HOME/.localclaw/workspace/skills` is empty and only existed for old LocalClaw skills, remove it:

```bash
rmdir "$HOME/.localclaw/workspace/skills" 2>/dev/null || true
```

## 5. Configure Coding Agents

Setup is harness-driven. LocalClaw asks the real harness to configure MCP instead of editing harness config files directly.

Preview prompts:

```bash
localclaw setup claude --dry-run
localclaw setup codex --dry-run
localclaw setup opencode --dry-run
```

Run setup for installed harnesses:

```bash
localclaw setup claude
localclaw setup codex
localclaw setup opencode
```

Each setup prompt instructs the harness to configure a stdio MCP server named `localclaw`:

```json
{
  "command": "localclaw",
  "args": ["mcp", "serve"]
}
```

## 6. Verify MCP Server Shape

The MCP server should expose only these tools:

- `localclaw_workspace_status`
- `localclaw_workspace_list`
- `localclaw_workspace_read`
- `localclaw_memory_create`
- `localclaw_memory_search`
- `localclaw_memory_get`
- `localclaw_memory_grep`

Removed tools for cron, sessions, Slack, Signal, backup, and LocalClaw-owned agent execution should be absent.

For Codex, first verify the configured server:

```bash
codex mcp list
codex mcp get localclaw
```

Then verify that a fresh Codex process can see the tool surface:

```bash
codex exec --skip-git-repo-check - <<'EOF'
Report whether a localclaw MCP server is available.
List the localclaw MCP tool names you can see.
Do not run shell commands.
EOF
```

Bare `codex` startup does not necessarily print every MCP tool. Existing Codex sessions can also keep old MCP tool metadata until the session is restarted.

## Troubleshooting

If `localclaw doctor` fails with `json: unknown field`, an old config is still being loaded. Move `~/.localclaw/localclaw.json` aside and run `localclaw doctor` again.

If Codex still shows removed tools such as cron or sessions, close that Codex session and start a new one. MCP tool schemas are loaded at session startup.

If a coding agent cannot find `localclaw`, restart that agent or terminal after fixing `PATH`, then re-run:

```bash
command -v localclaw
localclaw doctor
```

If memory search returns nothing after migration, rebuild the index:

```bash
localclaw memory index --force
localclaw memory search "known phrase"
```
