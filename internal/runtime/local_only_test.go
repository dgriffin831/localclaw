package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/llm/claudecode"
	"github.com/dgriffin831/localclaw/internal/workspace"
)

type fakeLLMClient struct {
	promptResponse string
	promptErr      error
}

func (f fakeLLMClient) Prompt(ctx context.Context, input string) (string, error) {
	if f.promptErr != nil {
		return "", f.promptErr
	}
	return f.promptResponse, nil
}

func (f fakeLLMClient) PromptStream(ctx context.Context, input string) (<-chan claudecode.StreamEvent, <-chan error) {
	events := make(chan claudecode.StreamEvent)
	errs := make(chan error)
	close(events)
	close(errs)
	return events, errs
}

type failingWorkspaceManager struct{}

func (f failingWorkspaceManager) Init(ctx context.Context) error { return nil }
func (f failingWorkspaceManager) ResolveWorkspace(agentID string) (string, error) {
	return "", fmt.Errorf("forced workspace resolve failure")
}
func (f failingWorkspaceManager) EnsureWorkspace(ctx context.Context, agentID string, ensureBootstrap bool) (workspace.WorkspaceInfo, error) {
	return workspace.WorkspaceInfo{}, nil
}
func (f failingWorkspaceManager) LoadBootstrapFiles(ctx context.Context, agentID, sessionKey string) ([]workspace.BootstrapFile, error) {
	return nil, nil
}
func (f failingWorkspaceManager) Root() string { return "" }

func TestNewFailsWhenNetworkServerEnabled(t *testing.T) {
	cfg := config.Default()
	cfg.Security.EnableHTTPServer = true

	if _, err := New(cfg); err == nil {
		t.Fatalf("expected startup failure when HTTP server is enabled")
	}
}

func TestResolveSessionDefaults(t *testing.T) {
	resolution := ResolveSession("  ", "")
	if resolution.AgentID != DefaultAgentID {
		t.Fatalf("expected default agent id %q, got %q", DefaultAgentID, resolution.AgentID)
	}
	if resolution.SessionID != DefaultSessionID {
		t.Fatalf("expected default session id %q, got %q", DefaultSessionID, resolution.SessionID)
	}
	if resolution.SessionKey != "default/main" {
		t.Fatalf("expected default session key, got %q", resolution.SessionKey)
	}
}

func TestAppResolvesWorkspaceAndSessionPaths(t *testing.T) {
	stateRoot := t.TempDir()
	cfg := config.Default()
	cfg.State.Root = stateRoot
	cfg.Agents.Defaults.Workspace = "."
	cfg.Workspace.Root = "."
	cfg.Session.Store = "agents/{agentId}/sessions/sessions.json"

	app, err := New(cfg)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("run app: %v", err)
	}

	workspacePath, err := app.ResolveWorkspacePath("")
	if err != nil {
		t.Fatalf("resolve workspace path: %v", err)
	}
	wantWorkspacePath := filepath.Join(stateRoot, "workspace")
	if workspacePath != wantWorkspacePath {
		t.Fatalf("unexpected workspace path %q", workspacePath)
	}

	sessionsPath, err := app.ResolveSessionsPath("")
	if err != nil {
		t.Fatalf("resolve sessions path: %v", err)
	}
	wantSessionsPath := filepath.Join(stateRoot, "agents", "default", "sessions", "sessions.json")
	if sessionsPath != wantSessionsPath {
		t.Fatalf("unexpected sessions path %q", sessionsPath)
	}

	transcriptPath, err := app.ResolveTranscriptPath("", "")
	if err != nil {
		t.Fatalf("resolve transcript path: %v", err)
	}
	wantTranscriptPath := filepath.Join(stateRoot, "agents", "default", "sessions", "main.jsonl")
	if transcriptPath != wantTranscriptPath {
		t.Fatalf("unexpected transcript path %q", transcriptPath)
	}
}

func TestResolveMemoryFlushConfigMergesAgentOverride(t *testing.T) {
	cfg := config.Default()
	cfg.Agents.Defaults.Compaction.MemoryFlush.Enabled = true
	cfg.Agents.Defaults.Compaction.MemoryFlush.ThresholdTokens = 28000
	cfg.Agents.Defaults.Compaction.MemoryFlush.TriggerWindowTokens = 4000
	cfg.Agents.Defaults.Compaction.MemoryFlush.Prompt = "default prompt"
	cfg.Agents.Defaults.Compaction.MemoryFlush.TimeoutSeconds = 20
	cfg.Agents.List = []config.AgentConfig{
		{
			ID: "writer",
			Compaction: config.CompactionConfig{
				MemoryFlush: config.MemoryFlushConfig{
					Prompt: "agent prompt",
				},
			},
		},
	}

	app, err := New(cfg)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	resolved := app.resolveMemoryFlushConfig("writer")
	if !resolved.Enabled {
		t.Fatalf("expected enabled to inherit from defaults")
	}
	if resolved.ThresholdTokens != 28000 {
		t.Fatalf("expected thresholdTokens=28000, got %d", resolved.ThresholdTokens)
	}
	if resolved.TriggerWindowTokens != 4000 {
		t.Fatalf("expected triggerWindowTokens=4000, got %d", resolved.TriggerWindowTokens)
	}
	if resolved.TimeoutSeconds != 20 {
		t.Fatalf("expected timeoutSeconds=20, got %d", resolved.TimeoutSeconds)
	}
	if resolved.Prompt != "agent prompt" {
		t.Fatalf("expected prompt override to be applied, got %q", resolved.Prompt)
	}
}

func TestResolveMemoryFlushConfigWithoutAgentOverrideUsesDefaults(t *testing.T) {
	cfg := config.Default()
	cfg.Agents.Defaults.Compaction.MemoryFlush.Enabled = true
	cfg.Agents.Defaults.Compaction.MemoryFlush.ThresholdTokens = 28000
	cfg.Agents.Defaults.Compaction.MemoryFlush.TriggerWindowTokens = 4000
	cfg.Agents.Defaults.Compaction.MemoryFlush.Prompt = "default prompt"
	cfg.Agents.Defaults.Compaction.MemoryFlush.TimeoutSeconds = 20
	cfg.Agents.List = []config.AgentConfig{
		{
			ID: "writer",
		},
	}

	app, err := New(cfg)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	resolved := app.resolveMemoryFlushConfig("writer")
	if !resolved.Enabled {
		t.Fatalf("expected enabled to match defaults")
	}
	if resolved.ThresholdTokens != 28000 {
		t.Fatalf("expected thresholdTokens=28000, got %d", resolved.ThresholdTokens)
	}
	if resolved.TriggerWindowTokens != 4000 {
		t.Fatalf("expected triggerWindowTokens=4000, got %d", resolved.TriggerWindowTokens)
	}
	if resolved.TimeoutSeconds != 20 {
		t.Fatalf("expected timeoutSeconds=20, got %d", resolved.TimeoutSeconds)
	}
	if resolved.Prompt != "default prompt" {
		t.Fatalf("expected default prompt to be preserved, got %q", resolved.Prompt)
	}
}

func TestResetSessionCreatesSessionMemorySnapshot(t *testing.T) {
	ctx := context.Background()
	stateRoot := t.TempDir()
	cfg := config.Default()
	cfg.State.Root = stateRoot
	cfg.Agents.Defaults.Workspace = "."
	cfg.Workspace.Root = "."
	cfg.Session.Store = "agents/{agentId}/sessions/sessions.json"

	app, err := New(cfg)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	if err := app.Run(ctx); err != nil {
		t.Fatalf("run app: %v", err)
	}

	app.llm = fakeLLMClient{promptErr: fmt.Errorf("llm unavailable")}
	app.now = func() time.Time {
		return time.Date(2026, 2, 15, 21, 4, 5, 0, time.UTC)
	}

	if err := app.AppendSessionTranscriptMessage(ctx, "", "", "user", "Capture this reset context"); err != nil {
		t.Fatalf("append user transcript: %v", err)
	}
	if err := app.AppendSessionTranscriptMessage(ctx, "", "", "assistant", "Snapshot should include this turn"); err != nil {
		t.Fatalf("append assistant transcript: %v", err)
	}

	resolution := app.ResetSession(ctx, ResetSessionRequest{
		AgentID:   "",
		SessionID: "",
		Source:    "/reset",
		StartNew:  false,
	})
	if resolution.SessionID != DefaultSessionID {
		t.Fatalf("expected reset to keep current session id %q, got %q", DefaultSessionID, resolution.SessionID)
	}

	workspacePath, err := app.ResolveWorkspacePath("")
	if err != nil {
		t.Fatalf("resolve workspace path: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(workspacePath, "memory", "2026-02-15-*.md"))
	if err != nil {
		t.Fatalf("glob snapshots: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one snapshot file, got %d (%v)", len(matches), matches)
	}

	content, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	body := string(content)
	if !strings.Contains(body, "source: /reset") {
		t.Fatalf("expected source metadata in snapshot")
	}
	if !strings.Contains(body, "Capture this reset context") {
		t.Fatalf("expected transcript text in snapshot")
	}
}

func TestResetSessionHookFailureIsNonFatal(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.State.Root = t.TempDir()
	cfg.Agents.Defaults.Workspace = "."
	cfg.Workspace.Root = "."
	cfg.Session.Store = "agents/{agentId}/sessions/sessions.json"

	app, err := New(cfg)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	if err := app.Run(ctx); err != nil {
		t.Fatalf("run app: %v", err)
	}

	app.workspace = failingWorkspaceManager{}
	app.now = func() time.Time {
		return time.Date(2026, 2, 15, 22, 0, 0, 0, time.UTC)
	}

	next := app.ResetSession(ctx, ResetSessionRequest{
		Source:   "/new",
		StartNew: true,
	})
	if next.SessionID == DefaultSessionID {
		t.Fatalf("expected /new reset to rotate session id")
	}
	if !strings.HasPrefix(next.SessionID, "s-20260215-220000") {
		t.Fatalf("unexpected next session id %q", next.SessionID)
	}
}

func TestResetSessionStartNewAvoidsCurrentSessionIDCollision(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.State.Root = t.TempDir()
	cfg.Agents.Defaults.Workspace = "."
	cfg.Workspace.Root = "."
	cfg.Session.Store = "agents/{agentId}/sessions/sessions.json"

	app, err := New(cfg)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	if err := app.Run(ctx); err != nil {
		t.Fatalf("run app: %v", err)
	}

	app.now = func() time.Time {
		return time.Date(2026, 2, 15, 23, 59, 59, 0, time.UTC)
	}

	currentSessionID := "s-20260215-235959"
	next := app.ResetSession(ctx, ResetSessionRequest{
		SessionID: currentSessionID,
		StartNew:  true,
	})

	if next.SessionID == currentSessionID {
		t.Fatalf("expected a different session id when starting new session, got %q", next.SessionID)
	}
	if !strings.HasPrefix(next.SessionID, "s-20260215-235959") {
		t.Fatalf("unexpected next session id %q", next.SessionID)
	}
}

func TestResetSessionStartNewAvoidsExistingTranscriptCollision(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.State.Root = t.TempDir()
	cfg.Agents.Defaults.Workspace = "."
	cfg.Workspace.Root = "."
	cfg.Session.Store = "agents/{agentId}/sessions/sessions.json"

	app, err := New(cfg)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	if err := app.Run(ctx); err != nil {
		t.Fatalf("run app: %v", err)
	}

	app.now = func() time.Time {
		return time.Date(2026, 2, 16, 0, 0, 0, 0, time.UTC)
	}

	if err := app.AppendSessionTranscriptMessage(ctx, "", "s-20260216-000000", "user", "existing session transcript"); err != nil {
		t.Fatalf("append transcript: %v", err)
	}

	next := app.ResetSession(ctx, ResetSessionRequest{
		SessionID: "main",
		StartNew:  true,
	})
	if next.SessionID == "s-20260216-000000" {
		t.Fatalf("expected reset to avoid colliding with existing transcript-backed session id")
	}
	if !strings.HasPrefix(next.SessionID, "s-20260216-000000") {
		t.Fatalf("unexpected next session id %q", next.SessionID)
	}
}
