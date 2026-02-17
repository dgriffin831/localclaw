package memory

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dgriffin831/localclaw/internal/session"
)

func TestSessionDeltaThresholdDebouncesSyncTrigger(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	sessionsRoot := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")
	transcript := filepath.Join(sessionsRoot, "main.jsonl")

	m := NewSQLiteIndexManager(IndexManagerConfig{
		DBPath:               dbPath,
		WorkspaceRoot:        workspace,
		Sources:              []string{"sessions"},
		SessionsRoot:         sessionsRoot,
		ChunkTokens:          32,
		ChunkOverlap:         0,
		SessionDeltaBytes:    1024,
		SessionDeltaMessages: 3,
	})
	if err := m.Open(ctx); err != nil {
		t.Fatalf("open manager: %v", err)
	}
	defer m.Close()

	if _, err := m.Sync(ctx, true); err != nil {
		t.Fatalf("initial sync: %v", err)
	}

	var (
		mu       sync.Mutex
		syncRuns int
	)
	m.testHookSync = func(context.Context, bool) error {
		mu.Lock()
		syncRuns++
		mu.Unlock()
		return nil
	}
	t.Cleanup(func() { m.testHookSync = nil })

	if err := m.StartAutoSync(ctx, AutoSyncConfig{SessionDebounce: 40 * time.Millisecond}); err != nil {
		t.Fatalf("start auto sync: %v", err)
	}
	defer m.StopAutoSync()

	bus := session.NewTranscriptEventBus()
	bus.Subscribe(m)
	writer := session.NewTranscriptWriter(session.TranscriptWriterSettings{Events: bus})

	for i := 0; i < 2; i++ {
		if err := writer.AppendMessage(ctx, transcript, session.TranscriptMessage{Role: "user", Content: "small update"}); err != nil {
			t.Fatalf("append message %d: %v", i, err)
		}
	}

	time.Sleep(120 * time.Millisecond)
	mu.Lock()
	before := syncRuns
	mu.Unlock()
	if before != 0 {
		t.Fatalf("expected no sync before threshold, got %d", before)
	}

	if err := writer.AppendMessage(ctx, transcript, session.TranscriptMessage{Role: "assistant", Content: "threshold crossed"}); err != nil {
		t.Fatalf("append threshold message: %v", err)
	}

	waitForCondition(t, 3*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return syncRuns >= 1
	}, "session delta trigger should schedule sync")
}

func TestSessionDeltaSyncFailureIsCaptured(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	sessionsRoot := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")
	transcript := filepath.Join(sessionsRoot, "main.jsonl")

	expectedErr := errors.New("simulated session delta sync failure")

	m := NewSQLiteIndexManager(IndexManagerConfig{
		DBPath:               dbPath,
		WorkspaceRoot:        workspace,
		Sources:              []string{"sessions"},
		SessionsRoot:         sessionsRoot,
		ChunkTokens:          32,
		ChunkOverlap:         0,
		SessionDeltaBytes:    1,
		SessionDeltaMessages: 1,
	})
	if err := m.Open(ctx); err != nil {
		t.Fatalf("open manager: %v", err)
	}
	defer m.Close()

	if _, err := m.Sync(ctx, true); err != nil {
		t.Fatalf("initial sync: %v", err)
	}

	m.testHookSync = func(context.Context, bool) error {
		return expectedErr
	}
	t.Cleanup(func() { m.testHookSync = nil })

	if err := m.StartAutoSync(ctx, AutoSyncConfig{SessionDebounce: 25 * time.Millisecond}); err != nil {
		t.Fatalf("start auto sync: %v", err)
	}
	defer m.StopAutoSync()

	bus := session.NewTranscriptEventBus()
	bus.Subscribe(m)
	writer := session.NewTranscriptWriter(session.TranscriptWriterSettings{Events: bus})

	if err := writer.AppendMessage(ctx, transcript, session.TranscriptMessage{Role: "user", Content: "trigger failure"}); err != nil {
		t.Fatalf("append message: %v", err)
	}

	waitForCondition(t, 3*time.Second, func() bool {
		err := m.LastBackgroundError()
		return err != nil && err.Error() == expectedErr.Error()
	}, "session delta sync error should be recorded")

	if err := m.StopAutoSync(); err != nil {
		t.Fatalf("stop auto sync: %v", err)
	}
}
