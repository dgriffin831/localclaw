package memory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAppendsAndSyncsForSearch(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")

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

	saved, err := m.Write(ctx, "release note alpha", WriteOptions{})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !saved.Appended {
		t.Fatalf("expected appended=true by default")
	}
	if saved.Path == "" {
		t.Fatalf("expected returned path")
	}
	if saved.BytesWritten <= 0 {
		t.Fatalf("expected positive bytes written, got %d", saved.BytesWritten)
	}
	if !saved.Indexed {
		t.Fatalf("expected indexed=true")
	}

	absSavedPath := filepath.Join(workspace, filepath.FromSlash(saved.Path))
	body, err := os.ReadFile(absSavedPath)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if !strings.Contains(string(body), "release note alpha") {
		t.Fatalf("expected saved content in file %q", absSavedPath)
	}

	results, err := m.Search(ctx, "release note alpha", SearchOptions{MaxResults: 4})
	if err != nil {
		t.Fatalf("search after save: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected saved content to be searchable")
	}
	if results[0].Path != saved.Path {
		t.Fatalf("expected top result path %q, got %q", saved.Path, results[0].Path)
	}
}

func TestWriteRejectsOutOfScopeAndNonMarkdownPaths(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")

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

	if _, err := m.Write(ctx, "alpha", WriteOptions{Path: "../escape.md"}); !errors.Is(err, ErrMemoryPathOutOfScope) {
		t.Fatalf("expected ErrMemoryPathOutOfScope, got %v", err)
	}
	if _, err := m.Write(ctx, "alpha", WriteOptions{Path: "memory/notes.txt"}); !errors.Is(err, ErrMemoryPathNotMarkdown) {
		t.Fatalf("expected ErrMemoryPathNotMarkdown, got %v", err)
	}
}

func TestWriteOverwriteReplacesExistingContent(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")
	path := filepath.Join(workspace, "memory", "notes.md")
	mustWriteMemoryFile(t, path, "old content")

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

	saved, err := m.Write(ctx, "new content", WriteOptions{Path: "memory/notes.md", Overwrite: true})
	if err != nil {
		t.Fatalf("overwrite save: %v", err)
	}
	if saved.Appended {
		t.Fatalf("expected appended=false when overwrite=true")
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if strings.Contains(string(body), "old content") {
		t.Fatalf("expected overwrite to remove old content, got %q", string(body))
	}
	if !strings.Contains(string(body), "new content") {
		t.Fatalf("expected overwrite content in file, got %q", string(body))
	}
}

func TestWriteRejectsBlankContent(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")

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

	if _, err := m.Write(ctx, "   \n\t ", WriteOptions{}); !errors.Is(err, ErrEmptyWriteContent) {
		t.Fatalf("expected ErrEmptyWriteContent, got %v", err)
	}
}
