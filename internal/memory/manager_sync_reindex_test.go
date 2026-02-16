package memory

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestSQLiteIndexManagerSafeReindexFailurePreservesExistingIndex(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")

	mustWriteMemoryFile(t, filepath.Join(workspace, "MEMORY.md"), "alpha memory survives")

	m := NewSQLiteIndexManager(IndexManagerConfig{
		DBPath:        dbPath,
		WorkspaceRoot: workspace,
		ChunkTokens:   4,
		ChunkOverlap:  0,
	})
	if err := m.Open(ctx); err != nil {
		t.Fatalf("open manager: %v", err)
	}
	defer m.Close()

	if _, err := m.Sync(ctx, true); err != nil {
		t.Fatalf("initial sync: %v", err)
	}

	baseline, err := m.Search(ctx, "alpha", SearchOptions{MaxResults: 5})
	if err != nil {
		t.Fatalf("baseline search: %v", err)
	}
	if len(baseline) == 0 {
		t.Fatalf("expected baseline index to contain alpha")
	}

	mustWriteMemoryFile(t, filepath.Join(workspace, "MEMORY.md"), "beta should not replace on failed swap")
	m.testHookBeforeReindexSwap = func(_ string) error {
		return errors.New("simulated swap failure")
	}
	t.Cleanup(func() { m.testHookBeforeReindexSwap = nil })

	if _, err := m.Sync(ctx, true); err == nil {
		t.Fatalf("expected force sync failure from simulated swap failure")
	}

	afterFailAlpha, err := m.Search(ctx, "alpha", SearchOptions{MaxResults: 5})
	if err != nil {
		t.Fatalf("search alpha after failed swap: %v", err)
	}
	if len(afterFailAlpha) == 0 {
		t.Fatalf("expected old index data to remain after failed swap")
	}

	afterFailBeta, err := m.Search(ctx, "beta", SearchOptions{MaxResults: 5})
	if err != nil {
		t.Fatalf("search beta after failed swap: %v", err)
	}
	if len(afterFailBeta) != 0 {
		t.Fatalf("expected failed swap to avoid partial replacement, got beta results: %d", len(afterFailBeta))
	}
}
