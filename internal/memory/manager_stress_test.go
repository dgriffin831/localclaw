package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestSQLiteIndexManagerStressIndexesManyFiles(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")

	const fileCount = 160
	for i := 0; i < fileCount; i++ {
		path := filepath.Join(workspace, "memory", fmt.Sprintf("note-%03d.md", i))
		mustWriteMemoryFile(t, path, fmt.Sprintf("entry %d\ncommon stress token", i))
	}

	m := NewSQLiteIndexManager(IndexManagerConfig{
		DBPath:        dbPath,
		WorkspaceRoot: workspace,
		ChunkTokens:   16,
		ChunkOverlap:  2,
		Provider:      EmbeddingProviderNone,
		EnableFTS:     true,
	})
	if err := m.Open(ctx); err != nil {
		t.Fatalf("open manager: %v", err)
	}
	defer m.Close()

	res, err := m.Sync(ctx, true)
	if err != nil {
		t.Fatalf("force sync: %v", err)
	}
	if res.ScannedFiles != fileCount {
		t.Fatalf("expected %d scanned files, got %d", fileCount, res.ScannedFiles)
	}

	results, err := m.Search(ctx, "common stress token", SearchOptions{MaxResults: 25})
	if err != nil {
		t.Fatalf("search stress token: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected non-empty search results after stress indexing")
	}
}

func TestSQLiteIndexManagerConcurrentSyncRequests(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")

	mustWriteMemoryFile(t, filepath.Join(workspace, "MEMORY.md"), "alpha base")
	for i := 0; i < 30; i++ {
		path := filepath.Join(workspace, "memory", fmt.Sprintf("doc-%02d.md", i))
		mustWriteMemoryFile(t, path, fmt.Sprintf("doc-%d baseline", i))
	}

	m := NewSQLiteIndexManager(IndexManagerConfig{
		DBPath:        dbPath,
		WorkspaceRoot: workspace,
		ChunkTokens:   24,
		ChunkOverlap:  3,
		Provider:      EmbeddingProviderNone,
		EnableFTS:     true,
	})
	if err := m.Open(ctx); err != nil {
		t.Fatalf("open manager: %v", err)
	}
	defer m.Close()

	if _, err := m.Sync(ctx, true); err != nil {
		t.Fatalf("initial sync: %v", err)
	}

	const workers = 8
	const iterations = 15
	errCh := make(chan error, workers*iterations)
	var wg sync.WaitGroup

	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				if i%5 == 0 {
					if err := os.WriteFile(filepath.Join(workspace, "MEMORY.md"), []byte(fmt.Sprintf("alpha worker %d iteration %d", worker, i)), 0o600); err != nil {
						errCh <- fmt.Errorf("worker %d rewrite memory iteration %d: %w", worker, i, err)
						return
					}
				}
				force := i%7 == 0
				if _, err := m.Sync(ctx, force); err != nil {
					errCh <- fmt.Errorf("worker %d sync iteration %d: %w", worker, i, err)
					return
				}
			}
		}(worker)
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}

	results, err := m.Search(ctx, "alpha", SearchOptions{MaxResults: 3})
	if err != nil {
		t.Fatalf("final search: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected indexed results after concurrent sync stress")
	}
}
