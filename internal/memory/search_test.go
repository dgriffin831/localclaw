package memory

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchKeywordOnlyReturnsSnippetsAndScores(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")

	mustWriteMemoryFile(t, filepath.Join(workspace, "MEMORY.md"), "alpha marker\nalpha marker\nalpha marker")
	mustWriteMemoryFile(t, filepath.Join(workspace, "memory", "notes.md"), "alpha marker")

	m := NewSQLiteIndexManager(IndexManagerConfig{
		DBPath:        dbPath,
		WorkspaceRoot: workspace,
		ChunkTokens:   64,
		ChunkOverlap:  0,
		EnableFTS:     true,
	})
	if err := m.Open(ctx); err != nil {
		t.Fatalf("open manager: %v", err)
	}
	defer m.Close()

	if _, err := m.Sync(ctx, true); err != nil {
		t.Fatalf("sync: %v", err)
	}

	results, err := m.Search(ctx, "marker", SearchOptions{MaxResults: 5, MinScore: 0})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}

	first := results[0]
	if first.Path != "MEMORY.md" {
		t.Fatalf("unexpected top result path: %q", first.Path)
	}
	if first.StartLine <= 0 || first.EndLine < first.StartLine {
		t.Fatalf("unexpected line range: %d-%d", first.StartLine, first.EndLine)
	}
	if first.Score <= 0 {
		t.Fatalf("expected positive score, got %f", first.Score)
	}
	if strings.TrimSpace(first.Snippet) == "" {
		t.Fatalf("expected non-empty snippet")
	}
	if first.Source != "memory" {
		t.Fatalf("unexpected source: %q", first.Source)
	}
}

func TestSearchFallsBackToLikeWhenFTSReturnsNoRows(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")

	mustWriteMemoryFile(t, filepath.Join(workspace, "MEMORY.md"), "marker token")

	m := NewSQLiteIndexManager(IndexManagerConfig{
		DBPath:        dbPath,
		WorkspaceRoot: workspace,
		ChunkTokens:   64,
		ChunkOverlap:  0,
		EnableFTS:     true,
	})
	if err := m.Open(ctx); err != nil {
		t.Fatalf("open manager: %v", err)
	}
	defer m.Close()

	if _, err := m.Sync(ctx, true); err != nil {
		t.Fatalf("sync: %v", err)
	}

	results, err := m.Search(ctx, "mark", SearchOptions{MaxResults: 5, MinScore: 0})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected LIKE fallback to return 1 result, got %d", len(results))
	}
	if results[0].Path != "MEMORY.md" {
		t.Fatalf("unexpected path from LIKE fallback: %q", results[0].Path)
	}
}

func TestGetRestrictsPathScopeAndSupportsLineSlice(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")

	mustWriteMemoryFile(t, filepath.Join(workspace, "memory", "notes.md"), "one\ntwo\nthree\nfour")
	outside := filepath.Join(t.TempDir(), "outside.md")
	mustWriteMemoryFile(t, outside, "outside")

	m := NewSQLiteIndexManager(IndexManagerConfig{
		DBPath:        dbPath,
		WorkspaceRoot: workspace,
		ChunkTokens:   64,
		ChunkOverlap:  0,
	})
	if err := m.Open(ctx); err != nil {
		t.Fatalf("open manager: %v", err)
	}
	defer m.Close()

	if _, err := m.Get(ctx, "../outside.md", GetOptions{}); !errors.Is(err, ErrMemoryPathOutOfScope) {
		t.Fatalf("expected ErrMemoryPathOutOfScope for traversal path, got %v", err)
	}
	if _, err := m.Get(ctx, outside, GetOptions{}); !errors.Is(err, ErrMemoryPathOutOfScope) {
		t.Fatalf("expected ErrMemoryPathOutOfScope for out-of-scope absolute path, got %v", err)
	}

	got, err := m.Get(ctx, "memory/notes.md", GetOptions{FromLine: 2, Lines: 2})
	if err != nil {
		t.Fatalf("get sliced: %v", err)
	}
	if got.Path != "memory/notes.md" {
		t.Fatalf("unexpected path: %q", got.Path)
	}
	if got.StartLine != 2 || got.EndLine != 3 {
		t.Fatalf("unexpected line range: %d-%d", got.StartLine, got.EndLine)
	}
	if got.Content != "two\nthree" {
		t.Fatalf("unexpected sliced content: %q", got.Content)
	}
}

func TestSearchEmptyQueryReturnsError(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")
	mustWriteMemoryFile(t, filepath.Join(workspace, "MEMORY.md"), "alpha")

	m := NewSQLiteIndexManager(IndexManagerConfig{
		DBPath:        dbPath,
		WorkspaceRoot: workspace,
		ChunkTokens:   64,
		ChunkOverlap:  0,
	})
	if err := m.Open(ctx); err != nil {
		t.Fatalf("open manager: %v", err)
	}
	defer m.Close()

	if _, err := m.Sync(ctx, true); err != nil {
		t.Fatalf("sync: %v", err)
	}

	if _, err := m.Search(ctx, "   ", SearchOptions{}); !errors.Is(err, ErrEmptySearchQuery) {
		t.Fatalf("expected ErrEmptySearchQuery, got %v", err)
	}
}

func TestGetRejectsNonMarkdownPath(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")
	mustWriteMemoryFile(t, filepath.Join(workspace, "memory", "notes.md"), "alpha")

	m := NewSQLiteIndexManager(IndexManagerConfig{
		DBPath:        dbPath,
		WorkspaceRoot: workspace,
		ChunkTokens:   64,
		ChunkOverlap:  0,
	})
	if err := m.Open(ctx); err != nil {
		t.Fatalf("open manager: %v", err)
	}
	defer m.Close()

	if _, err := m.Get(ctx, "memory/notes.txt", GetOptions{}); !errors.Is(err, ErrMemoryPathNotMarkdown) {
		t.Fatalf("expected ErrMemoryPathNotMarkdown, got %v", err)
	}
}
