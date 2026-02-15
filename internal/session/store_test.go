package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestResolveSessionsPathAndTranscriptPath(t *testing.T) {
	t.Parallel()

	stateRoot := t.TempDir()
	store := NewStore(Settings{
		StateRoot: stateRoot,
		StorePath: "agents/{agentId}/sessions/sessions.json",
	})

	sessionsPath, err := store.ResolveSessionsPath("alpha")
	if err != nil {
		t.Fatalf("resolve sessions path: %v", err)
	}
	wantSessionsPath := filepath.Join(stateRoot, "agents", "alpha", "sessions", "sessions.json")
	if sessionsPath != wantSessionsPath {
		t.Fatalf("unexpected sessions path %q", sessionsPath)
	}

	transcriptPath, err := store.ResolveTranscriptPath("alpha", "chat/main")
	if err != nil {
		t.Fatalf("resolve transcript path: %v", err)
	}
	wantTranscriptPath := filepath.Join(stateRoot, "agents", "alpha", "sessions", "chat-main.jsonl")
	if transcriptPath != wantTranscriptPath {
		t.Fatalf("unexpected transcript path %q", transcriptPath)
	}
}

func TestUpdateConcurrentDoesNotCorruptSessionsFile(t *testing.T) {
	stateRoot := t.TempDir()
	store := NewStore(Settings{
		StateRoot:         stateRoot,
		StorePath:         "agents/{agentId}/sessions/sessions.json",
		LockTimeout:       2 * time.Second,
		LockStaleAfter:    30 * time.Second,
		LockRetryInterval: 2 * time.Millisecond,
		KnownAgentIDs:     []string{"alpha"},
	})

	if err := store.Init(context.Background()); err != nil {
		t.Fatalf("init store: %v", err)
	}

	const workers = 12
	const updatesPerWorker = 25
	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < updatesPerWorker; j++ {
				_, err := store.Update(context.Background(), "alpha", "sess-1", func(entry *SessionEntry) error {
					entry.Origin = OriginCLI
					entry.TotalTokens++
					return nil
				})
				if err != nil {
					t.Errorf("update failed: %v", err)
					return
				}
			}
		}()
	}

	wg.Wait()

	entry, ok, err := store.Get(context.Background(), "alpha", "sess-1")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if !ok {
		t.Fatalf("expected session entry to exist")
	}
	if entry.TotalTokens != workers*updatesPerWorker {
		t.Fatalf("unexpected total tokens: got %d want %d", entry.TotalTokens, workers*updatesPerWorker)
	}

	path, err := store.ResolveSessionsPath("alpha")
	if err != nil {
		t.Fatalf("resolve sessions path: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sessions file: %v", err)
	}
	var parsed sessionsFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("sessions file should always contain valid JSON: %v", err)
	}
}

func TestAcquireLockTimesOutDeterministically(t *testing.T) {
	stateRoot := t.TempDir()
	current := time.Unix(1700000000, 0)
	store := NewStore(Settings{
		StateRoot:         stateRoot,
		StorePath:         "agents/{agentId}/sessions/sessions.json",
		LockTimeout:       3 * time.Second,
		LockStaleAfter:    10 * time.Second,
		LockRetryInterval: time.Second,
		Now:               func() time.Time { return current },
		Sleep:             func(d time.Duration) { current = current.Add(d) },
	})

	path, err := store.ResolveSessionsPath("alpha")
	if err != nil {
		t.Fatalf("resolve path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir sessions dir: %v", err)
	}
	lockPath := lockFilePath(path)
	if err := os.WriteFile(lockPath, []byte("1700000000000000000"), 0o600); err != nil {
		t.Fatalf("write lock file: %v", err)
	}

	_, err = store.Update(context.Background(), "alpha", "sess-1", func(entry *SessionEntry) error { return nil })
	if !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("expected lock timeout, got %v", err)
	}
}

func TestAcquireLockRemovesStaleLockDeterministically(t *testing.T) {
	stateRoot := t.TempDir()
	current := time.Unix(1700000100, 0)
	store := NewStore(Settings{
		StateRoot:         stateRoot,
		StorePath:         "agents/{agentId}/sessions/sessions.json",
		LockTimeout:       2 * time.Second,
		LockStaleAfter:    30 * time.Second,
		LockRetryInterval: 10 * time.Millisecond,
		Now:               func() time.Time { return current },
		Sleep:             func(time.Duration) {},
	})

	path, err := store.ResolveSessionsPath("alpha")
	if err != nil {
		t.Fatalf("resolve path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir sessions dir: %v", err)
	}
	lockPath := lockFilePath(path)
	old := current.Add(-2 * time.Minute).UnixNano()
	if err := os.WriteFile(lockPath, []byte(time.Unix(0, old).UTC().Format(time.RFC3339Nano)), 0o600); err != nil {
		t.Fatalf("write lock file: %v", err)
	}

	_, err = store.Update(context.Background(), "alpha", "sess-2", func(entry *SessionEntry) error {
		entry.Origin = OriginSlack
		return nil
	})
	if err != nil {
		t.Fatalf("update should succeed after stale lock cleanup: %v", err)
	}
}

func TestStoreFileUsesHardenedPermsWhereSupported(t *testing.T) {
	t.Parallel()

	stateRoot := t.TempDir()
	store := NewStore(Settings{
		StateRoot:     stateRoot,
		StorePath:     "agents/{agentId}/sessions/sessions.json",
		KnownAgentIDs: []string{"alpha"},
	})

	if err := store.Init(context.Background()); err != nil {
		t.Fatalf("init store: %v", err)
	}

	path, err := store.ResolveSessionsPath("alpha")
	if err != nil {
		t.Fatalf("resolve path: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat sessions file: %v", err)
	}

	if runtime.GOOS != "windows" {
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("expected sessions file perms 0600, got %03o", got)
		}
	}
}

func TestResolveSessionsPathSanitizesAgentIDToStayWithinStateRoot(t *testing.T) {
	t.Parallel()

	stateRoot := t.TempDir()
	store := NewStore(Settings{
		StateRoot: stateRoot,
		StorePath: "agents/{agentId}/sessions/sessions.json",
	})

	path, err := store.ResolveSessionsPath("../../outside")
	if err != nil {
		t.Fatalf("resolve sessions path: %v", err)
	}

	rel, err := filepath.Rel(stateRoot, path)
	if err != nil {
		t.Fatalf("relative path: %v", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("resolved path escaped state root: %q", path)
	}
}

func TestUpdatePreservesRequiredMetadataFields(t *testing.T) {
	t.Parallel()

	stateRoot := t.TempDir()
	store := NewStore(Settings{
		StateRoot: stateRoot,
		StorePath: "agents/{agentId}/sessions/sessions.json",
	})

	entry, err := store.Update(context.Background(), "alpha", "sess-required", func(e *SessionEntry) error {
		e.ID = ""
		e.AgentID = ""
		e.CreatedAt = ""
		e.UpdatedAt = ""
		return nil
	})
	if err != nil {
		t.Fatalf("update session: %v", err)
	}

	if entry.ID != "sess-required" {
		t.Fatalf("expected stable id, got %q", entry.ID)
	}
	if entry.AgentID != "alpha" {
		t.Fatalf("expected stable agent id, got %q", entry.AgentID)
	}
	if entry.CreatedAt == "" {
		t.Fatalf("expected createdAt to remain populated")
	}
	if entry.UpdatedAt == "" {
		t.Fatalf("expected updatedAt to remain populated")
	}
}
