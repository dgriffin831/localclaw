package memory

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestInstallSchemaDropsLegacyEmbeddingCacheArtifacts(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")

	db, err := openSQLiteDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE embedding_cache (
		provider TEXT,
		model TEXT,
		provider_key TEXT,
		hash TEXT,
		embedding BLOB,
		updated_at INTEGER
	);`); err != nil {
		t.Fatalf("create legacy embedding_cache: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);`); err != nil {
		t.Fatalf("create meta: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES('embedding_cache_enabled', '1');`); err != nil {
		t.Fatalf("seed legacy meta key: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES('embedding_provider', 'local');`); err != nil {
		t.Fatalf("seed legacy embedding_provider meta key: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES('vector_enabled', '1');`); err != nil {
		t.Fatalf("seed legacy vector_enabled meta key: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close sqlite db: %v", err)
	}

	m := NewSQLiteIndexManager(IndexManagerConfig{
		DBPath:        dbPath,
		WorkspaceRoot: t.TempDir(),
		EnableFTS:     true,
	})
	if err := m.Open(ctx); err != nil {
		t.Fatalf("open manager: %v", err)
	}
	defer m.Close()

	if err := m.InstallSchema(ctx); err != nil {
		t.Fatalf("install schema: %v", err)
	}

	var tableCount int
	if err := m.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='embedding_cache';`).Scan(&tableCount); err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	if tableCount != 0 {
		t.Fatalf("expected embedding_cache table to be removed, got count=%d", tableCount)
	}

	var value string
	err = m.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key='embedding_cache_enabled';`).Scan(&value)
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("query legacy meta key: %v", err)
	}
	if err == nil {
		t.Fatalf("expected embedding_cache_enabled meta key to be removed, got value=%q", value)
	}
	err = m.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key='embedding_provider';`).Scan(&value)
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("query legacy embedding_provider meta key: %v", err)
	}
	if err == nil {
		t.Fatalf("expected embedding_provider meta key to be removed, got value=%q", value)
	}
	err = m.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key='vector_enabled';`).Scan(&value)
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("query legacy vector_enabled meta key: %v", err)
	}
	if err == nil {
		t.Fatalf("expected vector_enabled meta key to be removed, got value=%q", value)
	}
}

func TestInstallSchemaRebuildsLegacyChunksTableWithEmbeddingColumns(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")

	db, err := openSQLiteDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE files (
		path TEXT PRIMARY KEY,
		source TEXT NOT NULL,
		hash TEXT NOT NULL,
		mtime INTEGER NOT NULL,
		size INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);`); err != nil {
		t.Fatalf("create files table: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE chunks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		path TEXT NOT NULL,
		source TEXT NOT NULL,
		start_line INTEGER NOT NULL,
		end_line INTEGER NOT NULL,
		hash TEXT NOT NULL,
		text TEXT NOT NULL,
		embedding BLOB,
		model TEXT,
		updated_at INTEGER NOT NULL,
		FOREIGN KEY(path) REFERENCES files(path) ON DELETE CASCADE
	);`); err != nil {
		t.Fatalf("create legacy chunks table: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO files(path, source, hash, mtime, size, updated_at)
		VALUES('/tmp/memory.md', 'memory', 'abc', 0, 0, 1);`); err != nil {
		t.Fatalf("seed files row: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO chunks(path, source, start_line, end_line, hash, text, embedding, model, updated_at)
		VALUES('/tmp/memory.md', 'memory', 1, 1, 'chunk-hash', 'legacy chunk', x'', 'legacy-model', 1);`); err != nil {
		t.Fatalf("seed chunks row: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close sqlite db: %v", err)
	}

	m := NewSQLiteIndexManager(IndexManagerConfig{
		DBPath:        dbPath,
		WorkspaceRoot: t.TempDir(),
		EnableFTS:     true,
	})
	if err := m.Open(ctx); err != nil {
		t.Fatalf("open manager: %v", err)
	}
	defer m.Close()

	if err := m.InstallSchema(ctx); err != nil {
		t.Fatalf("install schema: %v", err)
	}

	var chunkCount int
	if err := m.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chunks WHERE text='legacy chunk';`).Scan(&chunkCount); err != nil {
		t.Fatalf("count migrated chunk rows: %v", err)
	}
	if chunkCount != 1 {
		t.Fatalf("expected migrated chunk row to be preserved, got count=%d", chunkCount)
	}

	rows, err := m.db.QueryContext(ctx, `PRAGMA table_info(chunks);`)
	if err != nil {
		t.Fatalf("inspect chunks table schema: %v", err)
	}
	defer rows.Close()

	seenEmbedding := false
	seenModel := false
	for rows.Next() {
		var (
			cid       int
			name      string
			columnTyp string
			notNull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &columnTyp, &notNull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan chunks table info: %v", err)
		}
		switch name {
		case "embedding":
			seenEmbedding = true
		case "model":
			seenModel = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate chunks table info: %v", err)
	}
	if seenEmbedding || seenModel {
		t.Fatalf("expected legacy embedding/model columns to be removed, embedding=%t model=%t", seenEmbedding, seenModel)
	}
}
