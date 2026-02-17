package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCreateBackupWritesTarGzWithNormalizedEntries(t *testing.T) {
	stateRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(stateRoot, "localclaw.json"), []byte(`{"ok":true}`), 0o600); err != nil {
		t.Fatalf("write localclaw.json: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(stateRoot, "cron"), 0o755); err != nil {
		t.Fatalf("mkdir cron: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateRoot, "cron", "jobs.json"), []byte(`[]`), 0o600); err != nil {
		t.Fatalf("write cron/jobs.json: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(stateRoot, "workspace"), 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateRoot, "workspace", "AGENTS.md"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("write workspace/AGENTS.md: %v", err)
	}

	manager, err := NewManager(Settings{
		StateRoot:   stateRoot,
		RetainCount: 3,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	archivePath, err := manager.CreateBackup(context.Background(), []Source{
		{Path: filepath.Join(stateRoot, "localclaw.json"), ArchivePath: "localclaw.json"},
		{Path: filepath.Join(stateRoot, "cron"), ArchivePath: "cron"},
		{Path: filepath.Join(stateRoot, "workspace"), ArchivePath: "workspace"},
	})
	if err != nil {
		t.Fatalf("create backup: %v", err)
	}

	if filepath.Dir(archivePath) != filepath.Join(stateRoot, "backups") {
		t.Fatalf("expected backup under %s, got %s", filepath.Join(stateRoot, "backups"), archivePath)
	}
	name := filepath.Base(archivePath)
	if !regexp.MustCompile(`^localclaw-backup-\d{8}-\d{6}Z\.tar\.gz$`).MatchString(name) {
		t.Fatalf("unexpected archive filename %q", name)
	}

	entries, err := readArchiveEntries(archivePath)
	if err != nil {
		t.Fatalf("read archive entries: %v", err)
	}
	for _, required := range []string{
		"localclaw.json",
		"cron/",
		"cron/jobs.json",
		"workspace/",
		"workspace/AGENTS.md",
	} {
		if _, ok := entries[required]; !ok {
			t.Fatalf("expected archive entry %q, got %v", required, keys(entries))
		}
	}
}

func TestCreateBackupSkipsMissingOptionalSources(t *testing.T) {
	stateRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(stateRoot, "localclaw.json"), []byte(`{"ok":true}`), 0o600); err != nil {
		t.Fatalf("write localclaw.json: %v", err)
	}

	manager, err := NewManager(Settings{
		StateRoot:   stateRoot,
		RetainCount: 3,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	archivePath, err := manager.CreateBackup(context.Background(), []Source{
		{Path: filepath.Join(stateRoot, "localclaw.json"), ArchivePath: "localclaw.json"},
		{Path: filepath.Join(stateRoot, "cron"), ArchivePath: "cron", Optional: true},
		{Path: filepath.Join(stateRoot, "workspace"), ArchivePath: "workspace", Optional: true},
	})
	if err != nil {
		t.Fatalf("create backup: %v", err)
	}
	entries, err := readArchiveEntries(archivePath)
	if err != nil {
		t.Fatalf("read archive entries: %v", err)
	}
	if _, ok := entries["localclaw.json"]; !ok {
		t.Fatalf("expected localclaw.json entry, got %v", keys(entries))
	}
	if _, ok := entries["cron/"]; ok {
		t.Fatalf("did not expect missing optional cron/ entry, got %v", keys(entries))
	}
}

func TestCleanupRetainsNewestArchivesByCount(t *testing.T) {
	stateRoot := t.TempDir()
	manager, err := NewManager(Settings{
		StateRoot:   stateRoot,
		RetainCount: 2,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	if err := os.MkdirAll(manager.BackupsDir(), 0o755); err != nil {
		t.Fatalf("mkdir backups: %v", err)
	}

	names := []string{
		"localclaw-backup-20260101-000000Z.tar.gz",
		"localclaw-backup-20260102-000000Z.tar.gz",
		"localclaw-backup-20260103-000000Z.tar.gz",
		"localclaw-backup-20260104-000000Z.tar.gz",
	}
	for _, name := range names {
		path := filepath.Join(manager.BackupsDir(), name)
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatalf("write backup file: %v", err)
		}
	}

	removed, err := manager.Cleanup(context.Background())
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if len(removed) != 2 {
		t.Fatalf("expected 2 removed backups, got %d", len(removed))
	}

	for _, shouldRemain := range []string{
		"localclaw-backup-20260103-000000Z.tar.gz",
		"localclaw-backup-20260104-000000Z.tar.gz",
	} {
		if _, err := os.Stat(filepath.Join(manager.BackupsDir(), shouldRemain)); err != nil {
			t.Fatalf("expected retained backup %s: %v", shouldRemain, err)
		}
	}
}

func TestCreateBackupAndCleanupDoNotOverlap(t *testing.T) {
	stateRoot := t.TempDir()
	manager, err := NewManager(Settings{
		StateRoot:   stateRoot,
		RetainCount: 3,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	manager.createArchiveFn = func(ctx context.Context, archivePath string, sources []Source) error {
		_ = ctx
		_ = archivePath
		_ = sources
		close(started)
		<-release
		return nil
	}

	errCh := make(chan error, 1)
	go func() {
		_, createErr := manager.CreateBackup(context.Background(), nil)
		errCh <- createErr
	}()

	<-started
	if _, cleanupErr := manager.Cleanup(context.Background()); !errors.Is(cleanupErr, ErrRunInProgress) {
		t.Fatalf("expected cleanup overlap to return ErrRunInProgress, got %v", cleanupErr)
	}
	close(release)
	if createErr := <-errCh; createErr != nil {
		t.Fatalf("expected blocked create backup to finish cleanly, got %v", createErr)
	}
}

func TestStartAutoSaveSkipsOverlappingTicks(t *testing.T) {
	stateRoot := t.TempDir()
	manager, err := NewManager(Settings{
		StateRoot:   stateRoot,
		RetainCount: 3,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	tickerStub := newStubTicker()
	manager.newTicker = func(d time.Duration) ticker {
		_ = d
		return tickerStub
	}

	var createCalls int32
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	manager.createArchiveFn = func(ctx context.Context, archivePath string, sources []Source) error {
		_ = ctx
		_ = archivePath
		_ = sources
		atomic.AddInt32(&createCalls, 1)
		startedOnce.Do(func() { close(started) })
		<-release
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.StartAutoSave(ctx, time.Second, func() ([]Source, error) {
		return []Source{}, nil
	})

	tickerStub.fire()
	<-started
	tickerStub.fire()
	time.Sleep(30 * time.Millisecond)

	if got := atomic.LoadInt32(&createCalls); got != 1 {
		t.Fatalf("expected overlapping tick to be skipped while run active, createCalls=%d", got)
	}

	close(release)
	cancel()
}

func readArchiveEntries(path string) (map[string]struct{}, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	entries := map[string]struct{}{}
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		entries[header.Name] = struct{}{}
	}
	return entries, nil
}

func keys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	return out
}

type stubTicker struct {
	c chan time.Time
}

func newStubTicker() *stubTicker {
	return &stubTicker{c: make(chan time.Time, 8)}
}

func (t *stubTicker) C() <-chan time.Time {
	return t.c
}

func (t *stubTicker) Stop() {}

func (t *stubTicker) fire() {
	t.c <- time.Now()
}
