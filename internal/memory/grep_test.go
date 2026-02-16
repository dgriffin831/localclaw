package memory

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestGrepReturnsDeterministicPathAndLineOrdering(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")

	mustWriteMemoryFile(t, filepath.Join(workspace, "MEMORY.md"), "token\nz")
	mustWriteMemoryFile(t, filepath.Join(workspace, "memory", "notes.md"), "a\ntoken")

	m := NewSQLiteIndexManager(IndexManagerConfig{
		DBPath:        dbPath,
		WorkspaceRoot: workspace,
		ChunkTokens:   64,
		ChunkOverlap:  0,
		Provider:      EmbeddingProviderNone,
	})
	if err := m.Open(ctx); err != nil {
		t.Fatalf("open manager: %v", err)
	}
	defer m.Close()

	out, err := m.Grep(ctx, "token", GrepOptions{Mode: "literal"})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if out.Count != 2 {
		t.Fatalf("expected 2 matches, got %d", out.Count)
	}
	if out.Matches[0].Path != "MEMORY.md" || out.Matches[0].Line != 1 {
		t.Fatalf("unexpected first match: %+v", out.Matches[0])
	}
	if out.Matches[1].Path != "memory/notes.md" || out.Matches[1].Line != 2 {
		t.Fatalf("unexpected second match: %+v", out.Matches[1])
	}
}

func TestGrepRejectsOutOfScopePathGlob(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")

	mustWriteMemoryFile(t, filepath.Join(workspace, "MEMORY.md"), "token")

	m := NewSQLiteIndexManager(IndexManagerConfig{
		DBPath:        dbPath,
		WorkspaceRoot: workspace,
		ChunkTokens:   64,
		ChunkOverlap:  0,
		Provider:      EmbeddingProviderNone,
	})
	if err := m.Open(ctx); err != nil {
		t.Fatalf("open manager: %v", err)
	}
	defer m.Close()

	_, err := m.Grep(ctx, "token", GrepOptions{PathGlob: []string{"../*.md"}})
	if !errors.Is(err, ErrMemoryPathGlobOutOfScope) {
		t.Fatalf("expected ErrMemoryPathGlobOutOfScope, got %v", err)
	}
}
