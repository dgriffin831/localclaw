package memory

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadSessionTranscriptNormalizedParsesJSONLMessages(t *testing.T) {
	sessionsRoot := t.TempDir()
	transcript := filepath.Join(sessionsRoot, "main.jsonl")

	mustWriteMemoryFile(t, transcript, strings.Join([]string{
		`{"role":"user","content":"hello world"}`,
		`{"message":{"role":"assistant","content":[{"text":"hi there"}]}}`,
		`{"type":"event","text":"ignored event text"}`,
		`not json`,
	}, "\n"))

	normalized, err := ReadSessionTranscriptNormalized(transcript)
	if err != nil {
		t.Fatalf("read normalized transcript: %v", err)
	}

	if !strings.Contains(normalized, "user: hello world") {
		t.Fatalf("expected normalized user content, got %q", normalized)
	}
	if !strings.Contains(normalized, "assistant: hi there") {
		t.Fatalf("expected normalized assistant content, got %q", normalized)
	}
	if strings.Contains(normalized, "not json") {
		t.Fatalf("expected invalid json line to be skipped, got %q", normalized)
	}
}

func TestSQLiteIndexManagerSyncIndexesSessionSource(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	sessionsRoot := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")
	transcript := filepath.Join(sessionsRoot, "main.jsonl")

	mustWriteMemoryFile(t, transcript, strings.Join([]string{
		`{"role":"user","content":"session needle"}`,
		`{"role":"assistant","content":"session haystack"}`,
	}, "\n"))

	m := NewSQLiteIndexManager(IndexManagerConfig{
		DBPath:        dbPath,
		WorkspaceRoot: workspace,
		Sources:       []string{"sessions"},
		SessionsRoot:  sessionsRoot,
		ChunkTokens:   32,
		ChunkOverlap:  0,
		Provider:      EmbeddingProviderNone,
		EnableFTS:     true,
	})
	if err := m.Open(ctx); err != nil {
		t.Fatalf("open manager: %v", err)
	}
	defer m.Close()

	if _, err := m.Sync(ctx, true); err != nil {
		t.Fatalf("sync: %v", err)
	}

	results, err := m.Search(ctx, "needle", SearchOptions{MaxResults: 5})
	if err != nil {
		t.Fatalf("search session text: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected session transcript to be indexed")
	}
	if results[0].Source != "sessions" {
		t.Fatalf("expected sessions source, got %q", results[0].Source)
	}
	if !strings.HasSuffix(results[0].Path, "main.jsonl") {
		t.Fatalf("expected transcript path in result, got %q", results[0].Path)
	}
}
