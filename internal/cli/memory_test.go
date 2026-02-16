package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"

	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/runtime"
)

func TestRunMemoryStatusJSONDeepIndexIncludesDiagnostics(t *testing.T) {
	ctx := context.Background()
	cfg, app, workspace := newTestApp(t, []string{"memory", "sessions"})

	if err := os.MkdirAll(filepath.Join(workspace, "memory"), 0o700); err != nil {
		t.Fatalf("mkdir memory dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "memory", "notes.md"), []byte("alpha\nbeta\ngamma"), 0o600); err != nil {
		t.Fatalf("write memory file: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := RunMemoryCommand(ctx, cfg, app, []string{"status", "--deep", "--index", "--json"}, &stdout, &stderr); err != nil {
		t.Fatalf("run memory status: %v (stderr=%q)", err, stderr.String())
	}

	var payload statusOutput
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode status json: %v\noutput=%s", err, stdout.String())
	}

	if payload.AgentID != "default" {
		t.Fatalf("unexpected agent id %q", payload.AgentID)
	}
	if payload.Index.FileCount == 0 {
		t.Fatalf("expected indexed files > 0")
	}
	if !payload.Scan.Deep {
		t.Fatalf("expected deep scan true")
	}
	if len(payload.Scan.Issues) == 0 {
		t.Fatalf("expected source scan issues for sessions source")
	}
}

func TestRunMemoryIndexForceReindexesUnchangedFiles(t *testing.T) {
	ctx := context.Background()
	cfg, app, workspace := newTestApp(t, []string{"memory"})

	if err := os.MkdirAll(filepath.Join(workspace, "memory"), 0o700); err != nil {
		t.Fatalf("mkdir memory dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "memory", "notes.md"), []byte("index me"), 0o600); err != nil {
		t.Fatalf("write memory file: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := RunMemoryCommand(ctx, cfg, app, []string{"index", "--json"}, &stdout, &stderr); err != nil {
		t.Fatalf("first memory index: %v (stderr=%q)", err, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if err := RunMemoryCommand(ctx, cfg, app, []string{"index", "--json"}, &stdout, &stderr); err != nil {
		t.Fatalf("second memory index: %v (stderr=%q)", err, stderr.String())
	}
	var second indexOutput
	if err := json.Unmarshal(stdout.Bytes(), &second); err != nil {
		t.Fatalf("decode second index json: %v", err)
	}
	if second.Sync.IndexedFiles != 0 {
		t.Fatalf("expected second index to skip unchanged files, got indexed=%d", second.Sync.IndexedFiles)
	}

	stdout.Reset()
	stderr.Reset()
	if err := RunMemoryCommand(ctx, cfg, app, []string{"index", "--force", "--json"}, &stdout, &stderr); err != nil {
		t.Fatalf("forced memory index: %v (stderr=%q)", err, stderr.String())
	}
	var forced indexOutput
	if err := json.Unmarshal(stdout.Bytes(), &forced); err != nil {
		t.Fatalf("decode forced index json: %v", err)
	}
	if forced.Sync.IndexedFiles == 0 {
		t.Fatalf("expected forced index to reindex files")
	}
}

func TestRunMemorySearchJSONReturnsResults(t *testing.T) {
	ctx := context.Background()
	cfg, app, workspace := newTestApp(t, []string{"memory"})

	if err := os.WriteFile(filepath.Join(workspace, "MEMORY.md"), []byte("match term on line one\nline two"), 0o600); err != nil {
		t.Fatalf("write MEMORY.md: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := RunMemoryCommand(ctx, cfg, app, []string{"index"}, &stdout, &stderr); err != nil {
		t.Fatalf("index before search: %v (stderr=%q)", err, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if err := RunMemoryCommand(ctx, cfg, app, []string{"search", "match term", "--json"}, &stdout, &stderr); err != nil {
		t.Fatalf("memory search: %v (stderr=%q)", err, stderr.String())
	}

	var payload searchOutput
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode search json: %v\noutput=%s", err, stdout.String())
	}
	if payload.ResultCount == 0 {
		t.Fatalf("expected search results")
	}
	if payload.Results[0].Path == "" {
		t.Fatalf("expected result path")
	}
}

func TestRunMemorySearchRequiresQuery(t *testing.T) {
	ctx := context.Background()
	cfg, app, _ := newTestApp(t, []string{"memory"})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := RunMemoryCommand(ctx, cfg, app, []string{"search"}, &stdout, &stderr)
	if err == nil {
		t.Fatalf("expected search command to fail without query")
	}
}

func TestRunMemorySearchUsesConfiguredLocalEmbeddingRuntime(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("shell runtime fixture is not supported on windows")
	}

	ctx := context.Background()
	cfg := config.Default()
	cfg.State.Root = t.TempDir()
	cfg.Agents.Defaults.Workspace = "."
	cfg.Agents.Defaults.MemorySearch.Enabled = true
	cfg.Agents.Defaults.MemorySearch.Sources = []string{"memory"}
	cfg.Agents.Defaults.MemorySearch.Provider = "local"
	cfg.Agents.Defaults.MemorySearch.Fallback = "local"
	cfg.Agents.Defaults.MemorySearch.Store.Path = filepath.Join("memory", "{agentId}.sqlite")
	cfg.Agents.Defaults.MemorySearch.Store.Vector.Enabled = true
	cfg.Agents.Defaults.MemorySearch.Cache.Enabled = false
	cfg.Agents.Defaults.MemorySearch.Query.Hybrid.Enabled = true
	cfg.Agents.Defaults.MemorySearch.Local.RuntimePath = writeEmbeddingRuntimeFixture(t)
	cfg.Heartbeat.Enabled = false
	cfg.Cron.Enabled = false

	app, err := runtime.New(cfg)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	if err := app.Run(ctx); err != nil {
		t.Fatalf("run app: %v", err)
	}
	workspace, err := app.ResolveWorkspacePath("")
	if err != nil {
		t.Fatalf("resolve workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "MEMORY.md"), []byte("local embedding fixture"), 0o600); err != nil {
		t.Fatalf("write MEMORY.md: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := RunMemoryCommand(ctx, cfg, app, []string{"index"}, &stdout, &stderr); err != nil {
		t.Fatalf("memory index: %v (stderr=%q)", err, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if err := RunMemoryCommand(ctx, cfg, app, []string{"search", "no-keyword-hit", "--json"}, &stdout, &stderr); err != nil {
		t.Fatalf("memory search: %v (stderr=%q)", err, stderr.String())
	}

	var payload searchOutput
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode search json: %v\noutput=%s", err, stdout.String())
	}
	if payload.ResultCount == 0 {
		t.Fatalf("expected vector-only result using configured local runtime")
	}
}

func newTestApp(t *testing.T, sources []string) (config.Config, *runtime.App, string) {
	t.Helper()

	cfg := config.Default()
	cfg.State.Root = t.TempDir()
	cfg.Agents.Defaults.Workspace = "."
	cfg.Agents.Defaults.MemorySearch.Sources = sources
	cfg.Agents.Defaults.MemorySearch.Provider = "none"
	cfg.Agents.Defaults.MemorySearch.Fallback = "none"
	cfg.Agents.Defaults.MemorySearch.Store.Path = filepath.Join("memory", "{agentId}.sqlite")
	cfg.Agents.Defaults.MemorySearch.Store.Vector.Enabled = false
	cfg.Agents.Defaults.MemorySearch.Cache.Enabled = false
	cfg.Agents.Defaults.MemorySearch.Query.Hybrid.Enabled = false
	cfg.Heartbeat.Enabled = false
	cfg.Cron.Enabled = false

	app, err := runtime.New(cfg)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("run app: %v", err)
	}
	workspace, err := app.ResolveWorkspacePath("")
	if err != nil {
		t.Fatalf("resolve workspace: %v", err)
	}
	return cfg, app, workspace
}

func writeEmbeddingRuntimeFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "local-embed-runtime.sh")
	body := `#!/usr/bin/env bash
set -euo pipefail
if [ "${1:-}" != "embed" ] || [ "${2:-}" != "--format" ] || [ "${3:-}" != "json" ]; then
  echo "unsupported embedding runtime args" >&2
  exit 2
fi
cat >/dev/null
printf '{"embeddings":[[1.0,0.0,0.0]]}\n'
`
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatalf("write embedding runtime fixture: %v", err)
	}
	return path
}
