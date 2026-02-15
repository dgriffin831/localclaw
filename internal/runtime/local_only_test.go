package runtime

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dgriffin831/localclaw/internal/config"
)

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
