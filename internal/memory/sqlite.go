package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteIndexManager provides local SQLite-backed file/chunk indexing.
type SQLiteIndexManager struct {
	cfg             IndexManagerConfig
	db              *sql.DB
	mu              sync.Mutex
	schemaInstalled bool
	features        schemaFeatures
}

// NewSQLiteIndexManager creates a SQLite index manager.
func NewSQLiteIndexManager(cfg IndexManagerConfig) *SQLiteIndexManager {
	if cfg.ChunkTokens <= 0 {
		cfg.ChunkTokens = defaultChunkTokens
	}
	if cfg.ChunkOverlap < 0 {
		cfg.ChunkOverlap = 0
	}
	if strings.TrimSpace(cfg.Provider) == "" {
		cfg.Provider = "none"
	}
	return &SQLiteIndexManager{cfg: cfg}
}

// Open opens the SQLite database.
func (m *SQLiteIndexManager) Open(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.db != nil {
		return nil
	}
	if strings.TrimSpace(m.cfg.DBPath) == "" {
		return errors.New("memory index db path is empty")
	}

	dbPath, err := filepath.Abs(filepath.Clean(m.cfg.DBPath))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return err
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = ON;`); err != nil {
		db.Close()
		return err
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return err
	}

	m.cfg.DBPath = dbPath
	m.db = db
	return nil
}

// Close closes the SQLite database.
func (m *SQLiteIndexManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.db == nil {
		return nil
	}
	err := m.db.Close()
	m.db = nil
	m.schemaInstalled = false
	m.features = schemaFeatures{}
	return err
}

// InstallSchema installs the index schema if needed.
func (m *SQLiteIndexManager) InstallSchema(ctx context.Context) error {
	if err := m.ensureOpen(ctx); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.schemaInstalled {
		return nil
	}

	features, err := installSchema(ctx, m.db, m.cfg)
	if err != nil {
		return err
	}
	m.features = features
	m.schemaInstalled = true
	return nil
}

// Sync indexes memory markdown files into SQLite.
func (m *SQLiteIndexManager) Sync(ctx context.Context, force bool) (SyncResult, error) {
	var out SyncResult
	if err := m.InstallSchema(ctx); err != nil {
		return out, err
	}
	if strings.TrimSpace(m.cfg.WorkspaceRoot) == "" {
		return out, errors.New("workspace root is empty")
	}

	files, err := DiscoverMemoryFiles(m.cfg.WorkspaceRoot, m.cfg.ExtraPaths)
	if err != nil {
		return out, err
	}
	out.ScannedFiles = len(files)

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()

	existingHashes, err := selectFileHashes(ctx, tx)
	if err != nil {
		return out, err
	}

	discovered := make(map[string]struct{}, len(files))
	now := time.Now().Unix()

	for _, file := range files {
		discovered[file.AbsolutePath] = struct{}{}

		payload, err := os.ReadFile(file.AbsolutePath)
		if err != nil {
			return out, fmt.Errorf("read memory file %q: %w", file.AbsolutePath, err)
		}
		info, err := os.Stat(file.AbsolutePath)
		if err != nil {
			return out, fmt.Errorf("stat memory file %q: %w", file.AbsolutePath, err)
		}

		text := string(payload)
		hash := HashText(text)
		if !force {
			if prevHash, ok := existingHashes[file.AbsolutePath]; ok && prevHash == hash {
				out.SkippedFiles++
				continue
			}
		}

		source := classifyMemorySource(file)
		if _, err := tx.ExecContext(ctx, `INSERT INTO files(path, source, hash, mtime, size, updated_at)
			VALUES(?, ?, ?, ?, ?, ?)
			ON CONFLICT(path) DO UPDATE SET
				source=excluded.source,
				hash=excluded.hash,
				mtime=excluded.mtime,
				size=excluded.size,
				updated_at=excluded.updated_at;`,
			file.AbsolutePath,
			source,
			hash,
			info.ModTime().Unix(),
			info.Size(),
			now,
		); err != nil {
			return out, fmt.Errorf("upsert file row %q: %w", file.AbsolutePath, err)
		}

		if _, err := tx.ExecContext(ctx, `DELETE FROM chunks WHERE path = ?;`, file.AbsolutePath); err != nil {
			return out, fmt.Errorf("delete stale chunks for %q: %w", file.AbsolutePath, err)
		}

		chunks := chunkTextWithLines(text, m.cfg.ChunkTokens, m.cfg.ChunkOverlap)
		for _, chunk := range chunks {
			if _, err := tx.ExecContext(ctx, `INSERT INTO chunks(path, source, start_line, end_line, hash, model, text, embedding, updated_at)
				VALUES(?, ?, ?, ?, ?, ?, ?, NULL, ?);`,
				file.AbsolutePath,
				source,
				chunk.StartLine,
				chunk.EndLine,
				chunk.Hash,
				m.cfg.Model,
				chunk.Text,
				now,
			); err != nil {
				return out, fmt.Errorf("insert chunk for %q: %w", file.AbsolutePath, err)
			}
			out.IndexedChunks++
		}
		out.IndexedFiles++
	}

	for path := range existingHashes {
		if _, ok := discovered[path]; ok {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM files WHERE path = ?;`, path); err != nil {
			return out, fmt.Errorf("delete stale file row %q: %w", path, err)
		}
		out.RemovedFiles++
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES('last_sync_unix', ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value;`, fmt.Sprintf("%d", now)); err != nil {
		return out, fmt.Errorf("update last sync meta: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return out, err
	}

	return out, nil
}

// Status returns indexed file/chunk counts.
func (m *SQLiteIndexManager) Status(ctx context.Context) (IndexStatus, error) {
	if err := m.InstallSchema(ctx); err != nil {
		return IndexStatus{}, err
	}

	var fileCount int
	if err := m.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM files;`).Scan(&fileCount); err != nil {
		return IndexStatus{}, err
	}
	var chunkCount int
	if err := m.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chunks;`).Scan(&chunkCount); err != nil {
		return IndexStatus{}, err
	}

	return IndexStatus{
		DBPath:                m.cfg.DBPath,
		FileCount:             fileCount,
		ChunkCount:            chunkCount,
		FTSEnabled:            m.features.ftsEnabled,
		EmbeddingCacheEnabled: m.features.embeddingCacheEnabled,
	}, nil
}

func (m *SQLiteIndexManager) ensureOpen(ctx context.Context) error {
	m.mu.Lock()
	db := m.db
	m.mu.Unlock()
	if db != nil {
		return nil
	}
	return m.Open(ctx)
}

func selectFileHashes(ctx context.Context, tx *sql.Tx) (map[string]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT path, hash FROM files;`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var path string
		var hash string
		if err := rows.Scan(&path, &hash); err != nil {
			return nil, err
		}
		out[path] = hash
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

type indexedChunk struct {
	Text      string
	Hash      string
	StartLine int
	EndLine   int
}

func chunkTextWithLines(text string, tokens int, overlap int) []indexedChunk {
	if tokens <= 0 {
		return nil
	}

	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}

	chunkSize := tokens * approxCharsPerToken
	if chunkSize <= 0 {
		return nil
	}
	overlapSize := overlap * approxCharsPerToken
	if overlapSize < 0 {
		overlapSize = 0
	}
	if overlapSize >= chunkSize {
		overlapSize = chunkSize - 1
	}

	lineAt := make([]int, len(runes)+1)
	line := 1
	for i, r := range runes {
		lineAt[i] = line
		if r == '\n' {
			line++
		}
	}
	lineAt[len(runes)] = line

	chunks := make([]indexedChunk, 0)
	for start := 0; start < len(runes); {
		end := start + chunkSize
		if end > len(runes) {
			end = len(runes)
		}

		chunkText := string(runes[start:end])
		if strings.TrimSpace(chunkText) != "" {
			startLine := lineAt[start]
			endLine := startLine
			if end > start {
				endLine = lineAt[end-1]
			}
			chunks = append(chunks, indexedChunk{
				Text:      chunkText,
				Hash:      HashText(chunkText),
				StartLine: startLine,
				EndLine:   endLine,
			})
		}

		if end == len(runes) {
			break
		}
		next := end - overlapSize
		if next <= start {
			next = start + chunkSize
		}
		start = next
	}

	return chunks
}

func classifyMemorySource(file MemoryFile) string {
	rel := strings.ToLower(strings.TrimSpace(file.RelativePath))
	if rel == "" {
		return "extra"
	}
	if rel == "memory.md" || rel == "memory/memory.md" || strings.HasPrefix(rel, "memory/") {
		return "memory"
	}
	return "memory"
}
