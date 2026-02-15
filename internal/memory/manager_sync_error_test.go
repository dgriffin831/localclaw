package memory

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteIndexManagerWatchSyncFailureCapturedWithoutPanic(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")

	mustWriteMemoryFile(t, filepath.Join(workspace, "MEMORY.md"), "alpha")

	m := NewSQLiteIndexManager(IndexManagerConfig{
		DBPath:        dbPath,
		WorkspaceRoot: workspace,
		ChunkTokens:   2,
		ChunkOverlap:  1,
		Provider:      "none",
	})
	if err := m.Open(ctx); err != nil {
		t.Fatalf("open manager: %v", err)
	}
	defer m.Close()

	if _, err := m.Sync(ctx, true); err != nil {
		t.Fatalf("initial sync: %v", err)
	}

	expectedErr := errors.New("simulated watch-triggered sync failure")
	m.testHookSync = func(context.Context, bool) error {
		return expectedErr
	}
	t.Cleanup(func() { m.testHookSync = nil })

	if err := m.StartAutoSync(ctx, AutoSyncConfig{Watch: true, WatchDebounce: 40 * time.Millisecond, WatchPollInterval: 20 * time.Millisecond}); err != nil {
		t.Fatalf("start auto sync: %v", err)
	}
	defer m.StopAutoSync()

	mustWriteMemoryFile(t, filepath.Join(workspace, "MEMORY.md"), "alpha\nbeta")

	waitForCondition(t, 3*time.Second, func() bool {
		err := m.LastBackgroundError()
		return err != nil && err.Error() == expectedErr.Error()
	}, "background sync error should be captured")

	if err := m.StopAutoSync(); err != nil {
		t.Fatalf("stop auto sync after failure: %v", err)
	}
}
