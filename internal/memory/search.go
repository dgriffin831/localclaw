package memory

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	defaultSearchMaxResults = 8
	maxSnippetChars         = 280
)

var (
	ErrEmptySearchQuery      = errors.New("search query is empty")
	ErrMemoryPathOutOfScope  = errors.New("memory path is out of scope")
	ErrMemoryPathNotMarkdown = errors.New("memory path must reference a markdown file")
)

type searchChunk struct {
	ID           int64
	Path         string
	Source       string
	StartLine    int
	EndLine      int
	Hash         string
	Text         string
	EmbeddingRaw []byte
	KeywordScore float64
	VectorScore  float64
	MergedScore  float64
}

// Search performs memory_search-style retrieval over indexed chunks.
func (m *SQLiteIndexManager) Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error) {
	if err := m.InstallSchema(ctx); err != nil {
		return nil, err
	}

	trimmedQuery := strings.TrimSpace(query)
	if trimmedQuery == "" {
		return nil, ErrEmptySearchQuery
	}

	maxResults := opts.MaxResults
	if maxResults <= 0 {
		maxResults = defaultSearchMaxResults
	}
	candidateMultiplier := m.cfg.CandidateMultiplier
	if candidateMultiplier <= 0 {
		candidateMultiplier = 1
	}
	candidateLimit := maxResults * candidateMultiplier
	if candidateLimit < maxResults {
		candidateLimit = maxResults
	}

	keywordChunks, err := m.keywordCandidates(ctx, trimmedQuery, candidateLimit)
	if err != nil {
		return nil, err
	}

	merged := map[int64]*searchChunk{}
	for i := range keywordChunks {
		chunk := keywordChunks[i]
		merged[chunk.ID] = &chunk
	}

	provider, vectorEnabled, err := m.vectorProvider(ctx)
	if err != nil {
		return nil, err
	}
	if vectorEnabled && len(merged) == 0 {
		recent, err := m.recentCandidates(ctx, candidateLimit)
		if err != nil {
			return nil, err
		}
		for i := range recent {
			chunk := recent[i]
			if _, ok := merged[chunk.ID]; !ok {
				merged[chunk.ID] = &chunk
			}
		}
	}

	mergedChunks := make([]*searchChunk, 0, len(merged))
	for _, chunk := range merged {
		mergedChunks = append(mergedChunks, chunk)
	}

	if vectorEnabled && len(mergedChunks) > 0 {
		if err := m.attachVectorScores(ctx, provider, trimmedQuery, mergedChunks); err != nil {
			return nil, err
		}
	}

	weightKeyword := m.cfg.KeywordWeight
	weightVector := m.cfg.VectorWeight
	if weightKeyword == 0 && weightVector == 0 {
		weightKeyword = 1
	}

	results := make([]SearchResult, 0, len(mergedChunks))
	for _, chunk := range mergedChunks {
		score := chunk.KeywordScore
		if vectorEnabled && m.cfg.HybridEnabled {
			score = (weightKeyword * chunk.KeywordScore) + (weightVector * chunk.VectorScore)
		} else if vectorEnabled && !m.cfg.HybridEnabled {
			score = chunk.VectorScore
		}
		chunk.MergedScore = score

		if score < opts.MinScore {
			continue
		}

		results = append(results, SearchResult{
			Path:      m.displayPath(chunk.Path),
			StartLine: chunk.StartLine,
			EndLine:   chunk.EndLine,
			Score:     score,
			Snippet:   snippetFromChunk(chunk.Text),
			Source:    chunk.Source,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if results[i].Path != results[j].Path {
			return results[i].Path < results[j].Path
		}
		return results[i].StartLine < results[j].StartLine
	})

	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return results, nil
}

// Get performs memory_get-style file reads with scope and slicing restrictions.
func (m *SQLiteIndexManager) Get(ctx context.Context, path string, opts GetOptions) (GetResult, error) {
	_ = ctx

	file, err := m.resolveAllowedFile(path)
	if err != nil {
		return GetResult{}, err
	}

	contentBytes, err := os.ReadFile(file.AbsolutePath)
	if err != nil {
		return GetResult{}, err
	}

	lines := strings.Split(string(contentBytes), "\n")
	fromLine := opts.FromLine
	if fromLine <= 0 {
		fromLine = 1
	}
	start := fromLine - 1
	if start < 0 {
		start = 0
	}
	if start > len(lines) {
		start = len(lines)
	}

	end := len(lines)
	if opts.Lines > 0 {
		end = start + opts.Lines
		if end > len(lines) {
			end = len(lines)
		}
	}

	slice := lines[start:end]
	startLine := 0
	endLine := 0
	if len(slice) > 0 {
		startLine = start + 1
		endLine = start + len(slice)
	} else {
		startLine = fromLine
		endLine = fromLine - 1
	}

	resultPath := file.RelativePath
	if resultPath == "" {
		resultPath = filepath.ToSlash(filepath.Clean(file.AbsolutePath))
	}

	return GetResult{
		Path:      resultPath,
		StartLine: startLine,
		EndLine:   endLine,
		Content:   strings.Join(slice, "\n"),
		Source:    classifyMemorySource(file),
	}, nil
}

func (m *SQLiteIndexManager) keywordCandidates(ctx context.Context, query string, limit int) ([]searchChunk, error) {
	if m.features.ftsEnabled {
		chunks, err := m.keywordCandidatesFTS(ctx, query, limit)
		if err == nil {
			return chunks, nil
		}
	}
	return m.keywordCandidatesLike(ctx, query, limit)
}

func (m *SQLiteIndexManager) keywordCandidatesFTS(ctx context.Context, query string, limit int) ([]searchChunk, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT c.id, c.path, c.source, c.start_line, c.end_line, c.hash, c.text, c.embedding, bm25(chunks_fts)
		FROM chunks_fts
		JOIN chunks c ON c.id = chunks_fts.rowid
		WHERE chunks_fts MATCH ?
		ORDER BY bm25(chunks_fts)
		LIMIT ?;`, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	chunks := make([]searchChunk, 0)
	for rows.Next() {
		var chunk searchChunk
		var rank sql.NullFloat64
		if err := rows.Scan(&chunk.ID, &chunk.Path, &chunk.Source, &chunk.StartLine, &chunk.EndLine, &chunk.Hash, &chunk.Text, &chunk.EmbeddingRaw, &rank); err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range chunks {
		chunks[i].KeywordScore = reciprocalRank(i)
	}
	return chunks, nil
}

func (m *SQLiteIndexManager) keywordCandidatesLike(ctx context.Context, query string, limit int) ([]searchChunk, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT id, path, source, start_line, end_line, hash, text, embedding
		FROM chunks
		WHERE lower(text) LIKE ?
		ORDER BY updated_at DESC, id ASC
		LIMIT ?;`, "%"+strings.ToLower(query)+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	chunks := make([]searchChunk, 0)
	for rows.Next() {
		var chunk searchChunk
		if err := rows.Scan(&chunk.ID, &chunk.Path, &chunk.Source, &chunk.StartLine, &chunk.EndLine, &chunk.Hash, &chunk.Text, &chunk.EmbeddingRaw); err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	terms := queryTerms(query)
	maxHits := 0
	hits := make([]int, len(chunks))
	for i, chunk := range chunks {
		hits[i] = hitCount(strings.ToLower(chunk.Text), terms)
		if hits[i] > maxHits {
			maxHits = hits[i]
		}
	}
	for i := range chunks {
		if maxHits > 0 {
			chunks[i].KeywordScore = float64(hits[i]) / float64(maxHits)
		} else {
			chunks[i].KeywordScore = reciprocalRank(i)
		}
	}
	return chunks, nil
}

func (m *SQLiteIndexManager) recentCandidates(ctx context.Context, limit int) ([]searchChunk, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT id, path, source, start_line, end_line, hash, text, embedding
		FROM chunks
		ORDER BY updated_at DESC, id DESC
		LIMIT ?;`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	chunks := make([]searchChunk, 0)
	for rows.Next() {
		var chunk searchChunk
		if err := rows.Scan(&chunk.ID, &chunk.Path, &chunk.Source, &chunk.StartLine, &chunk.EndLine, &chunk.Hash, &chunk.Text, &chunk.EmbeddingRaw); err != nil {
			return nil, err
		}
		chunk.KeywordScore = 0
		chunks = append(chunks, chunk)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return chunks, nil
}

func (m *SQLiteIndexManager) attachVectorScores(ctx context.Context, provider EmbeddingProvider, query string, chunks []*searchChunk) error {
	queryEmbedding, err := provider.EmbedQuery(ctx, query)
	if err != nil {
		return err
	}

	missingIdx := make([]int, 0)
	missingTexts := make([]string, 0)
	chunkEmbeddings := make([][]float32, len(chunks))
	cacheProvider := strings.TrimSpace(provider.ProviderName())
	cacheModel := strings.TrimSpace(provider.Model())
	cacheProviderKey := strings.TrimSpace(provider.ProviderKey())
	cacheEnabled := m.features.embeddingCacheEnabled && cacheProvider != "" && cacheProviderKey != ""

	for i, chunk := range chunks {
		if len(chunk.EmbeddingRaw) > 0 {
			embedding, err := decodeEmbedding(chunk.EmbeddingRaw)
			if err == nil && len(embedding) > 0 {
				chunkEmbeddings[i] = embedding
				continue
			}
		}

		if cacheEnabled {
			cached, ok, err := m.lookupCachedEmbedding(ctx, cacheProvider, cacheModel, cacheProviderKey, chunk.Hash)
			if err != nil {
				return err
			}
			if ok && len(cached) > 0 {
				chunkEmbeddings[i] = cached
				raw := encodeEmbedding(cached)
				chunks[i].EmbeddingRaw = raw
				_, _ = m.db.ExecContext(ctx, `UPDATE chunks SET embedding = ? WHERE id = ?;`, raw, chunks[i].ID)
				continue
			}
		}

		missingIdx = append(missingIdx, i)
		missingTexts = append(missingTexts, chunk.Text)
	}

	if len(missingTexts) > 0 {
		embeddings, err := provider.EmbedBatch(ctx, missingTexts)
		if err != nil {
			return err
		}
		for i, chunkIndex := range missingIdx {
			if i >= len(embeddings) {
				break
			}
			chunkEmbeddings[chunkIndex] = embeddings[i]
			raw := encodeEmbedding(embeddings[i])
			chunks[chunkIndex].EmbeddingRaw = raw
			_, _ = m.db.ExecContext(ctx, `UPDATE chunks SET embedding = ? WHERE id = ?;`, raw, chunks[chunkIndex].ID)
			if cacheEnabled {
				if err := m.storeCachedEmbedding(ctx, cacheProvider, cacheModel, cacheProviderKey, chunks[chunkIndex].Hash, raw); err != nil {
					return err
				}
			}
		}
	}

	if cacheEnabled && m.cfg.EmbeddingCacheMax > 0 {
		if err := m.pruneEmbeddingCache(ctx, cacheProvider, cacheModel, cacheProviderKey, m.cfg.EmbeddingCacheMax); err != nil {
			return err
		}
	}

	for i := range chunks {
		if len(chunkEmbeddings[i]) == 0 {
			continue
		}
		chunks[i].VectorScore = cosineSimilarity(queryEmbedding, chunkEmbeddings[i])
	}
	return nil
}

func (m *SQLiteIndexManager) lookupCachedEmbedding(ctx context.Context, provider string, model string, providerKey string, hash string) ([]float32, bool, error) {
	if strings.TrimSpace(hash) == "" {
		return nil, false, nil
	}

	var raw []byte
	err := m.db.QueryRowContext(ctx, `SELECT embedding
		FROM embedding_cache
		WHERE provider = ? AND model = ? AND provider_key = ? AND hash = ?;`,
		provider, model, providerKey, hash,
	).Scan(&raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	embedding, err := decodeEmbedding(raw)
	if err != nil {
		return nil, false, nil
	}
	return embedding, true, nil
}

func (m *SQLiteIndexManager) storeCachedEmbedding(ctx context.Context, provider string, model string, providerKey string, hash string, embedding []byte) error {
	if strings.TrimSpace(hash) == "" || len(embedding) == 0 {
		return nil
	}
	_, err := m.db.ExecContext(ctx, `INSERT INTO embedding_cache(provider, model, provider_key, hash, embedding, updated_at)
		VALUES(?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider, model, provider_key, hash) DO UPDATE SET
			embedding=excluded.embedding,
			updated_at=excluded.updated_at;`,
		provider, model, providerKey, hash, embedding, time.Now().Unix(),
	)
	return err
}

func (m *SQLiteIndexManager) pruneEmbeddingCache(ctx context.Context, provider string, model string, providerKey string, maxEntries int) error {
	if maxEntries <= 0 {
		return nil
	}
	_, err := m.db.ExecContext(ctx, `DELETE FROM embedding_cache
		WHERE provider = ? AND model = ? AND provider_key = ?
			AND rowid NOT IN (
				SELECT rowid
				FROM embedding_cache
				WHERE provider = ? AND model = ? AND provider_key = ?
				ORDER BY updated_at DESC, rowid DESC
				LIMIT ?
			);`,
		provider, model, providerKey,
		provider, model, providerKey,
		maxEntries,
	)
	return err
}

func (m *SQLiteIndexManager) vectorProvider(ctx context.Context) (EmbeddingProvider, bool, error) {
	if !m.cfg.EnableVector {
		return nil, false, nil
	}
	if m.embeddingProvider != nil {
		return m.embeddingProvider, strings.EqualFold(m.embeddingProvider.ProviderName(), EmbeddingProviderLocal), nil
	}
	if !strings.EqualFold(m.cfg.Provider, EmbeddingProviderLocal) {
		return nil, false, nil
	}

	resolution, err := ResolveEmbeddingProvider(EmbeddingProviderConfig{
		Provider: m.cfg.Provider,
		Fallback: m.cfg.Fallback,
		Model:    m.cfg.Model,
		Local:    m.cfg.Local,
	})
	if err != nil {
		return nil, false, err
	}
	if !strings.EqualFold(resolution.ProviderName, EmbeddingProviderLocal) {
		return nil, false, nil
	}
	m.embeddingProvider = resolution.Provider
	_ = ctx
	return m.embeddingProvider, true, nil
}

func (m *SQLiteIndexManager) resolveAllowedFile(path string) (MemoryFile, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return MemoryFile{}, ErrMemoryPathOutOfScope
	}
	if !isMarkdownFile(trimmed) {
		return MemoryFile{}, ErrMemoryPathNotMarkdown
	}

	workspaceRoot := strings.TrimSpace(m.cfg.WorkspaceRoot)
	if workspaceRoot == "" {
		return MemoryFile{}, errors.New("workspace root is empty")
	}

	allowed, err := DiscoverMemoryFiles(workspaceRoot, m.cfg.ExtraPaths)
	if err != nil {
		return MemoryFile{}, err
	}
	allowedByAbs := make(map[string]MemoryFile, len(allowed))
	allowedByRel := make(map[string]MemoryFile, len(allowed))
	for _, file := range allowed {
		allowedByAbs[filepath.Clean(file.AbsolutePath)] = file
		if file.RelativePath != "" {
			allowedByRel[file.RelativePath] = file
		}
	}

	if filepath.IsAbs(trimmed) {
		abs := filepath.Clean(trimmed)
		if file, ok := allowedByAbs[abs]; ok {
			return file, nil
		}
		return MemoryFile{}, ErrMemoryPathOutOfScope
	}

	normalizedRel, err := NormalizeRelativePath(trimmed)
	if err != nil {
		if errors.Is(err, errPathOutsideRoot) {
			return MemoryFile{}, ErrMemoryPathOutOfScope
		}
		return MemoryFile{}, ErrMemoryPathOutOfScope
	}
	if file, ok := allowedByRel[normalizedRel]; ok {
		return file, nil
	}
	return MemoryFile{}, ErrMemoryPathOutOfScope
}

func (m *SQLiteIndexManager) displayPath(path string) string {
	rel, err := SafeRelativePath(m.cfg.WorkspaceRoot, path)
	if err == nil {
		return rel
	}
	return filepath.ToSlash(filepath.Clean(path))
}

func snippetFromChunk(text string) string {
	snippet := strings.Join(strings.Fields(text), " ")
	if len(snippet) <= maxSnippetChars {
		return snippet
	}
	return strings.TrimSpace(snippet[:maxSnippetChars])
}

func reciprocalRank(index int) float64 {
	return 1.0 / float64(index+1)
}

func queryTerms(query string) []string {
	parts := strings.Fields(strings.ToLower(query))
	terms := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		terms = append(terms, part)
	}
	if len(terms) == 0 {
		terms = append(terms, strings.ToLower(strings.TrimSpace(query)))
	}
	return terms
}

func hitCount(text string, terms []string) int {
	total := 0
	for _, term := range terms {
		if term == "" {
			continue
		}
		idx := 0
		for {
			offset := strings.Index(text[idx:], term)
			if offset < 0 {
				break
			}
			total++
			idx += offset + len(term)
			if idx >= len(text) {
				break
			}
		}
	}
	return total
}

func cosineSimilarity(a []float32, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot float64
	var normA float64
	var normB float64
	for i := range a {
		av := float64(a[i])
		bv := float64(b[i])
		dot += av * bv
		normA += av * av
		normB += bv * bv
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func encodeEmbedding(v []float32) []byte {
	if len(v) == 0 {
		return nil
	}
	buf := make([]byte, len(v)*4)
	for i, value := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(value))
	}
	return buf
}

func decodeEmbedding(raw []byte) ([]float32, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if len(raw)%4 != 0 {
		return nil, fmt.Errorf("invalid embedding byte length %d", len(raw))
	}
	out := make([]float32, len(raw)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
	}
	return out, nil
}
