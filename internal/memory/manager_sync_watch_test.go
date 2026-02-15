package memory

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteIndexManagerWatchSyncMarksDirtyAndTriggersSync(t *testing.T) {
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

	if err := m.StartAutoSync(ctx, AutoSyncConfig{Watch: true, WatchDebounce: 50 * time.Millisecond, WatchPollInterval: 20 * time.Millisecond}); err != nil {
		t.Fatalf("start auto sync: %v", err)
	}
	defer m.StopAutoSync()

	mustWriteMemoryFile(t, filepath.Join(workspace, "MEMORY.md"), "alpha\nbeta\ngamma\ndelta\nepsilon")

	waitForCondition(t, 2*time.Second, func() bool {
		return m.Dirty()
	}, "watcher should mark manager dirty")

	waitForCondition(t, 3*time.Second, func() bool {
		status, err := m.Status(ctx)
		if err != nil {
			return false
		}
		return status.ChunkCount >= 2 && !m.Dirty()
	}, "watcher should trigger sync and clear dirty state")
}

func waitForCondition(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for condition: %s", msg)
}
