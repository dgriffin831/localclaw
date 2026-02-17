package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	archiveTimestampFormat = "20060102-150405Z"
	archiveNamePrefix      = "localclaw-backup-"
	archiveNameSuffix      = ".tar.gz"
)

var (
	archiveNamePattern = regexp.MustCompile(`^localclaw-backup-(\d{8}-\d{6}Z)\.tar\.gz$`)
	dayIntervalPattern = regexp.MustCompile(`^([0-9]+)d$`)
)

var ErrRunInProgress = errors.New("backup run already active")

type Source struct {
	Path        string
	ArchivePath string
	Optional    bool
}

type Settings struct {
	StateRoot   string
	RetainCount int
}

type ticker interface {
	C() <-chan time.Time
	Stop()
}

type localTicker struct {
	t *time.Ticker
}

func (t *localTicker) C() <-chan time.Time {
	return t.t.C
}

func (t *localTicker) Stop() {
	t.t.Stop()
}

type Manager struct {
	stateRoot   string
	backupsDir  string
	retainCount int

	now       func() time.Time
	logf      func(format string, args ...interface{})
	newTicker func(interval time.Duration) ticker

	mu      sync.Mutex
	running bool

	createArchiveFn func(ctx context.Context, archivePath string, sources []Source) error
	cleanupFn       func(ctx context.Context) ([]string, error)
}

func NewManager(settings Settings) (*Manager, error) {
	if settings.RetainCount <= 0 {
		return nil, errors.New("retain count must be > 0")
	}
	stateRoot, err := resolveStateRoot(settings.StateRoot)
	if err != nil {
		return nil, err
	}

	manager := &Manager{
		stateRoot:   stateRoot,
		backupsDir:  filepath.Join(stateRoot, "backups"),
		retainCount: settings.RetainCount,
		now:         time.Now,
		logf:        log.Printf,
		newTicker: func(interval time.Duration) ticker {
			return &localTicker{t: time.NewTicker(interval)}
		},
	}
	manager.createArchiveFn = manager.createArchive
	manager.cleanupFn = manager.cleanupArchives
	return manager, nil
}

func (m *Manager) StateRoot() string {
	return m.stateRoot
}

func (m *Manager) BackupsDir() string {
	return m.backupsDir
}

func ParseInterval(raw string) (time.Duration, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, errors.New("backup.interval is required")
	}

	lower := strings.ToLower(value)
	if matches := dayIntervalPattern.FindStringSubmatch(lower); len(matches) == 2 {
		days, err := strconv.Atoi(matches[1])
		if err != nil || days <= 0 {
			return 0, errors.New("backup.interval must be > 0")
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid backup.interval %q: %w", value, err)
	}
	if parsed <= 0 {
		return 0, errors.New("backup.interval must be > 0")
	}
	return parsed, nil
}

func (m *Manager) CreateBackup(ctx context.Context, sources []Source) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !m.startRun() {
		return "", ErrRunInProgress
	}
	defer m.endRun()

	if err := os.MkdirAll(m.backupsDir, 0o755); err != nil {
		return "", fmt.Errorf("create backups directory: %w", err)
	}

	archivePath, err := m.nextArchivePath()
	if err != nil {
		return "", err
	}
	if err := m.createArchiveFn(ctx, archivePath, sources); err != nil {
		_ = os.Remove(archivePath)
		return "", err
	}
	return archivePath, nil
}

func (m *Manager) Cleanup(ctx context.Context) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !m.startRun() {
		return nil, ErrRunInProgress
	}
	defer m.endRun()
	return m.cleanupFn(ctx)
}

func (m *Manager) StartAutoSave(ctx context.Context, interval time.Duration, sourcesFn func() ([]Source, error)) {
	if interval <= 0 || sourcesFn == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	t := m.newTicker(interval)
	go func() {
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C():
				go m.runAutoSaveTick(ctx, sourcesFn)
			}
		}
	}()
}

func (m *Manager) StartAutoClean(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	t := m.newTicker(interval)
	go func() {
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C():
				go m.runAutoCleanTick(ctx)
			}
		}
	}()
}

func (m *Manager) runAutoSaveTick(ctx context.Context, sourcesFn func() ([]Source, error)) {
	sources, err := sourcesFn()
	if err != nil {
		m.log("backup auto-save failed to resolve sources: %v", err)
		return
	}
	path, err := m.CreateBackup(ctx, sources)
	if errors.Is(err, ErrRunInProgress) {
		m.log("backup auto-save skipped: previous backup run still active")
		return
	}
	if err != nil {
		m.log("backup auto-save failed: %v", err)
		return
	}
	m.log("backup auto-save created archive: %s", path)
}

func (m *Manager) runAutoCleanTick(ctx context.Context) {
	removed, err := m.Cleanup(ctx)
	if errors.Is(err, ErrRunInProgress) {
		m.log("backup auto-clean skipped: previous backup run still active")
		return
	}
	if err != nil {
		m.log("backup auto-clean failed: %v", err)
		return
	}
	if len(removed) > 0 {
		m.log("backup auto-clean removed %d archive(s)", len(removed))
	}
}

func (m *Manager) startRun() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		return false
	}
	m.running = true
	return true
}

func (m *Manager) endRun() {
	m.mu.Lock()
	m.running = false
	m.mu.Unlock()
}

func (m *Manager) log(format string, args ...interface{}) {
	if m.logf == nil {
		return
	}
	m.logf(format, args...)
}

func (m *Manager) nextArchivePath() (string, error) {
	base := m.now().UTC()
	for i := 0; i < 1000; i++ {
		candidateTime := base.Add(time.Duration(i) * time.Second)
		name := archiveNamePrefix + candidateTime.Format(archiveTimestampFormat) + archiveNameSuffix
		candidate := filepath.Join(m.backupsDir, name)
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		} else if err != nil {
			return "", fmt.Errorf("stat backup path: %w", err)
		}
	}
	return "", errors.New("failed to allocate unique backup archive name")
}

func (m *Manager) createArchive(ctx context.Context, archivePath string, sources []Source) (retErr error) {
	file, err := os.Create(archivePath)
	if err != nil {
		return fmt.Errorf("create archive file: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); retErr == nil && closeErr != nil {
			retErr = fmt.Errorf("close archive file: %w", closeErr)
		}
	}()

	gzipWriter := gzip.NewWriter(file)
	defer func() {
		if closeErr := gzipWriter.Close(); retErr == nil && closeErr != nil {
			retErr = fmt.Errorf("close gzip writer: %w", closeErr)
		}
	}()

	tarWriter := tar.NewWriter(gzipWriter)
	defer func() {
		if closeErr := tarWriter.Close(); retErr == nil && closeErr != nil {
			retErr = fmt.Errorf("close tar writer: %w", closeErr)
		}
	}()

	seenArchiveRoots := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return err
		}
		archiveRoot, err := normalizeArchivePath(source.ArchivePath)
		if err != nil {
			return fmt.Errorf("normalize archive path %q: %w", source.ArchivePath, err)
		}
		if _, exists := seenArchiveRoots[archiveRoot]; exists {
			continue
		}
		seenArchiveRoots[archiveRoot] = struct{}{}

		if err := m.addSource(tarWriter, ctx, source.Path, archiveRoot, source.Optional); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) addSource(tw *tar.Writer, ctx context.Context, sourcePath, archivePath string, optional bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	pathValue := filepath.Clean(strings.TrimSpace(sourcePath))
	if pathValue == "" || pathValue == "." {
		if optional {
			return nil
		}
		return errors.New("source path is required")
	}

	info, err := os.Lstat(pathValue)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && optional {
			return nil
		}
		return fmt.Errorf("stat source %q: %w", pathValue, err)
	}

	switch mode := info.Mode(); {
	case info.IsDir():
		return m.addDirectory(tw, ctx, pathValue, archivePath, info)
	case mode&os.ModeSymlink != 0:
		return m.addSymlink(tw, pathValue, archivePath, info)
	case mode.IsRegular():
		return m.addFile(tw, ctx, pathValue, archivePath, info)
	default:
		if optional {
			return nil
		}
		return fmt.Errorf("unsupported source type %q", pathValue)
	}
}

func (m *Manager) addDirectory(tw *tar.Writer, ctx context.Context, sourcePath, archivePath string, info os.FileInfo) error {
	if err := writeHeader(tw, info, ensureTrailingSlash(archivePath), ""); err != nil {
		return fmt.Errorf("write directory header %q: %w", archivePath, err)
	}

	entries, err := os.ReadDir(sourcePath)
	if err != nil {
		return fmt.Errorf("read directory %q: %w", sourcePath, err)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		childSource := filepath.Join(sourcePath, entry.Name())
		childArchive := path.Join(archivePath, entry.Name())
		childInfo, err := os.Lstat(childSource)
		if err != nil {
			return fmt.Errorf("stat source %q: %w", childSource, err)
		}
		switch mode := childInfo.Mode(); {
		case childInfo.IsDir():
			if err := m.addDirectory(tw, ctx, childSource, childArchive, childInfo); err != nil {
				return err
			}
		case mode&os.ModeSymlink != 0:
			if err := m.addSymlink(tw, childSource, childArchive, childInfo); err != nil {
				return err
			}
		case mode.IsRegular():
			if err := m.addFile(tw, ctx, childSource, childArchive, childInfo); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Manager) addFile(tw *tar.Writer, ctx context.Context, sourcePath, archivePath string, info os.FileInfo) error {
	if err := writeHeader(tw, info, archivePath, ""); err != nil {
		return fmt.Errorf("write file header %q: %w", archivePath, err)
	}

	file, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open file %q: %w", sourcePath, err)
	}
	defer file.Close()

	if err := copyWithContext(ctx, tw, file); err != nil {
		return fmt.Errorf("copy file %q: %w", sourcePath, err)
	}
	return nil
}

func (m *Manager) addSymlink(tw *tar.Writer, sourcePath, archivePath string, info os.FileInfo) error {
	target, err := os.Readlink(sourcePath)
	if err != nil {
		return fmt.Errorf("read symlink target %q: %w", sourcePath, err)
	}
	if err := writeHeader(tw, info, archivePath, target); err != nil {
		return fmt.Errorf("write symlink header %q: %w", archivePath, err)
	}
	return nil
}

func copyWithContext(ctx context.Context, dst io.Writer, src io.Reader) error {
	buf := make([]byte, 32*1024)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := src.Read(buf)
		if n > 0 {
			if _, err := dst.Write(buf[:n]); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func writeHeader(tw *tar.Writer, info os.FileInfo, archivePath string, symlinkTarget string) error {
	header, err := tar.FileInfoHeader(info, symlinkTarget)
	if err != nil {
		return err
	}
	header.Name = filepath.ToSlash(archivePath)
	return tw.WriteHeader(header)
}

func (m *Manager) cleanupArchives(ctx context.Context) ([]string, error) {
	if _, err := os.Stat(m.backupsDir); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("stat backups directory: %w", err)
	}

	entries, err := os.ReadDir(m.backupsDir)
	if err != nil {
		return nil, fmt.Errorf("read backups directory: %w", err)
	}

	type archiveFile struct {
		Name      string
		Path      string
		Timestamp time.Time
	}
	files := make([]archiveFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		timestamp, ok := parseArchiveTimestamp(entry.Name())
		if !ok {
			continue
		}
		files = append(files, archiveFile{
			Name:      entry.Name(),
			Path:      filepath.Join(m.backupsDir, entry.Name()),
			Timestamp: timestamp,
		})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].Timestamp.Equal(files[j].Timestamp) {
			return files[i].Name > files[j].Name
		}
		return files[i].Timestamp.After(files[j].Timestamp)
	})

	if len(files) <= m.retainCount {
		return nil, nil
	}

	removed := make([]string, 0, len(files)-m.retainCount)
	for _, file := range files[m.retainCount:] {
		if err := ctx.Err(); err != nil {
			return removed, err
		}
		if err := os.Remove(file.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return removed, fmt.Errorf("remove backup archive %q: %w", file.Path, err)
		}
		removed = append(removed, file.Path)
	}
	return removed, nil
}

func parseArchiveTimestamp(name string) (time.Time, bool) {
	matches := archiveNamePattern.FindStringSubmatch(name)
	if len(matches) != 2 {
		return time.Time{}, false
	}
	timestamp, err := time.Parse(archiveTimestampFormat, matches[1])
	if err != nil {
		return time.Time{}, false
	}
	return timestamp.UTC(), true
}

func normalizeArchivePath(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", errors.New("archive path is required")
	}
	normalized := strings.ReplaceAll(trimmed, "\\", "/")
	cleaned := path.Clean("/" + normalized)
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "" || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("archive path %q is invalid", value)
	}
	return cleaned, nil
}

func ensureTrailingSlash(value string) string {
	if strings.HasSuffix(value, "/") {
		return value
	}
	return value + "/"
}

func resolveStateRoot(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("state root is required")
	}
	if strings.HasPrefix(trimmed, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		trimmed = filepath.Join(home, strings.TrimPrefix(trimmed, "~"))
	}
	if filepath.IsAbs(trimmed) {
		return filepath.Clean(trimmed), nil
	}
	absolute, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("resolve state root absolute path: %w", err)
	}
	return filepath.Clean(absolute), nil
}
