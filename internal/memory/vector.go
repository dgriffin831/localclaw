package memory

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

const (
	SearchModeHybrid  = "hybrid"
	SearchModeKeyword = "keyword"
	SearchModeVector  = "vector"
)

func normalizeSearchMode(value string, fallback string) string {
	mode := strings.ToLower(strings.TrimSpace(value))
	if mode == "" {
		mode = strings.ToLower(strings.TrimSpace(fallback))
	}
	if mode == "" {
		mode = SearchModeHybrid
	}
	switch mode {
	case SearchModeHybrid, SearchModeKeyword, SearchModeVector:
		return mode
	default:
		return SearchModeHybrid
	}
}

func normalizeVector(values []float32) []float32 {
	var sum float64
	for _, value := range values {
		sum += float64(value * value)
	}
	if sum == 0 {
		return values
	}
	scale := float32(1 / math.Sqrt(sum))
	out := make([]float32, len(values))
	for i, value := range values {
		out[i] = value * scale
	}
	return out
}

func encodeFloat32Vector(values []float32) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, len(values)*4))
	if err := binary.Write(buf, binary.LittleEndian, values); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decodeFloat32Vector(data []byte) ([]float32, error) {
	if len(data)%4 != 0 {
		return nil, errors.New("vector blob length is not divisible by 4")
	}
	out := make([]float32, len(data)/4)
	if err := binary.Read(bytes.NewReader(data), binary.LittleEndian, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func dotProduct(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var out float64
	for i := range a {
		out += float64(a[i] * b[i])
	}
	return out
}

func (m *SQLiteIndexManager) vectorEnabled() bool {
	return m.cfg.Vector.Enabled && strings.TrimSpace(m.cfg.Vector.Model.ID) != ""
}

func (m *SQLiteIndexManager) vectorReady(ctx context.Context) (bool, string) {
	if !m.vectorEnabled() {
		return false, "vector search is disabled"
	}
	if m.cfg.EmbedderFactory == nil {
		status := VectorModelStatus(m.cfg.Vector)
		if !status.Verified {
			return false, "vector model is missing or sha256 verification failed"
		}
		if !status.LlamaServerFound {
			return false, "llama-server binary is not available"
		}
	}
	count, dimension, err := m.vectorStats(ctx)
	if err != nil {
		return false, err.Error()
	}
	if count == 0 || dimension == 0 {
		return false, "no chunk vectors are indexed"
	}
	return true, ""
}

func (m *SQLiteIndexManager) vectorStats(ctx context.Context) (int, int, error) {
	if err := m.InstallSchema(ctx); err != nil {
		return 0, 0, err
	}
	var count int
	var dimension sql.NullInt64
	err := m.db.QueryRowContext(ctx, `SELECT COUNT(*), MAX(dimension) FROM chunk_vectors WHERE model_id = ?;`, m.cfg.Vector.Model.ID).Scan(&count, &dimension)
	if err != nil {
		return 0, 0, err
	}
	if !dimension.Valid {
		return count, 0, nil
	}
	return count, int(dimension.Int64), nil
}

func (m *SQLiteIndexManager) embedMissingVectors(ctx context.Context, db *sql.DB) (int, string) {
	if !m.vectorEnabled() {
		return 0, ""
	}
	if m.cfg.EmbedderFactory == nil {
		status := VectorModelStatus(m.cfg.Vector)
		if !status.Verified {
			return 0, "vector model is missing or sha256 verification failed"
		}
		if !status.LlamaServerFound {
			return 0, "llama-server binary is not available"
		}
	}

	embedder, err := m.newEmbedder(ctx)
	if err != nil {
		return 0, err.Error()
	}
	defer embedder.Close()

	rows, err := db.QueryContext(ctx, `SELECT c.id, c.hash, c.text
		FROM chunks c
		LEFT JOIN chunk_vectors v ON v.chunk_id = c.id AND v.model_id = ?
		WHERE v.chunk_id IS NULL
		ORDER BY c.id ASC;`, m.cfg.Vector.Model.ID)
	if err != nil {
		return 0, err.Error()
	}
	defer rows.Close()

	type missingChunk struct {
		ID   int64
		Hash string
		Text string
	}
	missing := []missingChunk{}
	for rows.Next() {
		var chunk missingChunk
		if err := rows.Scan(&chunk.ID, &chunk.Hash, &chunk.Text); err != nil {
			return 0, err.Error()
		}
		missing = append(missing, chunk)
	}
	if err := rows.Err(); err != nil {
		return 0, err.Error()
	}

	count := 0
	for _, chunk := range missing {
		vector, err := embedder.Embed(ctx, chunk.Text)
		if err != nil {
			return count, err.Error()
		}
		encoded, err := encodeFloat32Vector(vector)
		if err != nil {
			return count, err.Error()
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO chunk_vectors(chunk_id, chunk_hash, model_id, dimension, vector, updated_at)
			VALUES(?, ?, ?, ?, ?, strftime('%s','now'))
			ON CONFLICT(chunk_id, model_id) DO UPDATE SET
				chunk_hash=excluded.chunk_hash,
				dimension=excluded.dimension,
				vector=excluded.vector,
				updated_at=excluded.updated_at;`,
			chunk.ID, chunk.Hash, m.cfg.Vector.Model.ID, len(vector), encoded); err != nil {
			return count, err.Error()
		}
		count++
	}
	return count, ""
}

func (m *SQLiteIndexManager) vectorCandidates(ctx context.Context, query string, limit int) ([]searchChunk, string, error) {
	ready, reason := m.vectorReady(ctx)
	if !ready {
		return nil, reason, nil
	}
	embedder, err := m.newEmbedder(ctx)
	if err != nil {
		return nil, err.Error(), nil
	}
	defer embedder.Close()
	queryVector, err := embedder.Embed(ctx, query)
	if err != nil {
		return nil, err.Error(), nil
	}

	rows, err := m.db.QueryContext(ctx, `SELECT c.id, c.path, c.source, c.start_line, c.end_line, c.hash, c.text, v.vector
		FROM chunks c
		JOIN chunk_vectors v ON v.chunk_id = c.id
		WHERE v.model_id = ?;`, m.cfg.Vector.Model.ID)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	chunks := []searchChunk{}
	for rows.Next() {
		var chunk searchChunk
		var blob []byte
		if err := rows.Scan(&chunk.ID, &chunk.Path, &chunk.Source, &chunk.StartLine, &chunk.EndLine, &chunk.Hash, &chunk.Text, &blob); err != nil {
			return nil, "", err
		}
		vector, err := decodeFloat32Vector(blob)
		if err != nil {
			continue
		}
		chunk.VectorScore = dotProduct(queryVector, vector)
		chunks = append(chunks, chunk)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	sort.Slice(chunks, func(i, j int) bool {
		if chunks[i].VectorScore != chunks[j].VectorScore {
			return chunks[i].VectorScore > chunks[j].VectorScore
		}
		return chunks[i].ID < chunks[j].ID
	})
	if len(chunks) > limit {
		chunks = chunks[:limit]
	}
	return chunks, "", nil
}

func mergeSearchChunks(keywordChunks, vectorChunks []searchChunk) []*searchChunk {
	merged := map[int64]*searchChunk{}
	for i := range keywordChunks {
		chunk := keywordChunks[i]
		merged[chunk.ID] = &chunk
	}
	for i := range vectorChunks {
		chunk := vectorChunks[i]
		if existing, ok := merged[chunk.ID]; ok {
			existing.VectorScore = chunk.VectorScore
			continue
		}
		merged[chunk.ID] = &chunk
	}
	out := make([]*searchChunk, 0, len(merged))
	for _, chunk := range merged {
		if chunk.KeywordScore > 0 && chunk.VectorScore > 0 {
			chunk.HybridScore = (chunk.KeywordScore * 0.55) + (chunk.VectorScore * 0.45)
		} else if chunk.KeywordScore > 0 {
			chunk.HybridScore = chunk.KeywordScore
		} else {
			chunk.HybridScore = chunk.VectorScore + 0.05
		}
		out = append(out, chunk)
	}
	return out
}

func vectorUnavailableError(reason string) error {
	if strings.TrimSpace(reason) == "" {
		reason = "vector search is unavailable"
	}
	return fmt.Errorf("vector search unavailable: %s", reason)
}

func (m *SQLiteIndexManager) newEmbedder(ctx context.Context) (Embedder, error) {
	if m.cfg.EmbedderFactory != nil {
		return m.cfg.EmbedderFactory(ctx, m.cfg.Vector)
	}
	return NewManagedEmbedder(ctx, m.cfg.Vector)
}
