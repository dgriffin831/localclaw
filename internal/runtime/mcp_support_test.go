package runtime

import (
	"context"
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
