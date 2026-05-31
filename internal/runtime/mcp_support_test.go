package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/memory"
)

func TestRunBootstrapsWorkspaceControlFiles(t *testing.T) {
	app := newTestApp(t)
	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	workspacePath, err := app.ResolveWorkspacePath("")
	if err != nil {
		t.Fatalf("ResolveWorkspacePath: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspacePath, "SOUL.md")); err != nil {
		t.Fatalf("expected SOUL.md bootstrap: %v", err)
	}
}

func TestMCPMemoryCreateSearchGet(t *testing.T) {
	ctx := context.Background()
	app := newTestApp(t)
	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	created, err := app.MCPMemoryCreate(ctx, MCPMemoryCreateRequest{
		Title:   "Important Finding",
		Content: "persistent unique marker alpha beta",
		Tags:    []string{"Finding"},
	})
	if err != nil {
		t.Fatalf("MCPMemoryCreate: %v", err)
	}
	if !created.Indexed {
		t.Fatalf("expected created memory to be indexed: %+v", created)
	}
	results, err := app.MCPMemorySearch(ctx, "", "unique marker", memory.SearchOptions{MaxResults: 5})
	if err != nil {
		t.Fatalf("MCPMemorySearch: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected search results")
	}
	got, err := app.MCPMemoryGet(ctx, "", created.Path, memory.GetOptions{})
	if err != nil {
		t.Fatalf("MCPMemoryGet: %v", err)
	}
	if !strings.Contains(got.Content, "persistent unique marker") {
		t.Fatalf("expected created content, got %q", got.Content)
	}
}

func TestMCPWorkspaceReadRejectsTraversal(t *testing.T) {
	ctx := context.Background()
	app := newTestApp(t)
	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := app.MCPWorkspaceRead(ctx, "", "../outside.md", memory.GetOptions{}); err == nil {
		t.Fatalf("expected traversal read to fail")
	}
}

func TestMCPWorkspaceListIncludesControlFiles(t *testing.T) {
	ctx := context.Background()
	app := newTestApp(t)
	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	files, err := app.MCPWorkspaceList(ctx, "")
	if err != nil {
		t.Fatalf("MCPWorkspaceList: %v", err)
	}
	for _, file := range files {
		if file.Path == "SOUL.md" {
			return
		}
	}
	t.Fatalf("expected SOUL.md in workspace list: %+v", files)
}

func TestResolveStorePathResolvesRelativePathUnderStateRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	got, err := resolveStorePath(root, "memory/{agentId}.sqlite", "codex")
	if err != nil {
		t.Fatalf("resolveStorePath: %v", err)
	}
	want := filepath.Join(root, "memory", "codex.sqlite")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func newTestApp(t *testing.T) *App {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := config.Default()
	cfg.App.Root = filepath.Join(t.TempDir(), "state")
	app, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return app
}
