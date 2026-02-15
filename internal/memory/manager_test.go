package memory

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSQLiteIndexManagerSyncForceBuildsIndexAndStatus(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")

	mustWriteMemoryFile(t, filepath.Join(workspace, "MEMORY.md"), "alpha\nbeta\ngamma")
	mustWriteMemoryFile(t, filepath.Join(workspace, "memory", "notes.md"), "delta\nepsilon\nzeta")

	m := NewSQLiteIndexManager(IndexManagerConfig{
		DBPath:               dbPath,
		WorkspaceRoot:        workspace,
		ChunkTokens:          2,
		ChunkOverlap:         1,
		Provider:             "none",
		EnableFTS:            true,
		EnableEmbeddingCache: true,
	})
	if err := m.Open(ctx); err != nil {
		t.Fatalf("open manager: %v", err)
	}
	defer m.Close()

	res, err := m.Sync(ctx, true)
	if err != nil {
		t.Fatalf("sync force: %v", err)
	}
	if res.ScannedFiles != 2 {
		t.Fatalf("expected 2 scanned files, got %d", res.ScannedFiles)
	}
	if res.IndexedFiles != 2 {
		t.Fatalf("expected 2 indexed files, got %d", res.IndexedFiles)
	}
	if res.SkippedFiles != 0 {
		t.Fatalf("expected 0 skipped files, got %d", res.SkippedFiles)
	}

	expectedChunks := len(ChunkText("alpha\nbeta\ngamma", 2, 1)) + len(ChunkText("delta\nepsilon\nzeta", 2, 1))
	if res.IndexedChunks != expectedChunks {
		t.Fatalf("expected %d indexed chunks, got %d", expectedChunks, res.IndexedChunks)
	}

	status, err := m.Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.FileCount != 2 {
		t.Fatalf("expected status file count 2, got %d", status.FileCount)
	}
	if status.ChunkCount != expectedChunks {
		t.Fatalf("expected status chunk count %d, got %d", expectedChunks, status.ChunkCount)
	}
}

func TestSQLiteIndexManagerSyncNoChangesMostlyNoop(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")

	mustWriteMemoryFile(t, filepath.Join(workspace, "MEMORY.md"), "alpha\nbeta\ngamma")
	mustWriteMemoryFile(t, filepath.Join(workspace, "memory", "notes.md"), "delta\nepsilon\nzeta")

	m := NewSQLiteIndexManager(IndexManagerConfig{
		DBPath:        dbPath,
		WorkspaceRoot: workspace,
		ChunkTokens:   2,
		ChunkOverlap:  1,
		Provider:      "none",
	})
	if err := m.Open(ctx); err != nil {
		t.Fatalf("open manager: %v", err)
	}
	defer m.Close()

	if _, err := m.Sync(ctx, true); err != nil {
		t.Fatalf("initial sync: %v", err)
	}

	res, err := m.Sync(ctx, false)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if res.IndexedFiles != 0 {
		t.Fatalf("expected no re-indexed files, got %d", res.IndexedFiles)
	}
	if res.SkippedFiles != 2 {
		t.Fatalf("expected 2 skipped files, got %d", res.SkippedFiles)
	}
	if res.IndexedChunks != 0 {
		t.Fatalf("expected no new chunks, got %d", res.IndexedChunks)
	}
}

func TestSQLiteIndexManagerSyncRemovesStaleFiles(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")

	keepFile := filepath.Join(workspace, "MEMORY.md")
	removeFile := filepath.Join(workspace, "memory", "remove.md")
	mustWriteMemoryFile(t, keepFile, "alpha\nbeta")
	mustWriteMemoryFile(t, removeFile, "delta\nepsilon")

	m := NewSQLiteIndexManager(IndexManagerConfig{
		DBPath:        dbPath,
		WorkspaceRoot: workspace,
		ChunkTokens:   2,
		ChunkOverlap:  1,
		Provider:      "none",
	})
	if err := m.Open(ctx); err != nil {
		t.Fatalf("open manager: %v", err)
	}
	defer m.Close()

	if _, err := m.Sync(ctx, true); err != nil {
		t.Fatalf("initial sync: %v", err)
	}

	if err := os.Remove(removeFile); err != nil {
		t.Fatalf("remove stale file: %v", err)
	}

	res, err := m.Sync(ctx, false)
	if err != nil {
		t.Fatalf("sync after remove: %v", err)
	}
	if res.RemovedFiles != 1 {
		t.Fatalf("expected 1 removed file, got %d", res.RemovedFiles)
	}

	status, err := m.Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.FileCount != 1 {
		t.Fatalf("expected status file count 1, got %d", status.FileCount)
	}
}

func mustWriteMemoryFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("mkdir %q: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}
