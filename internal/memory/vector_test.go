package memory

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

type fakeEmbedder struct{}

func (fakeEmbedder) Embed(_ context.Context, input string) ([]float32, error) {
	lower := strings.ToLower(input)
	switch {
	case strings.TrimSpace(lower) == "feline", strings.Contains(lower, "cat"), strings.Contains(lower, "semantic"):
		return normalizeVector([]float32{1, 0}), nil
	case strings.Contains(lower, "banana"), strings.Contains(lower, "keyword"):
		return normalizeVector([]float32{0, 1}), nil
	default:
		return normalizeVector([]float32{0.5, 0.5}), nil
	}
}

func (fakeEmbedder) Close() error { return nil }

func TestVectorSyncIndexesChunkVectorsAndReusesUnchangedChunks(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")
	mustWriteMemoryFile(t, filepath.Join(workspace, "MEMORY.md"), "semantic cat memory")

	m := newVectorTestManager(dbPath, workspace)
	if err := m.Open(ctx); err != nil {
		t.Fatalf("open manager: %v", err)
	}
	defer m.Close()

	first, err := m.Sync(ctx, true)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if first.IndexedVectors == 0 {
		t.Fatalf("expected indexed vectors, got %+v", first)
	}

	second, err := m.Sync(ctx, false)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if second.IndexedVectors != 0 {
		t.Fatalf("expected unchanged chunks to reuse cached vectors, got %+v", second)
	}

	status, err := m.Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.Vector.VectorCount == 0 || status.Vector.Dimension != 2 {
		t.Fatalf("expected vector status count/dimension, got %+v", status.Vector)
	}
}

func TestHybridSearchRanksSemanticVectorMatchAboveKeywordOnlyMatch(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")
	mustWriteMemoryFile(t, filepath.Join(workspace, "MEMORY.md"), "feline keyword weak")
	mustWriteMemoryFile(t, filepath.Join(workspace, "memory", "semantic.md"), "semantic cat memory")

	m := newVectorTestManager(dbPath, workspace)
	if err := m.Open(ctx); err != nil {
		t.Fatalf("open manager: %v", err)
	}
	defer m.Close()
	if _, err := m.Sync(ctx, true); err != nil {
		t.Fatalf("sync: %v", err)
	}

	results, err := m.Search(ctx, "feline", SearchOptions{Mode: SearchModeHybrid, MaxResults: 2})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %+v", results)
	}
	if results[0].Path != "memory/semantic.md" {
		t.Fatalf("expected semantic vector match first, got %+v", results)
	}
	if results[0].VectorScore <= 0 {
		t.Fatalf("expected vector score on semantic result, got %+v", results[0])
	}
}

func TestVectorModeFailsWhenVectorsUnavailable(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")
	mustWriteMemoryFile(t, filepath.Join(workspace, "MEMORY.md"), "alpha")

	m := NewSQLiteIndexManager(IndexManagerConfig{
		DBPath:        dbPath,
		WorkspaceRoot: workspace,
		ChunkTokens:   64,
		ChunkOverlap:  0,
		EnableFTS:     true,
		Vector: VectorConfig{
			Enabled:    true,
			SearchMode: SearchModeVector,
			Model:      VectorModelConfig{ID: "test-model", Path: filepath.Join(t.TempDir(), "missing.gguf"), SHA256: "missing"},
			Server:     VectorServerConfig{Managed: true, Binary: "llama-server", Host: "127.0.0.1", StartupTimeoutSeconds: 1},
		},
	})
	if err := m.Open(ctx); err != nil {
		t.Fatalf("open manager: %v", err)
	}
	defer m.Close()
	if _, err := m.Sync(ctx, true); err != nil {
		t.Fatalf("sync: %v", err)
	}

	if _, err := m.Search(ctx, "alpha", SearchOptions{Mode: SearchModeVector}); err == nil || !strings.Contains(err.Error(), "vector search unavailable") {
		t.Fatalf("expected vector unavailable error, got %v", err)
	}
}

func TestHybridSearchFallsBackToKeywordWithWarning(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")
	mustWriteMemoryFile(t, filepath.Join(workspace, "MEMORY.md"), "alpha fallback")

	m := NewSQLiteIndexManager(IndexManagerConfig{
		DBPath:        dbPath,
		WorkspaceRoot: workspace,
		ChunkTokens:   64,
		ChunkOverlap:  0,
		EnableFTS:     true,
		Vector: VectorConfig{
			Enabled:    true,
			SearchMode: SearchModeHybrid,
			Model:      VectorModelConfig{ID: "test-model", Path: filepath.Join(t.TempDir(), "missing.gguf"), SHA256: "missing"},
			Server:     VectorServerConfig{Managed: true, Binary: "llama-server", Host: "127.0.0.1", StartupTimeoutSeconds: 1},
		},
	})
	if err := m.Open(ctx); err != nil {
		t.Fatalf("open manager: %v", err)
	}
	defer m.Close()
	if _, err := m.Sync(ctx, true); err != nil {
		t.Fatalf("sync: %v", err)
	}

	results, err := m.Search(ctx, "alpha", SearchOptions{Mode: SearchModeHybrid})
	if err != nil {
		t.Fatalf("hybrid search should fall back to keyword: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected keyword fallback result")
	}
	if !strings.Contains(results[0].Warning, "vector search unavailable") {
		t.Fatalf("expected vector warning, got %+v", results[0])
	}
}

func newVectorTestManager(dbPath, workspace string) *SQLiteIndexManager {
	return NewSQLiteIndexManager(IndexManagerConfig{
		DBPath:        dbPath,
		WorkspaceRoot: workspace,
		ChunkTokens:   64,
		ChunkOverlap:  0,
		EnableFTS:     true,
		Vector: VectorConfig{
			Enabled:    true,
			SearchMode: SearchModeHybrid,
			Model:      VectorModelConfig{ID: "test-model", Path: "unused.gguf", SHA256: "unused"},
			Server:     VectorServerConfig{Managed: true, Binary: "llama-server", Host: "127.0.0.1", StartupTimeoutSeconds: 1},
		},
		EmbedderFactory: func(context.Context, VectorConfig) (Embedder, error) {
			return fakeEmbedder{}, nil
		},
	})
}
