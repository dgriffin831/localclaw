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

func TestGrepHonorsConfiguredSources(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	sessionsRoot := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")

	mustWriteMemoryFile(t, filepath.Join(workspace, "MEMORY.md"), "only-memory-token")
	mustWriteMemoryFile(t, filepath.Join(sessionsRoot, "session.jsonl"), `{"role":"user","content":"only-session-token"}`)

	memoryOnly := NewSQLiteIndexManager(IndexManagerConfig{
		DBPath:        dbPath,
		WorkspaceRoot: workspace,
		SessionsRoot:  sessionsRoot,
		Sources:       []string{"memory"},
		ChunkTokens:   64,
		ChunkOverlap:  0,
	})
	if err := memoryOnly.Open(ctx); err != nil {
		t.Fatalf("open manager: %v", err)
	}
	defer memoryOnly.Close()

	out, err := memoryOnly.Grep(ctx, "only-session-token", GrepOptions{Source: "all"})
	if err != nil {
		t.Fatalf("grep sessions from memory-only manager: %v", err)
	}
	if out.Count != 0 {
		t.Fatalf("expected sessions to be excluded when memory is the only configured source, got %d matches", out.Count)
	}

	sessionsOnly := NewSQLiteIndexManager(IndexManagerConfig{
		DBPath:        filepath.Join(t.TempDir(), "memory.sqlite"),
		WorkspaceRoot: workspace,
		SessionsRoot:  sessionsRoot,
		Sources:       []string{"sessions"},
		ChunkTokens:   64,
		ChunkOverlap:  0,
	})
	if err := sessionsOnly.Open(ctx); err != nil {
		t.Fatalf("open manager: %v", err)
	}
	defer sessionsOnly.Close()

	out, err = sessionsOnly.Grep(ctx, "only-memory-token", GrepOptions{Source: "all"})
	if err != nil {
		t.Fatalf("grep memory from sessions-only manager: %v", err)
	}
	if out.Count != 0 {
		t.Fatalf("expected memory files to be excluded when sessions is the only configured source, got %d matches", out.Count)
	}
}
