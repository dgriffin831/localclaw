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

func TestSearchKeywordRankingWinsEvenWhenVectorProviderConfigured(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")

	mustWriteMemoryFile(t, filepath.Join(workspace, "MEMORY.md"), "FIRST shared shared shared")
	mustWriteMemoryFile(t, filepath.Join(workspace, "memory", "notes.md"), "SECOND shared")

	m := NewSQLiteIndexManager(IndexManagerConfig{
		DBPath:        dbPath,
		WorkspaceRoot: workspace,
		ChunkTokens:   64,
		ChunkOverlap:  0,
		Provider:      EmbeddingProviderLocal,
		EnableVector:  true,
		EnableFTS:     false,
	})
	if err := m.Open(ctx); err != nil {
		t.Fatalf("open manager: %v", err)
	}
	defer m.Close()

	if _, err := m.Sync(ctx, true); err != nil {
		t.Fatalf("sync: %v", err)
	}

	m.embeddingProvider = fakeEmbeddingProvider{}

	results, err := m.Search(ctx, "shared", SearchOptions{MaxResults: 2})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Path != "MEMORY.md" {
		t.Fatalf("expected keyword-only top result path MEMORY.md, got %q", results[0].Path)
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
		Provider:      EmbeddingProviderNone,
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

func TestSearchDisableVectorSkipsVectorOnlyFallback(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")

	mustWriteMemoryFile(t, filepath.Join(workspace, "memory", "notes.md"), "SECOND shared")

	m := NewSQLiteIndexManager(IndexManagerConfig{
		DBPath:        dbPath,
		WorkspaceRoot: workspace,
		ChunkTokens:   64,
		ChunkOverlap:  0,
		Provider:      EmbeddingProviderLocal,
		EnableVector:  false,
		EnableFTS:     false,
	})
	if err := m.Open(ctx); err != nil {
		t.Fatalf("open manager: %v", err)
	}
	defer m.Close()

	if _, err := m.Sync(ctx, true); err != nil {
		t.Fatalf("sync: %v", err)
	}

	m.embeddingProvider = fakeEmbeddingProvider{}

	results, err := m.Search(ctx, "nomatch", SearchOptions{MaxResults: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no results when vector is disabled and no keyword matches, got %d", len(results))
	}
}

func TestSearchV2KeywordOnlyDoesNotUseVectorPromotion(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")

	mustWriteMemoryFile(t, filepath.Join(workspace, "MEMORY.md"), "FIRST shared shared shared")
	mustWriteMemoryFile(t, filepath.Join(workspace, "memory", "notes.md"), "SECOND shared")

	m := NewSQLiteIndexManager(IndexManagerConfig{
		DBPath:        dbPath,
		WorkspaceRoot: workspace,
		ChunkTokens:   64,
		ChunkOverlap:  0,
		Provider:      EmbeddingProviderLocal,
		EnableVector:  true,
		EnableFTS:     false,
		HybridEnabled: false,
	})
	if err := m.Open(ctx); err != nil {
		t.Fatalf("open manager: %v", err)
	}
	defer m.Close()

	if _, err := m.Sync(ctx, true); err != nil {
		t.Fatalf("sync: %v", err)
	}

	m.embeddingProvider = fakeEmbeddingProvider{}

	results, err := m.Search(ctx, "shared", SearchOptions{MaxResults: 2})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Path != "MEMORY.md" {
		t.Fatalf("expected keyword-only top result path MEMORY.md, got %q", results[0].Path)
	}
}

func TestSearchV2KeywordOnlyDoesNotReturnVectorOnlyFallbackCandidates(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")

	mustWriteMemoryFile(t, filepath.Join(workspace, "memory", "notes.md"), "SECOND shared")

	m := NewSQLiteIndexManager(IndexManagerConfig{
		DBPath:        dbPath,
		WorkspaceRoot: workspace,
		ChunkTokens:   64,
		ChunkOverlap:  0,
		Provider:      EmbeddingProviderLocal,
		EnableVector:  true,
		EnableFTS:     false,
		HybridEnabled: false,
	})
	if err := m.Open(ctx); err != nil {
		t.Fatalf("open manager: %v", err)
	}
	defer m.Close()

	if _, err := m.Sync(ctx, true); err != nil {
		t.Fatalf("sync: %v", err)
	}

	m.embeddingProvider = fakeEmbeddingProvider{}

	results, err := m.Search(ctx, "nomatch", SearchOptions{MaxResults: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no results when query has no keyword matches, got %d", len(results))
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
		Provider:      EmbeddingProviderNone,
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
		Provider:      EmbeddingProviderNone,
	})
	if err := m.Open(ctx); err != nil {
		t.Fatalf("open manager: %v", err)
	}
	defer m.Close()

	if _, err := m.Get(ctx, "memory/notes.txt", GetOptions{}); !errors.Is(err, ErrMemoryPathNotMarkdown) {
		t.Fatalf("expected ErrMemoryPathNotMarkdown, got %v", err)
	}
}

type fakeEmbeddingProvider struct{}

func (fakeEmbeddingProvider) ProviderName() string { return EmbeddingProviderLocal }
func (fakeEmbeddingProvider) Model() string        { return "test-local" }
func (fakeEmbeddingProvider) ProviderKey() string  { return "test-local" }

func (fakeEmbeddingProvider) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	_ = ctx
	if strings.Contains(strings.ToUpper(text), "SHARED") {
		return []float32{0, 1}, nil
	}
	return []float32{1, 0}, nil
}

func (fakeEmbeddingProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	_ = ctx
	out := make([][]float32, 0, len(texts))
	for _, text := range texts {
		upper := strings.ToUpper(text)
		switch {
		case strings.Contains(upper, "SECOND"):
			out = append(out, []float32{0, 1})
		default:
			out = append(out, []float32{1, 0})
		}
	}
	return out, nil
}
