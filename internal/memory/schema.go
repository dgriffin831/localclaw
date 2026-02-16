package memory

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const schemaVersion = "2"

var coreSchemaStatements = []string{
	`PRAGMA foreign_keys = ON;`,
	`CREATE TABLE IF NOT EXISTS meta (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);`,
	`CREATE TABLE IF NOT EXISTS files (
		path TEXT PRIMARY KEY,
		source TEXT NOT NULL,
		hash TEXT NOT NULL,
		mtime INTEGER NOT NULL,
		size INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);`,
	`CREATE TABLE IF NOT EXISTS chunks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		path TEXT NOT NULL,
		source TEXT NOT NULL,
		start_line INTEGER NOT NULL,
		end_line INTEGER NOT NULL,
		hash TEXT NOT NULL,
		text TEXT NOT NULL,
		updated_at INTEGER NOT NULL,
		FOREIGN KEY(path) REFERENCES files(path) ON DELETE CASCADE
	);`,
	`CREATE INDEX IF NOT EXISTS idx_files_hash ON files(hash);`,
	`CREATE INDEX IF NOT EXISTS idx_chunks_path ON chunks(path);`,
	`CREATE INDEX IF NOT EXISTS idx_chunks_hash ON chunks(hash);`,
	`INSERT INTO meta(key, value) VALUES('schema_version', ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value;`,
}

type schemaFeatures struct {
	ftsEnabled bool
}

func installSchema(ctx context.Context, db *sql.DB, cfg IndexManagerConfig) (schemaFeatures, error) {
	features := schemaFeatures{}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return features, err
	}
	defer tx.Rollback()

	for _, stmt := range coreSchemaStatements[:len(coreSchemaStatements)-1] {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return features, fmt.Errorf("install core schema: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, coreSchemaStatements[len(coreSchemaStatements)-1], schemaVersion); err != nil {
		return features, fmt.Errorf("set schema version: %w", err)
	}

	if cfg.EnableFTS {
		enabled, err := installOptionalFTS(ctx, tx)
		if err != nil {
			return features, err
		}
		features.ftsEnabled = enabled
	}

	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS embedding_cache;`); err != nil {
		return features, fmt.Errorf("drop legacy embedding_cache table: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES('fts_enabled', ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value;`, boolAsString(features.ftsEnabled)); err != nil {
		return features, fmt.Errorf("set fts_enabled meta: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM meta WHERE key = 'embedding_cache_enabled';`); err != nil {
		return features, fmt.Errorf("clear legacy embedding_cache meta: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return features, err
	}
	return features, nil
}

func installOptionalFTS(ctx context.Context, tx *sql.Tx) (bool, error) {
	_, err := tx.ExecContext(ctx, `CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
		text,
		path UNINDEXED,
		source UNINDEXED,
		content='chunks',
		content_rowid='id'
	);`)
	if err != nil {
		lower := strings.ToLower(err.Error())
		if strings.Contains(lower, "no such module") || strings.Contains(lower, "fts5") {
			return false, nil
		}
		return false, fmt.Errorf("install chunks_fts schema: %w", err)
	}

	triggerStatements := []string{
		`CREATE TRIGGER IF NOT EXISTS chunks_ai AFTER INSERT ON chunks BEGIN
			INSERT INTO chunks_fts(rowid, text, path, source) VALUES (new.id, new.text, new.path, new.source);
		END;`,
		`CREATE TRIGGER IF NOT EXISTS chunks_ad AFTER DELETE ON chunks BEGIN
			INSERT INTO chunks_fts(chunks_fts, rowid, text, path, source) VALUES ('delete', old.id, old.text, old.path, old.source);
		END;`,
		`CREATE TRIGGER IF NOT EXISTS chunks_au AFTER UPDATE ON chunks BEGIN
			INSERT INTO chunks_fts(chunks_fts, rowid, text, path, source) VALUES ('delete', old.id, old.text, old.path, old.source);
			INSERT INTO chunks_fts(rowid, text, path, source) VALUES (new.id, new.text, new.path, new.source);
		END;`,
	}
	for _, stmt := range triggerStatements {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return false, fmt.Errorf("install chunks_fts triggers: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO chunks_fts(rowid, text, path, source)
		SELECT id, text, path, source FROM chunks
		WHERE id NOT IN (SELECT rowid FROM chunks_fts);`); err != nil {
		return false, fmt.Errorf("seed chunks_fts: %w", err)
	}

	return true, nil
}

func boolAsString(v bool) string {
	if v {
		return "1"
	}
	return "0"
}
