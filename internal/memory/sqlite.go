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

	"github.com/dgriffin831/localclaw/internal/session"
	_ "modernc.org/sqlite"
)

const (
	defaultWatchDebounce     = 500 * time.Millisecond
	defaultSessionDebounce   = 500 * time.Millisecond
	defaultWatchPollInterval = 250 * time.Millisecond
)

// SQLiteIndexManager provides local SQLite-backed file/chunk indexing.
type SQLiteIndexManager struct {
	cfg               IndexManagerConfig
	db                *sql.DB
	embeddingProvider EmbeddingProvider
	mu                sync.Mutex
	schemaInstalled   bool
	features          schemaFeatures
	syncMu            sync.Mutex

	stateMu           sync.Mutex
	dirty             bool
	lastBackgroundErr error
	watchSnapshot     map[string]string
	autoCancel        context.CancelFunc
	autoDone          chan struct{}
	autoTriggerCh     chan autoSyncTrigger
	sessionDeltaBytes int
	sessionDeltaMsgs  int

	// test hooks exercised by package tests.
	testHookSync              func(ctx context.Context, force bool) error
	testHookBeforeReindexSwap func(tempPath string) error
}

type autoSyncTrigger int

const (
	autoSyncTriggerSession autoSyncTrigger = iota + 1
)

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
	if cfg.CandidateMultiplier <= 0 {
		cfg.CandidateMultiplier = 4
	}
	if cfg.VectorWeight == 0 && cfg.KeywordWeight == 0 {
		cfg.VectorWeight = 0.8
		cfg.KeywordWeight = 0.2
	}
	if cfg.VectorWeight < 0 {
		cfg.VectorWeight = 0
	}
	if cfg.KeywordWeight < 0 {
		cfg.KeywordWeight = 0
	}
	if len(cfg.Sources) == 0 {
		cfg.Sources = []string{"memory"}
	}
	if cfg.SessionDeltaBytes < 0 {
		cfg.SessionDeltaBytes = 0
	}
	if cfg.SessionDeltaMessages < 0 {
		cfg.SessionDeltaMessages = 0
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
	db, err := openSQLiteDB(ctx, dbPath)
	if err != nil {
		return err
	}

	m.cfg.DBPath = dbPath
	m.db = db
	return nil
}

// Close closes the SQLite database.
func (m *SQLiteIndexManager) Close() error {
	_ = m.StopAutoSync()

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
	m.syncMu.Lock()
	defer m.syncMu.Unlock()

	if m.testHookSync != nil {
		if err := m.testHookSync(ctx, force); err != nil {
			m.setDirty(true)
			return SyncResult{}, err
		}
	}

	var (
		out SyncResult
		err error
	)
	if force {
		out, err = m.syncWithSafeReindexSwap(ctx)
	} else {
		if err = m.InstallSchema(ctx); err == nil {
			out, err = m.syncIntoDB(ctx, m.db, false)
		}
	}
	if err != nil {
		m.setDirty(true)
		return SyncResult{}, err
	}

	m.setDirty(false)
	if snapshot, snapErr := m.scanWatchSnapshot(); snapErr == nil {
		m.stateMu.Lock()
		m.watchSnapshot = snapshot
		m.stateMu.Unlock()
	}
	return out, nil
}

// StartAutoSync starts background watch/interval sync loops.
func (m *SQLiteIndexManager) StartAutoSync(ctx context.Context, cfg AutoSyncConfig) error {
	if !cfg.Watch && cfg.Interval <= 0 && !m.sourceEnabled("sessions") {
		return nil
	}
	if cfg.WatchDebounce <= 0 {
		cfg.WatchDebounce = defaultWatchDebounce
	}
	if cfg.SessionDebounce <= 0 {
		cfg.SessionDebounce = defaultSessionDebounce
	}
	if cfg.WatchPollInterval <= 0 {
		cfg.WatchPollInterval = defaultWatchPollInterval
	}

	if _, err := m.Status(ctx); err != nil {
		return err
	}

	if snapshot, err := m.scanWatchSnapshot(); err == nil {
		m.stateMu.Lock()
		m.watchSnapshot = snapshot
		m.stateMu.Unlock()
	}

	m.stateMu.Lock()
	if m.autoCancel != nil {
		m.stateMu.Unlock()
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	triggerCh := make(chan autoSyncTrigger, 1)
	m.autoCancel = cancel
	m.autoDone = done
	m.autoTriggerCh = triggerCh
	m.sessionDeltaBytes = 0
	m.sessionDeltaMsgs = 0
	m.stateMu.Unlock()

	go m.runAutoSync(runCtx, done, triggerCh, cfg)
	return nil
}

// StopAutoSync stops background watch/interval sync loops.
func (m *SQLiteIndexManager) StopAutoSync() error {
	m.stateMu.Lock()
	cancel := m.autoCancel
	done := m.autoDone
	m.autoCancel = nil
	m.autoDone = nil
	m.autoTriggerCh = nil
	m.sessionDeltaBytes = 0
	m.sessionDeltaMsgs = 0
	m.stateMu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	return nil
}

// Dirty reports whether pending changes or failed syncs have marked the index dirty.
func (m *SQLiteIndexManager) Dirty() bool {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	return m.dirty
}

// LastBackgroundError returns the latest background autosync error.
func (m *SQLiteIndexManager) LastBackgroundError() error {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	return m.lastBackgroundErr
}

// HandleTranscriptUpdate handles session transcript append notifications.
func (m *SQLiteIndexManager) HandleTranscriptUpdate(ctx context.Context, update session.TranscriptUpdate) error {
	_ = ctx
	if !m.sourceEnabled("sessions") {
		return nil
	}

	shouldTrigger := false
	m.stateMu.Lock()
	m.sessionDeltaBytes += maxInt(update.DeltaBytes, 0)
	m.sessionDeltaMsgs += maxInt(update.DeltaMessages, 0)
	byteThreshold := m.cfg.SessionDeltaBytes
	msgThreshold := m.cfg.SessionDeltaMessages
	if byteThreshold <= 0 && msgThreshold <= 0 {
		shouldTrigger = m.sessionDeltaBytes > 0 || m.sessionDeltaMsgs > 0
	} else {
		if byteThreshold > 0 && m.sessionDeltaBytes >= byteThreshold {
			shouldTrigger = true
		}
		if msgThreshold > 0 && m.sessionDeltaMsgs >= msgThreshold {
			shouldTrigger = true
		}
	}
	if shouldTrigger {
		m.sessionDeltaBytes = 0
		m.sessionDeltaMsgs = 0
	}
	triggerCh := m.autoTriggerCh
	m.stateMu.Unlock()

	if shouldTrigger {
		m.setDirty(true)
		if triggerCh != nil {
			select {
			case triggerCh <- autoSyncTriggerSession:
			default:
			}
		}
	}
	return nil
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

func (m *SQLiteIndexManager) syncWithSafeReindexSwap(ctx context.Context) (SyncResult, error) {
	if strings.TrimSpace(m.cfg.WorkspaceRoot) == "" {
		return SyncResult{}, errors.New("workspace root is empty")
	}

	tempPath := fmt.Sprintf("%s.reindex.%d.tmp", m.cfg.DBPath, time.Now().UnixNano())
	tempDB, err := openSQLiteDB(ctx, tempPath)
	if err != nil {
		return SyncResult{}, err
	}
	features, err := installSchema(ctx, tempDB, m.cfg)
	if err != nil {
		_ = tempDB.Close()
		_ = os.Remove(tempPath)
		return SyncResult{}, err
	}
	result, err := m.syncIntoDB(ctx, tempDB, true)
	closeErr := tempDB.Close()
	if err != nil {
		_ = os.Remove(tempPath)
		return SyncResult{}, err
	}
	if closeErr != nil {
		_ = os.Remove(tempPath)
		return SyncResult{}, closeErr
	}

	if m.testHookBeforeReindexSwap != nil {
		if err := m.testHookBeforeReindexSwap(tempPath); err != nil {
			_ = os.Remove(tempPath)
			return SyncResult{}, err
		}
	}

	if err := m.swapDBFile(ctx, tempPath, features); err != nil {
		_ = os.Remove(tempPath)
		return SyncResult{}, err
	}
	return result, nil
}

func (m *SQLiteIndexManager) swapDBFile(ctx context.Context, tempPath string, features schemaFeatures) error {
	dbPath := m.cfg.DBPath
	backupPath := fmt.Sprintf("%s.reindex.backup.%d", dbPath, time.Now().UnixNano())

	m.mu.Lock()
	oldDB := m.db
	m.db = nil
	m.schemaInstalled = false
	m.features = schemaFeatures{}
	m.mu.Unlock()

	if oldDB != nil {
		if err := oldDB.Close(); err != nil {
			return err
		}
	}

	haveBackup := false
	if err := os.Rename(dbPath, backupPath); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
	} else {
		haveBackup = true
	}

	if err := os.Rename(tempPath, dbPath); err != nil {
		if haveBackup {
			_ = os.Rename(backupPath, dbPath)
		}
		reopened, reopenErr := openSQLiteDB(ctx, dbPath)
		if reopenErr == nil {
			m.mu.Lock()
			m.db = reopened
			m.schemaInstalled = true
			m.features = features
			m.mu.Unlock()
		}
		return err
	}

	if haveBackup {
		_ = os.Remove(backupPath)
	}

	newDB, err := openSQLiteDB(ctx, dbPath)
	if err != nil {
		if haveBackup {
			_ = os.Remove(dbPath)
			_ = os.Rename(backupPath, dbPath)
			newDB, _ = openSQLiteDB(ctx, dbPath)
		}
		if newDB != nil {
			m.mu.Lock()
			m.db = newDB
			m.schemaInstalled = true
			m.features = features
			m.mu.Unlock()
		}
		return err
	}

	m.mu.Lock()
	m.db = newDB
	m.schemaInstalled = true
	m.features = features
	m.mu.Unlock()
	return nil
}

func (m *SQLiteIndexManager) syncIntoDB(ctx context.Context, db *sql.DB, force bool) (SyncResult, error) {
	var out SyncResult
	if strings.TrimSpace(m.cfg.WorkspaceRoot) == "" {
		return out, errors.New("workspace root is empty")
	}

	documents, err := m.discoverIndexDocuments()
	if err != nil {
		return out, err
	}
	out.ScannedFiles = len(documents)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()

	existingHashes, err := selectFileHashes(ctx, tx)
	if err != nil {
		return out, err
	}

	discovered := make(map[string]struct{}, len(documents))
	now := time.Now().Unix()

	for _, doc := range documents {
		discovered[doc.Path] = struct{}{}
		hash := HashText(doc.Text)
		if !force {
			if prevHash, ok := existingHashes[doc.Path]; ok && prevHash == hash {
				out.SkippedFiles++
				continue
			}
		}

		if _, err := tx.ExecContext(ctx, `INSERT INTO files(path, source, hash, mtime, size, updated_at)
			VALUES(?, ?, ?, ?, ?, ?)
			ON CONFLICT(path) DO UPDATE SET
				source=excluded.source,
				hash=excluded.hash,
				mtime=excluded.mtime,
				size=excluded.size,
				updated_at=excluded.updated_at;`,
			doc.Path,
			doc.Source,
			hash,
			doc.MTimeUnix,
			doc.Size,
			now,
		); err != nil {
			return out, fmt.Errorf("upsert file row %q: %w", doc.Path, err)
		}

		if _, err := tx.ExecContext(ctx, `DELETE FROM chunks WHERE path = ?;`, doc.Path); err != nil {
			return out, fmt.Errorf("delete stale chunks for %q: %w", doc.Path, err)
		}

		chunks := chunkTextWithLines(doc.Text, m.cfg.ChunkTokens, m.cfg.ChunkOverlap)
		for _, chunk := range chunks {
			if _, err := tx.ExecContext(ctx, `INSERT INTO chunks(path, source, start_line, end_line, hash, model, text, embedding, updated_at)
				VALUES(?, ?, ?, ?, ?, ?, ?, NULL, ?);`,
				doc.Path,
				doc.Source,
				chunk.StartLine,
				chunk.EndLine,
				chunk.Hash,
				m.cfg.Model,
				chunk.Text,
				now,
			); err != nil {
				return out, fmt.Errorf("insert chunk for %q: %w", doc.Path, err)
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

type indexDocument struct {
	Path      string
	Source    string
	Text      string
	MTimeUnix int64
	Size      int64
}

func (m *SQLiteIndexManager) discoverIndexDocuments() ([]indexDocument, error) {
	documents := make([]indexDocument, 0)

	if m.sourceEnabled("memory") {
		memoryFiles, err := DiscoverMemoryFiles(m.cfg.WorkspaceRoot, m.cfg.ExtraPaths)
		if err != nil {
			return nil, err
		}
		for _, file := range memoryFiles {
			payload, err := os.ReadFile(file.AbsolutePath)
			if err != nil {
				return nil, fmt.Errorf("read memory file %q: %w", file.AbsolutePath, err)
			}
			info, err := os.Stat(file.AbsolutePath)
			if err != nil {
				return nil, fmt.Errorf("stat memory file %q: %w", file.AbsolutePath, err)
			}
			documents = append(documents, indexDocument{
				Path:      file.AbsolutePath,
				Source:    classifyMemorySource(file),
				Text:      string(payload),
				MTimeUnix: info.ModTime().Unix(),
				Size:      info.Size(),
			})
		}
	}

	if m.sourceEnabled("sessions") {
		sessionFiles, err := DiscoverSessionFiles(m.cfg.SessionsRoot)
		if err != nil {
			return nil, err
		}
		for _, file := range sessionFiles {
			normalized, err := ReadSessionTranscriptNormalized(file.AbsolutePath)
			if err != nil {
				return nil, fmt.Errorf("read session transcript %q: %w", file.AbsolutePath, err)
			}
			info, err := os.Stat(file.AbsolutePath)
			if err != nil {
				return nil, fmt.Errorf("stat session transcript %q: %w", file.AbsolutePath, err)
			}
			documents = append(documents, indexDocument{
				Path:      file.AbsolutePath,
				Source:    "sessions",
				Text:      normalized,
				MTimeUnix: info.ModTime().Unix(),
				Size:      info.Size(),
			})
		}
	}

	return documents, nil
}

func (m *SQLiteIndexManager) runAutoSync(ctx context.Context, done chan<- struct{}, triggerCh <-chan autoSyncTrigger, cfg AutoSyncConfig) {
	defer close(done)
	defer func() {
		if rec := recover(); rec != nil {
			m.recordBackgroundError(fmt.Errorf("autosync panic: %v", rec))
		}
	}()

	var watchTicker *time.Ticker
	var watchCh <-chan time.Time
	if cfg.Watch {
		watchTicker = time.NewTicker(cfg.WatchPollInterval)
		watchCh = watchTicker.C
		defer watchTicker.Stop()
	}

	var intervalTicker *time.Ticker
	var intervalCh <-chan time.Time
	if cfg.Interval > 0 {
		intervalTicker = time.NewTicker(cfg.Interval)
		intervalCh = intervalTicker.C
		defer intervalTicker.Stop()
	}

	watchDebounceDelay := cfg.WatchDebounce
	watchDebounceCh := make(<-chan time.Time)
	var watchDebounceTimer *time.Timer
	sessionDebounceDelay := cfg.SessionDebounce
	sessionDebounceCh := make(<-chan time.Time)
	var sessionDebounceTimer *time.Timer

	scheduleWatchDebouncedSync := func() {
		if watchDebounceTimer == nil {
			watchDebounceTimer = time.NewTimer(watchDebounceDelay)
			watchDebounceCh = watchDebounceTimer.C
			return
		}
		if !watchDebounceTimer.Stop() {
			select {
			case <-watchDebounceTimer.C:
			default:
			}
		}
		watchDebounceTimer.Reset(watchDebounceDelay)
	}

	scheduleSessionDebouncedSync := func() {
		if sessionDebounceTimer == nil {
			sessionDebounceTimer = time.NewTimer(sessionDebounceDelay)
			sessionDebounceCh = sessionDebounceTimer.C
			return
		}
		if !sessionDebounceTimer.Stop() {
			select {
			case <-sessionDebounceTimer.C:
			default:
			}
		}
		sessionDebounceTimer.Reset(sessionDebounceDelay)
	}

	runBackgroundSync := func() {
		if _, err := m.Sync(ctx, false); err != nil {
			m.recordBackgroundError(err)
		}
	}

	for {
		select {
		case <-ctx.Done():
			if watchDebounceTimer != nil {
				watchDebounceTimer.Stop()
			}
			if sessionDebounceTimer != nil {
				sessionDebounceTimer.Stop()
			}
			return
		case <-watchDebounceCh:
			watchDebounceCh = nil
			runBackgroundSync()
		case <-sessionDebounceCh:
			sessionDebounceCh = nil
			runBackgroundSync()
		case <-watchCh:
			changed, err := m.detectWatchChanges()
			if err != nil {
				m.recordBackgroundError(err)
				continue
			}
			if changed {
				m.setDirty(true)
				scheduleWatchDebouncedSync()
			}
		case trigger := <-triggerCh:
			switch trigger {
			case autoSyncTriggerSession:
				scheduleSessionDebouncedSync()
			}
		case <-intervalCh:
			m.setDirty(true)
			runBackgroundSync()
		}
	}
}

func (m *SQLiteIndexManager) detectWatchChanges() (bool, error) {
	next, err := m.scanWatchSnapshot()
	if err != nil {
		return false, err
	}

	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	if mapsEqual(m.watchSnapshot, next) {
		return false, nil
	}
	m.watchSnapshot = next
	return true, nil
}

func (m *SQLiteIndexManager) scanWatchSnapshot() (map[string]string, error) {
	files, err := DiscoverMemoryFiles(m.cfg.WorkspaceRoot, m.cfg.ExtraPaths)
	if err != nil {
		return nil, err
	}
	snapshot := make(map[string]string, len(files))
	for _, file := range files {
		info, err := os.Stat(file.AbsolutePath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		snapshot[file.AbsolutePath] = fmt.Sprintf("%d:%d", info.ModTime().UnixNano(), info.Size())
	}
	return snapshot, nil
}

func (m *SQLiteIndexManager) setDirty(v bool) {
	m.stateMu.Lock()
	m.dirty = v
	m.stateMu.Unlock()
}

func (m *SQLiteIndexManager) recordBackgroundError(err error) {
	if err == nil {
		return
	}
	m.stateMu.Lock()
	m.lastBackgroundErr = err
	m.stateMu.Unlock()
}

func (m *SQLiteIndexManager) sourceEnabled(name string) bool {
	target := strings.ToLower(strings.TrimSpace(name))
	if target == "" {
		return false
	}
	if len(m.cfg.Sources) == 0 {
		return target == "memory"
	}
	for _, source := range m.cfg.Sources {
		if strings.ToLower(strings.TrimSpace(source)) == target {
			return true
		}
	}
	return false
}

func mapsEqual(a map[string]string, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		if vb, ok := b[k]; !ok || vb != va {
			return false
		}
	}
	return true
}

func maxInt(v int, min int) int {
	if v < min {
		return min
	}
	return v
}

func openSQLiteDB(ctx context.Context, dbPath string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = ON;`); err != nil {
		db.Close()
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
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
