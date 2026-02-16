package memory

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	KeywordScore float64
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
	candidateMultiplier := 4
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

	mergedChunks := make([]*searchChunk, 0, len(merged))
	for _, chunk := range merged {
		mergedChunks = append(mergedChunks, chunk)
	}

	results := make([]SearchResult, 0, len(mergedChunks))
	for _, chunk := range mergedChunks {
		score := chunk.KeywordScore

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
		if err == nil && len(chunks) > 0 {
			return chunks, nil
		}
	}
	return m.keywordCandidatesLike(ctx, query, limit)
}

func (m *SQLiteIndexManager) keywordCandidatesFTS(ctx context.Context, query string, limit int) ([]searchChunk, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT c.id, c.path, c.source, c.start_line, c.end_line, c.hash, c.text, bm25(chunks_fts)
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
		if err := rows.Scan(&chunk.ID, &chunk.Path, &chunk.Source, &chunk.StartLine, &chunk.EndLine, &chunk.Hash, &chunk.Text, &rank); err != nil {
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
	rows, err := m.db.QueryContext(ctx, `SELECT id, path, source, start_line, end_line, hash, text
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
		if err := rows.Scan(&chunk.ID, &chunk.Path, &chunk.Source, &chunk.StartLine, &chunk.EndLine, &chunk.Hash, &chunk.Text); err != nil {
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
