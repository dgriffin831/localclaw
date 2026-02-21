# Summary

I performed a focused bug-hunt across runtime/config/CLI/channels/MCP/memory/cron/session/TUI and kept only high-confidence defects with concrete proof. I found 3 real correctness/reliability bugs:

1. `memory` CLI ignores per-agent memory overrides and uses defaults.
2. Runtime merge logic cannot apply explicit `memory.sync.onSearch=false` agent overrides.
3. Per-agent `compaction.memoryFlush` overrides cannot disable inherited defaults (`enabled=false` and zero values are ignored).

All findings below include exact file/line references, impact, verification, and a concrete fix prompt.

## Bug 1: Memory CLI Ignores Per-Agent Memory Overrides

- Severity: High
- Confidence: High (reproduced)

### Affected code

- `internal/cli/memory.go:541`
- `internal/cli/memory.go:542`
- `internal/cli/memory.go:543`

```go
// TODO: Resolve per-agent memory config with the same merge logic used by runtime (including agent overrides) instead of always using defaults.
memoryCfg := cfg.Agents.Defaults.Memory
storePath, err := resolveStorePath(cfg.App.Root, memoryCfg.Store.Path, resolvedAgent)
```

### Why this is a bug

`localclaw memory ... --agent <id>` should honor that agent's resolved memory policy, but CLI hardcodes `agents.defaults.memory`. This causes wrong DB paths, wrong max results, and wrong source/sync behavior for non-default agents.

### Reproduction/verification steps

1. Run:

```bash
tmpdir=$(mktemp -d)
cat > "$tmpdir/config.json" <<'EOF'
{
  "app": {"name":"localclaw","root":"TMPROOT","default":{"verbose":false,"mouse":false,"tools":false}},
  "security": {"mode":"sandbox-write"},
  "llm": {
    "provider":"claudecode",
    "claude_code":{"binary_path":"claude","profile":"default","extra_args":[],"session_mode":"always","session_arg":"--session-id","resume_args":["--resume","{sessionId}"],"session_id_fields":["session_id"]},
    "codex":{"binary_path":"codex","profile":"default","model":"","reasoning_default":"medium","extra_args":[],"session_mode":"existing","resume_args":["resume","{sessionId}"],"session_id_fields":["thread_id"],"resume_output":"json","mcp":{"config_path":"","server_name":"localclaw"}}
  },
  "channels": {"enabled":[],"slack":{"bot_token_env":"SLACK_BOT_TOKEN","default_channel":"","api_base_url":"https://slack.com/api","timeout_seconds":10},"signal":{"cli_path":"signal-cli","account":"+10000000000","default_recipient":"","timeout_seconds":10,"inbound":{"enabled":false,"allow_from":[],"agent_by_sender":{},"default_agent":"default","send_typing":true,"typing_interval_seconds":5,"send_read_receipts":true,"poll_timeout_seconds":5,"max_messages_per_poll":10}}},
  "agents": {
    "defaults": {
      "workspace": ".",
      "memory": {"enabled": true,"tools": {"get":true,"search":true,"grep":true},"sources": ["memory"],"extraPaths": [],"store": {"path":"TMPROOT/memory/default.sqlite"},"chunking": {"tokens":400,"overlap":40},"query": {"maxResults":8},"sync": {"onSearch":false,"sessions":{"deltaBytes":32768,"deltaMessages":20}}},
      "compaction": {"memoryFlush":{"enabled":true,"thresholdTokens":28000,"triggerWindowTokens":4000,"prompt":"","timeoutSeconds":20}}
    },
    "list": [{"id":"agent-x","workspace":".","memory":{"store":{"path":"TMPROOT/memory/agent-x-override.sqlite"},"query":{"maxResults":3}}}]
  },
  "session": {"store":"TMPROOT/agents/{agentId}/sessions/sessions.json"},
  "backup": {"auto_save":false,"auto_clean":false,"interval":"1d","retain_count":1},
  "cron": {"enabled":false},
  "heartbeat": {"enabled":false,"interval_seconds":30}
}
EOF
sed -i "s|TMPROOT|$tmpdir/state|g" "$tmpdir/config.json"
mkdir -p "$tmpdir/state"
go run ./cmd/localclaw -config "$tmpdir/config.json" memory status --agent agent-x --json
```

2. Observe output includes:
   - `"agentId": "agent-x"`
   - `"storePath": ".../memory/default.sqlite"` (wrong; expected `agent-x-override.sqlite`)

### Proposed fix

- Refactor CLI memory context resolution to use the same merged per-agent memory config as runtime (`resolveMemoryConfig` semantics).
- Avoid duplicate merge logic divergence by extracting shared resolver into a reusable helper.
- Add tests ensuring CLI `memory status/index/search/grep` honor per-agent overrides (store path, query max, source list, sync flags).

### Ready-to-run Codex fix prompt

```text
Implement a fix for localclaw bug: memory CLI ignores per-agent memory overrides.

Scope:
1) Update internal/cli/memory.go so newMemoryCommandContext resolves memory config using the same per-agent merge behavior as runtime (currently in internal/runtime/tools.go resolveMemoryConfig/mergeMemoryConfig).
2) Prefer extracting shared merge/resolve logic into a reusable package-level helper to avoid future drift.
3) Ensure CLI uses resolved config for store path, sources, extraPaths, chunking, query.maxResults, and sync settings.
4) Add/adjust tests in internal/cli/memory_test.go that fail before fix and pass after fix:
   - per-agent store.path override is used by `memory status --agent ... --json`
   - per-agent query.maxResults override affects CLI search default
5) Run go test ./internal/cli and go test ./...

Do not change behavior outside this bug fix.
```

## Bug 2: `memory.sync.onSearch=false` Override Cannot Be Applied Per Agent

- Severity: High
- Confidence: High (reproduced + code-path proof)

### Affected code

- `internal/config/config.go:227`
- `internal/runtime/tools.go:329`
- `internal/runtime/tools.go:366`
- `internal/runtime/tools.go:367`
- `internal/runtime/mcp_support.go:85`

```go
type SyncConfig struct {
    OnSearch bool `json:"onSearch"`
    ...
}

func hasMemoryOverride(cfg config.MemoryOverrideConfig) bool {
    ...
    cfg.Sync.OnSearch ||
    ...
}

if override.Sync.OnSearch {
    merged.Sync.OnSearch = true
}

if memoryCfg.Sync.OnSearch {
    _, err := manager.Sync(ctx, false)
}
```

### Why this is a bug

Because `OnSearch` is a non-pointer bool in override config and merge only applies `true`, an agent cannot explicitly override inherited `true` to `false`. Result: unintended auto-indexing on search continues for that agent.

### Reproduction/verification steps

Run:

```bash
cat > ./tmp_repro_onsearch.go <<'EOF'
package main
import (
  "context"
  "fmt"
  "os"
  "path/filepath"
  "github.com/dgriffin831/localclaw/internal/config"
  "github.com/dgriffin831/localclaw/internal/memory"
  "github.com/dgriffin831/localclaw/internal/runtime"
)
func main() {
  ctx := context.Background()
  cfg := config.Default()
  root, _ := os.MkdirTemp("", "localclaw-repro-")
  cfg.App.Root = root
  cfg.Channels.Enabled = []string{}
  cfg.Cron.Enabled = false
  cfg.Heartbeat.Enabled = false
  cfg.Agents.Defaults.Memory.Sync.OnSearch = true
  cfg.Agents.List = []config.AgentConfig{{
    ID: "agent-y",
    Workspace: ".",
    Memory: config.MemoryOverrideConfig{
      Query: config.QueryConfig{MaxResults: 1},
      Sync:  config.SyncConfig{OnSearch: false},
    },
  }}
  app, _ := runtime.New(cfg)
  _ = app.Run(ctx)
  ws, _ := app.ResolveWorkspacePath("agent-y")
  _ = os.WriteFile(filepath.Join(ws, "MEMORY.md"), []byte("override-check-token\n"), 0600)
  results, _ := app.MCPMemorySearch(ctx, "agent-y", "main", "override-check-token", memory.SearchOptions{})
  fmt.Printf("results=%d\n", len(results))
}
EOF
go run ./tmp_repro_onsearch.go
rm -f ./tmp_repro_onsearch.go
```

Observed output: `results=1` (auto-sync still ran), proving `OnSearch=false` override did not take effect.

### Proposed fix

- Introduce explicit override semantics for `memory.sync.onSearch` (pointer bool in override shape).
- Update merge logic to apply explicit false as well as true.
- Update override detection logic accordingly.
- Add regression tests for:
  - default true + agent false => resolved false
  - default false + agent true => resolved true

### Ready-to-run Codex fix prompt

```text
Fix localclaw bug: per-agent memory.sync.onSearch=false cannot override inherited true.

Tasks:
1) Change override modeling so agents.list[].memory.sync.onSearch can be tri-state (unset/true/false), e.g. pointer bool in override config.
2) Update runtime merge logic in internal/runtime/tools.go:
   - hasMemoryOverride should detect explicit onSearch override presence.
   - mergeMemoryConfig should apply explicit false and true values.
3) Update config structs/validation tests as needed in internal/config.
4) Add runtime tests proving:
   - defaults.onSearch=true + agent override false resolves to false
   - defaults.onSearch=false + agent override true resolves to true
5) Run go test ./internal/runtime ./internal/config and go test ./...

Keep behavior unchanged outside this override bug.
```

## Bug 3: Per-Agent `memoryFlush` Overrides Cannot Disable Inherited Defaults

- Severity: High
- Confidence: High (reproduced + code-path proof)

### Affected code

- `internal/config/config.go:167`
- `internal/config/config.go:168`
- `internal/runtime/app.go:578`
- `internal/runtime/app.go:587`
- `internal/runtime/app.go:589`
- `internal/runtime/app.go:598`
- `internal/runtime/app.go:599`
- `internal/runtime/app.go:600`
- `internal/runtime/app.go:602`
- `internal/runtime/app.go:611`
- `internal/runtime/app.go:467`

```go
type MemoryFlushConfig struct {
    Enabled bool `json:"enabled"`
    ThresholdTokens int `json:"thresholdTokens"`
    ...
}

func hasMemoryFlushOverride(cfg config.MemoryFlushConfig) bool {
    return cfg.Enabled || cfg.ThresholdTokens > 0 || ...
}

func mergeMemoryFlushConfig(base, override config.MemoryFlushConfig) config.MemoryFlushConfig {
    merged := base
    if override.Enabled { merged.Enabled = true }
    if override.ThresholdTokens > 0 { merged.ThresholdTokens = override.ThresholdTokens }
    ...
}
```

### Why this is a bug

Agent-level compaction settings cannot explicitly disable inherited flush behavior:

- `enabled=false` is treated as “no override”.
- zero-valued thresholds/timeouts cannot override inherited non-zero defaults.

So an operator cannot turn memory flush off for one agent even when config appears to do so.

### Reproduction/verification steps

Run:

```bash
cat > ./tmp_repro_flush.go <<'EOF'
package main
import (
  "context"
  "fmt"
  "os"
  "github.com/dgriffin831/localclaw/internal/config"
  "github.com/dgriffin831/localclaw/internal/runtime"
)
func main() {
  ctx := context.Background()
  cfg := config.Default()
  root, _ := os.MkdirTemp("", "localclaw-repro-flush-")
  cfg.App.Root = root
  cfg.Channels.Enabled = []string{}
  cfg.Cron.Enabled = false
  cfg.Heartbeat.Enabled = false
  cfg.Agents.Defaults.Compaction.MemoryFlush.Enabled = true
  cfg.Agents.Defaults.Compaction.MemoryFlush.ThresholdTokens = 1
  cfg.Agents.Defaults.Compaction.MemoryFlush.TriggerWindowTokens = 0
  cfg.Agents.Defaults.Compaction.MemoryFlush.TimeoutSeconds = 1
  cfg.Agents.List = []config.AgentConfig{{
    ID: "writer",
    Workspace: ".",
    Compaction: config.CompactionConfig{MemoryFlush: config.MemoryFlushConfig{Enabled: false}},
  }}
  app, _ := runtime.New(cfg)
  _ = app.Run(ctx)
  _ = app.AddSessionTokens(ctx, "writer", "s1", 5)
  err := app.RunMemoryFlushIfNeeded(ctx, "writer", "s1")
  if err == nil { fmt.Println("flush_result=nil"); return }
  fmt.Printf("flush_error=%v\n", err)
}
EOF
go run ./tmp_repro_flush.go
rm -f ./tmp_repro_flush.go
```

Observed output: non-nil `flush_error=...`, which proves flush still attempted despite per-agent `enabled:false`.

### Proposed fix

- Introduce explicit override schema for `agents.list[].compaction.memoryFlush` using pointer fields (or dedicated `MemoryFlushOverrideConfig`).
- Update resolver/merge logic to support explicit false/zero overrides.
- Add tests covering:
  - defaults enabled true + agent enabled false => resolved disabled
  - defaults threshold non-zero + agent threshold zero override => resolved zero

### Ready-to-run Codex fix prompt

```text
Fix localclaw bug: per-agent compaction.memoryFlush overrides cannot disable inherited defaults.

Required changes:
1) Introduce explicit override representation for agents.list[].compaction.memoryFlush (tri-state fields; pointer-based recommended).
2) Update runtime resolve/merge in internal/runtime/app.go:
   - remove truthy-only override detection
   - apply explicit false/zero/empty overrides when provided
3) Keep default behavior for agents.defaults.compaction.memoryFlush unchanged.
4) Add regression tests in internal/runtime (and internal/config if needed):
   - defaults enabled=true, agent enabled=false => RunMemoryFlushIfNeeded short-circuits
   - defaults threshold/timeouts non-zero, agent explicitly sets zero => resolved values are zero
5) Run go test ./internal/runtime ./internal/config and go test ./...

Do not modify unrelated runtime behavior.
```
