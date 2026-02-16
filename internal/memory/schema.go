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

	if err := migrateLegacyChunkSchema(ctx, tx); err != nil {
		return features, err
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
	if _, err := tx.ExecContext(ctx, `DELETE FROM meta WHERE key LIKE 'embedding_%' OR key LIKE 'vector_%';`); err != nil {
		return features, fmt.Errorf("clear legacy embedding/vector meta keys: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return features, err
	}
	return features, nil
}

func migrateLegacyChunkSchema(ctx context.Context, tx *sql.Tx) error {
	legacyEmbeddingSchema, err := chunksTableHasLegacyEmbeddingColumns(ctx, tx)
	if err != nil {
		return err
	}
	if !legacyEmbeddingSchema {
		return nil
	}

	resetStatements := []string{
		`DROP TRIGGER IF EXISTS chunks_ai;`,
		`DROP TRIGGER IF EXISTS chunks_ad;`,
		`DROP TRIGGER IF EXISTS chunks_au;`,
		`DROP TABLE IF EXISTS chunks_fts;`,
		`ALTER TABLE chunks RENAME TO chunks_legacy;`,
		`CREATE TABLE chunks (
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
		`CREATE INDEX IF NOT EXISTS idx_chunks_path ON chunks(path);`,
		`CREATE INDEX IF NOT EXISTS idx_chunks_hash ON chunks(hash);`,
	}
	for _, stmt := range resetStatements {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate legacy chunks schema: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO chunks(id, path, source, start_line, end_line, hash, text, updated_at)
		SELECT id, path, source, start_line, end_line, hash, text, updated_at
		FROM chunks_legacy;`); err != nil {
		return fmt.Errorf("copy legacy chunks rows: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS chunks_legacy;`); err != nil {
		return fmt.Errorf("drop legacy chunks table: %w", err)
	}
	return nil
}

func chunksTableHasLegacyEmbeddingColumns(ctx context.Context, tx *sql.Tx) (bool, error) {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(chunks);`)
	if err != nil {
		return false, fmt.Errorf("inspect chunks schema: %w", err)
	}
	defer rows.Close()

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
			return false, fmt.Errorf("scan chunks schema: %w", err)
		}
		if strings.EqualFold(name, "embedding") || strings.EqualFold(name, "model") {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate chunks schema: %w", err)
	}
	return false, nil
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
