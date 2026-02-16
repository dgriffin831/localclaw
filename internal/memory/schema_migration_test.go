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
}
