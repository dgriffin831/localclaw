package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dgriffin831/localclaw/internal/memory"
)

func TestReadTranscriptHistoryAcceptsLargeLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	large := strings.Repeat("a", 70*1024)
	line := "{\"role\":\"assistant\",\"content\":\"" + large + "\"}\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	items, err := readTranscriptHistory(path)
	if err != nil {
		t.Fatalf("readTranscriptHistory error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if len(items[0].Content) != transcriptItemMaxChars {
		t.Fatalf("expected truncated content length %d, got %d", transcriptItemMaxChars, len(items[0].Content))
	}
}

func TestReadTranscriptHistorySkipsInvalidRowsAndExtractsText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	lines := strings.Join([]string{
		"not-json",
		"{\"role\":\"assistant\",\"content\":[{\"text\":\"hello\"},{\"text\":\"world\"}]}",
		"{\"role\":\"assistant\",\"content\":\"\"}",
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	items, err := readTranscriptHistory(path)
	if err != nil {
		t.Fatalf("readTranscriptHistory error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 valid item, got %d", len(items))
	}
	if items[0].Content != "hello world" {
		t.Fatalf("unexpected content: %q", items[0].Content)
	}
}

func TestMCPMemorySearchRespectsMemoryEnabledFlag(t *testing.T) {
	ctx := context.Background()
	_, app, workspace := newToolTestApp(t, true)
	app.cfg.Agents.Defaults.Memory.Enabled = false

	if err := osWriteFile(filepath.Join(workspace, "MEMORY.md"), "incident summary"); err != nil {
		t.Fatalf("write memory file: %v", err)
	}

	_, err := app.MCPMemorySearch(ctx, "", "", "incident", memory.SearchOptions{})
	if err == nil {
		t.Fatalf("expected MCP memory_search to fail when agents.defaults.memory.enabled=false")
	}
	if !strings.Contains(err.Error(), "memory tools are disabled") {
		t.Fatalf("expected memory disabled error, got %v", err)
	}
}

func TestMCPMemoryGrepRespectsPerToolFlag(t *testing.T) {
	ctx := context.Background()
	_, app, workspace := newToolTestApp(t, true)
	app.cfg.Agents.Defaults.Memory.Tools.Grep = false

	if err := osWriteFile(filepath.Join(workspace, "MEMORY.md"), "token-123"); err != nil {
		t.Fatalf("write memory file: %v", err)
	}

	_, err := app.MCPMemoryGrep(ctx, "", "", "token-123", memory.GrepOptions{})
	if err == nil {
		t.Fatalf("expected MCP memory_grep to fail when agents.defaults.memory.tools.grep=false")
	}
	if !strings.Contains(err.Error(), "memory_grep is disabled") {
		t.Fatalf("expected disabled-tool error, got %v", err)
	}
}

func TestMCPSessionDeleteRemovesMetadataAndTranscript(t *testing.T) {
	ctx := context.Background()
	_, app, _ := newToolTestApp(t, true)

	if err := app.AddSessionTokens(ctx, "default", "archive-1", 42); err != nil {
		t.Fatalf("seed session metadata: %v", err)
	}
	if err := app.AppendSessionTranscriptMessage(ctx, "default", "archive-1", "user", "hello"); err != nil {
		t.Fatalf("seed transcript: %v", err)
	}

	transcriptPath, err := app.ResolveTranscriptPath("default", "archive-1")
	if err != nil {
		t.Fatalf("resolve transcript path: %v", err)
	}
	if _, statErr := os.Stat(transcriptPath); statErr != nil {
		t.Fatalf("expected seeded transcript to exist: %v", statErr)
	}

	removed, err := app.MCPSessionDelete(ctx, "default", "archive-1")
	if err != nil {
		t.Fatalf("delete session: %v", err)
	}
	if !removed {
		t.Fatalf("expected removed=true")
	}

	if _, err := app.MCPSessionStatus(ctx, "default", "archive-1"); !errors.Is(err, ErrMCPNotFound) {
		t.Fatalf("expected session status to return not found after delete, got %v", err)
	}
	if _, statErr := os.Stat(transcriptPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected transcript file to be removed, got %v", statErr)
	}
}

func TestMCPSessionDeleteRemovesOrphanTranscript(t *testing.T) {
	ctx := context.Background()
	_, app, _ := newToolTestApp(t, true)

	transcriptPath, err := app.ResolveTranscriptPath("default", "orphan")
	if err != nil {
		t.Fatalf("resolve transcript path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(transcriptPath), 0o700); err != nil {
		t.Fatalf("mkdir transcript dir: %v", err)
	}
	if err := os.WriteFile(transcriptPath, []byte("{\"role\":\"user\",\"content\":\"orphan\"}\n"), 0o600); err != nil {
		t.Fatalf("write orphan transcript: %v", err)
	}

	removed, err := app.MCPSessionDelete(ctx, "default", "orphan")
	if err != nil {
		t.Fatalf("delete orphan session: %v", err)
	}
	if !removed {
		t.Fatalf("expected removed=true for orphan transcript cleanup")
	}
	if _, statErr := os.Stat(transcriptPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected orphan transcript removal, got %v", statErr)
	}
}

func TestMCPSessionDeleteReturnsFalseWhenSessionIsMissing(t *testing.T) {
	ctx := context.Background()
	_, app, _ := newToolTestApp(t, true)

	removed, err := app.MCPSessionDelete(ctx, "default", "missing")
	if err != nil {
		t.Fatalf("delete missing session: %v", err)
	}
	if removed {
		t.Fatalf("expected removed=false for missing session")
	}
}
