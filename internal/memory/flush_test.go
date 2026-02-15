package memory

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dgriffin831/localclaw/internal/session"
)

func TestShouldRunMemoryFlushNearThreshold(t *testing.T) {
	t.Parallel()

	settings := FlushSettings{Enabled: true, CompactionThresholdTokens: 1000, TriggerWindowTokens: 100}
	entry := session.SessionEntry{TotalTokens: 950, CompactionCount: 2}

	if !ShouldRunMemoryFlush(settings, entry, true) {
		t.Fatalf("expected flush to run near threshold")
	}
}

func TestShouldRunMemoryFlushSkipsWhenBelowThreshold(t *testing.T) {
	t.Parallel()

	settings := FlushSettings{Enabled: true, CompactionThresholdTokens: 1000, TriggerWindowTokens: 100}
	entry := session.SessionEntry{TotalTokens: 899, CompactionCount: 2}

	if ShouldRunMemoryFlush(settings, entry, true) {
		t.Fatalf("expected flush to skip below threshold")
	}
}

func TestShouldRunMemoryFlushSkipsReadOnlyWorkspace(t *testing.T) {
	t.Parallel()

	settings := FlushSettings{Enabled: true, CompactionThresholdTokens: 1000, TriggerWindowTokens: 100}
	entry := session.SessionEntry{TotalTokens: 950, CompactionCount: 2}

	if ShouldRunMemoryFlush(settings, entry, false) {
		t.Fatalf("expected flush to skip for read-only workspace")
	}
}

func TestShouldRunMemoryFlushSkipsWhenAlreadyFlushedForCompactionCycle(t *testing.T) {
	t.Parallel()

	settings := FlushSettings{Enabled: true, CompactionThresholdTokens: 1000, TriggerWindowTokens: 100}
	entry := session.SessionEntry{
		TotalTokens:                990,
		CompactionCount:            3,
		MemoryFlushAt:              "2026-02-15T00:00:00Z",
		MemoryFlushCompactionCount: 3,
	}

	if ShouldRunMemoryFlush(settings, entry, true) {
		t.Fatalf("expected flush to skip when already flushed in current compaction cycle")
	}
}

func TestShouldRunMemoryFlushRunsAgainAfterCompactionCountIncrements(t *testing.T) {
	t.Parallel()

	settings := FlushSettings{Enabled: true, CompactionThresholdTokens: 1000, TriggerWindowTokens: 100}
	entry := session.SessionEntry{
		TotalTokens:                990,
		CompactionCount:            4,
		MemoryFlushAt:              "2026-02-15T00:00:00Z",
		MemoryFlushCompactionCount: 3,
	}

	if !ShouldRunMemoryFlush(settings, entry, true) {
		t.Fatalf("expected flush to run after compaction count increments")
	}
}

func TestMaybeRunMemoryFlushPersistsSessionMetadata(t *testing.T) {
	ctx := context.Background()
	stateRoot := t.TempDir()

	store := session.NewStore(session.Settings{StateRoot: stateRoot, StorePath: "agents/{agentId}/sessions/sessions.json"})
	if err := store.Init(ctx); err != nil {
		t.Fatalf("init session store: %v", err)
	}

	_, err := store.Update(ctx, "default", "main", func(entry *session.SessionEntry) error {
		entry.Key = "default/main"
		entry.TotalTokens = 950
		entry.CompactionCount = 2
		return nil
	})
	if err != nil {
		t.Fatalf("seed session entry: %v", err)
	}

	llm := &stubFlushPromptClient{}
	ran, err := MaybeRunMemoryFlush(ctx, FlushRunRequest{
		AgentID:           "default",
		SessionID:         "main",
		SessionKey:        "default/main",
		WorkspacePath:     t.TempDir(),
		WorkspaceWritable: true,
		Settings: FlushSettings{
			Enabled:                   true,
			CompactionThresholdTokens: 1000,
			TriggerWindowTokens:       100,
			Prompt:                    "summarize to memory",
			Timeout:                   time.Second,
		},
	}, store, llm)
	if err != nil {
		t.Fatalf("run flush: %v", err)
	}
	if !ran {
		t.Fatalf("expected flush to run")
	}
	if llm.callCount() != 1 {
		t.Fatalf("expected one silent flush prompt, got %d", llm.callCount())
	}
	if !strings.Contains(llm.lastInput(), "summarize to memory") {
		t.Fatalf("expected configured flush prompt to be used, got %q", llm.lastInput())
	}

	entry, ok, err := store.Get(ctx, "default", "main")
	if err != nil {
		t.Fatalf("get session entry: %v", err)
	}
	if !ok {
		t.Fatalf("expected session entry")
	}
	if entry.MemoryFlushAt == "" {
		t.Fatalf("expected memoryFlushAt to be persisted")
	}
	if entry.MemoryFlushCompactionCount != 2 {
		t.Fatalf("expected memoryFlushCompactionCount=2, got %d", entry.MemoryFlushCompactionCount)
	}
}

func TestMaybeRunMemoryFlushSkipsWithoutCallingLLMWhenReadOnly(t *testing.T) {
	ctx := context.Background()
	stateRoot := t.TempDir()

	store := session.NewStore(session.Settings{StateRoot: stateRoot, StorePath: "agents/{agentId}/sessions/sessions.json"})
	if err := store.Init(ctx); err != nil {
		t.Fatalf("init session store: %v", err)
	}

	_, err := store.Update(ctx, "default", "main", func(entry *session.SessionEntry) error {
		entry.TotalTokens = 950
		entry.CompactionCount = 2
		return nil
	})
	if err != nil {
		t.Fatalf("seed session entry: %v", err)
	}

	llm := &stubFlushPromptClient{}
	ran, err := MaybeRunMemoryFlush(ctx, FlushRunRequest{
		AgentID:           "default",
		SessionID:         "main",
		SessionKey:        "default/main",
		WorkspacePath:     t.TempDir(),
		WorkspaceWritable: false,
		Settings: FlushSettings{
			Enabled:                   true,
			CompactionThresholdTokens: 1000,
			TriggerWindowTokens:       100,
		},
	}, store, llm)
	if err != nil {
		t.Fatalf("run flush: %v", err)
	}
	if ran {
		t.Fatalf("expected flush to skip when workspace is read-only")
	}
	if llm.callCount() != 0 {
		t.Fatalf("expected no LLM call for skipped flush")
	}

	entry, ok, err := store.Get(ctx, "default", "main")
	if err != nil {
		t.Fatalf("get session entry: %v", err)
	}
	if !ok {
		t.Fatalf("expected session entry")
	}
	if entry.MemoryFlushAt != "" {
		t.Fatalf("expected memoryFlushAt to remain empty on skip")
	}
}

func TestMaybeRunMemoryFlushReturnsErrorWhenPromptFails(t *testing.T) {
	ctx := context.Background()
	stateRoot := t.TempDir()

	store := session.NewStore(session.Settings{StateRoot: stateRoot, StorePath: "agents/{agentId}/sessions/sessions.json"})
	if err := store.Init(ctx); err != nil {
		t.Fatalf("init session store: %v", err)
	}

	_, err := store.Update(ctx, "default", "main", func(entry *session.SessionEntry) error {
		entry.TotalTokens = 950
		entry.CompactionCount = 2
		return nil
	})
	if err != nil {
		t.Fatalf("seed session entry: %v", err)
	}

	llm := &stubFlushPromptClient{err: errors.New("boom")}
	ran, err := MaybeRunMemoryFlush(ctx, FlushRunRequest{
		AgentID:           "default",
		SessionID:         "main",
		SessionKey:        "default/main",
		WorkspacePath:     t.TempDir(),
		WorkspaceWritable: true,
		Settings: FlushSettings{
			Enabled:                   true,
			CompactionThresholdTokens: 1000,
			TriggerWindowTokens:       100,
		},
	}, store, llm)
	if err == nil {
		t.Fatalf("expected flush prompt error")
	}
	if ran {
		t.Fatalf("expected ran=false on prompt error")
	}

	entry, ok, getErr := store.Get(ctx, "default", "main")
	if getErr != nil {
		t.Fatalf("get session entry: %v", getErr)
	}
	if !ok {
		t.Fatalf("expected session entry")
	}
	if entry.MemoryFlushAt != "" {
		t.Fatalf("expected memoryFlushAt to remain empty when prompt fails")
	}
}

type stubFlushPromptClient struct {
	mu    sync.Mutex
	err   error
	calls int
	input string
}

func (s *stubFlushPromptClient) Prompt(ctx context.Context, input string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.input = input
	if s.err != nil {
		return "", s.err
	}
	return "ok", nil
}

func (s *stubFlushPromptClient) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *stubFlushPromptClient) lastInput() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.input
}
