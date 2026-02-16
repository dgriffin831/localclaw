package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
