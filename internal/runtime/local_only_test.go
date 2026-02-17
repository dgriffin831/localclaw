package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/llm"
	"github.com/dgriffin831/localclaw/internal/session"
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

func (f fakeLLMClient) PromptStream(ctx context.Context, input string) (<-chan llm.StreamEvent, <-chan error) {
	events := make(chan llm.StreamEvent)
	errs := make(chan error)
	close(events)
	close(errs)
	return events, errs
}

func (f fakeLLMClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{}
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

func TestNewFailsWhenClaudeMCPWiringInvalid(t *testing.T) {
	stateRootFile := filepath.Join(t.TempDir(), "state-root-file")
	if err := os.WriteFile(stateRootFile, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write state root file: %v", err)
	}

	cfg := config.Default()
	cfg.App.Root = stateRootFile

	if _, err := New(cfg); err == nil {
		t.Fatalf("expected startup failure when claude mcp wiring is invalid")
	} else if !strings.Contains(err.Error(), "invalid claude mcp wiring") {
		t.Fatalf("expected claude mcp wiring error, got %v", err)
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
	cfg.App.Root = stateRoot
	cfg.Agents.Defaults.Workspace = "."
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

func TestRunBootstrapsDefaultConfigFileWhenMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := config.Default()
	cfg.Agents.Defaults.Workspace = "."
	cfg.Session.Store = "agents/{agentId}/sessions/sessions.json"
	cfg.Cron.Enabled = false
	cfg.Heartbeat.Enabled = false

	app, err := New(cfg)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("run app: %v", err)
	}

	configPath := filepath.Join(home, ".localclaw", "localclaw.json")
	payload, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read bootstrapped config: %v", err)
	}
	if strings.Contains(string(payload), "thinking_messages") {
		t.Fatalf("expected bootstrapped default config to omit app.thinking_messages, got %s", string(payload))
	}

	var decoded config.Config
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode bootstrapped config: %v", err)
	}
	if decoded.App.Name != cfg.App.Name {
		t.Fatalf("expected app.name=%q, got %q", cfg.App.Name, decoded.App.Name)
	}
	if decoded.LLM.Provider != cfg.LLM.Provider {
		t.Fatalf("expected llm.provider=%q, got %q", cfg.LLM.Provider, decoded.LLM.Provider)
	}
}

func TestRunDoesNotOverwriteExistingBootstrappedConfigFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := filepath.Join(home, ".localclaw", "localclaw.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	existing := []byte("{\"app\":{\"name\":\"custom\"}}\n")
	if err := os.WriteFile(configPath, existing, 0o600); err != nil {
		t.Fatalf("write existing config: %v", err)
	}

	cfg := config.Default()
	cfg.Agents.Defaults.Workspace = "."
	cfg.Session.Store = "agents/{agentId}/sessions/sessions.json"
	cfg.Cron.Enabled = false
	cfg.Heartbeat.Enabled = false

	app, err := New(cfg)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("run app: %v", err)
	}

	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read existing config after run: %v", err)
	}
	if string(got) != string(existing) {
		t.Fatalf("expected existing config to remain unchanged, got %q", string(got))
	}
}

func TestNewConfiguresClaudeMCPConfigPathUnderStateRoot(t *testing.T) {
	stateRoot := t.TempDir()
	argsPath := filepath.Join(t.TempDir(), "claude-args.txt")
	claudeScriptPath := filepath.Join(t.TempDir(), "claude")
	script := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
printf "%%s\n" "$@" > %q
printf '%%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"ok"}'
`, argsPath)
	if err := os.WriteFile(claudeScriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude script: %v", err)
	}

	cfg := config.Default()
	cfg.App.Root = stateRoot
	cfg.LLM.ClaudeCode.BinaryPath = claudeScriptPath

	app, err := New(cfg)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	requestClient, ok := app.llm.(llm.RequestClient)
	if !ok {
		t.Fatalf("expected request-capable llm client")
	}
	events, errs := requestClient.PromptStreamRequest(context.Background(), llm.Request{Input: "hello"})
	for events != nil || errs != nil {
		select {
		case _, ok := <-events:
			if !ok {
				events = nil
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err != nil {
				t.Fatalf("prompt stream request error: %v", err)
			}
		}
	}

	argsFile, err := os.Open(argsPath)
	if err != nil {
		t.Fatalf("open captured args: %v", err)
	}
	defer argsFile.Close()

	var args []string
	scanner := bufio.NewScanner(argsFile)
	for scanner.Scan() {
		args = append(args, strings.TrimSpace(scanner.Text()))
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan captured args: %v", err)
	}

	var mcpConfigPath string
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--mcp-config" {
			mcpConfigPath = args[i+1]
			break
		}
	}
	if mcpConfigPath == "" {
		t.Fatalf("expected --mcp-config flag in args: %v", args)
	}
	expectedPrefix := filepath.Join(stateRoot, "runtime", "mcp") + string(os.PathSeparator)
	if !strings.HasPrefix(mcpConfigPath, expectedPrefix) {
		t.Fatalf("expected mcp config path under state root, got %q (args=%v)", mcpConfigPath, args)
	}
}

func TestNewPassesClaudeExtraArgs(t *testing.T) {
	stateRoot := t.TempDir()
	argsPath := filepath.Join(t.TempDir(), "claude-args.txt")
	claudeScriptPath := filepath.Join(t.TempDir(), "claude")
	script := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
printf "%%s\n" "$@" > %q
printf '%%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"ok"}'
`, argsPath)
	if err := os.WriteFile(claudeScriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude script: %v", err)
	}

	cfg := config.Default()
	cfg.App.Root = stateRoot
	cfg.LLM.ClaudeCode.BinaryPath = claudeScriptPath
	cfg.LLM.ClaudeCode.ExtraArgs = []string{
		"--dangerously-skip-permissions",
		"--allowed-tools",
		"mcp__localclaw__localclaw_memory_search,mcp__localclaw__localclaw_memory_get",
	}

	app, err := New(cfg)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	requestClient, ok := app.llm.(llm.RequestClient)
	if !ok {
		t.Fatalf("expected request-capable llm client")
	}
	events, errs := requestClient.PromptStreamRequest(context.Background(), llm.Request{Input: "hello"})
	for events != nil || errs != nil {
		select {
		case _, ok := <-events:
			if !ok {
				events = nil
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err != nil {
				t.Fatalf("prompt stream request error: %v", err)
			}
		}
	}

	argsFile, err := os.Open(argsPath)
	if err != nil {
		t.Fatalf("open captured args: %v", err)
	}
	defer argsFile.Close()

	var args []string
	scanner := bufio.NewScanner(argsFile)
	for scanner.Scan() {
		args = append(args, strings.TrimSpace(scanner.Text()))
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan captured args: %v", err)
	}

	hasDangerousSkip := false
	hasAllowedTools := false
	for i, arg := range args {
		if arg == "--dangerously-skip-permissions" {
			hasDangerousSkip = true
		}
		if arg == "--allowed-tools" && i+1 < len(args) &&
			args[i+1] == "mcp__localclaw__localclaw_memory_search,mcp__localclaw__localclaw_memory_get" {
			hasAllowedTools = true
		}
	}
	if !hasDangerousSkip {
		t.Fatalf("expected --dangerously-skip-permissions in args: %v", args)
	}
	if !hasAllowedTools {
		t.Fatalf("expected configured --allowed-tools in args: %v", args)
	}
}

func TestNewDoesNotOverrideCodexHomeFromEnvironment(t *testing.T) {
	stateRoot := t.TempDir()
	tmpDir := t.TempDir()
	argsPath := filepath.Join(tmpDir, "codex-args.txt")
	envPath := filepath.Join(tmpDir, "codex-env.txt")
	codexScriptPath := filepath.Join(tmpDir, "codex")
	expectedHome := filepath.Join(tmpDir, "codex-home")
	t.Setenv("CODEX_HOME", expectedHome)
	script := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
printf "%%s\n" "$@" > %q
env | grep '^CODEX_HOME=' > %q || true
cat >/dev/null
printf '%%s\n' '{"type":"item.completed","item":{"type":"agent_message","text":"ok"}}'
`, argsPath, envPath)
	if err := os.WriteFile(codexScriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex script: %v", err)
	}

	cfg := config.Default()
	cfg.App.Root = stateRoot
	cfg.LLM.Provider = "codex"
	cfg.LLM.Codex.BinaryPath = codexScriptPath

	app, err := New(cfg)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	requestClient, ok := app.llm.(llm.RequestClient)
	if !ok {
		t.Fatalf("expected request-capable llm client")
	}
	events, errs := requestClient.PromptStreamRequest(context.Background(), llm.Request{Input: "hello"})
	for events != nil || errs != nil {
		select {
		case _, ok := <-events:
			if !ok {
				events = nil
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err != nil {
				t.Fatalf("prompt stream request error: %v", err)
			}
		}
	}

	envPayload, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read captured env: %v", err)
	}
	if !strings.Contains(string(envPayload), "CODEX_HOME="+expectedHome) {
		t.Fatalf("expected CODEX_HOME pass-through from environment, got %q", strings.TrimSpace(string(envPayload)))
	}
}

func TestPromptForSessionIncludesDefaultAllowedToolsForClaude(t *testing.T) {
	stateRoot := t.TempDir()
	argsPath := filepath.Join(t.TempDir(), "claude-args.txt")
	claudeScriptPath := filepath.Join(t.TempDir(), "claude")
	script := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
printf "%%s\n" "$@" > %q
printf '%%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"ok"}'
`, argsPath)
	if err := os.WriteFile(claudeScriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude script: %v", err)
	}

	cfg := config.Default()
	cfg.App.Root = stateRoot
	cfg.LLM.ClaudeCode.BinaryPath = claudeScriptPath
	cfg.Agents.Defaults.Memory.Enabled = true
	cfg.Agents.Defaults.Memory.Tools.Get = true
	cfg.Agents.Defaults.Memory.Tools.Search = true
	cfg.Agents.Defaults.Memory.Tools.Grep = true
	cfg.Agents.Defaults.Memory.Store.Path = filepath.Join("memory", "{agentId}.sqlite")

	app, err := New(cfg)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	if _, err := app.PromptForSession(context.Background(), "", "", "hello"); err != nil {
		t.Fatalf("prompt for session: %v", err)
	}

	argsFile, err := os.Open(argsPath)
	if err != nil {
		t.Fatalf("open captured args: %v", err)
	}
	defer argsFile.Close()

	var args []string
	scanner := bufio.NewScanner(argsFile)
	for scanner.Scan() {
		args = append(args, strings.TrimSpace(scanner.Text()))
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan captured args: %v", err)
	}

	var allowedTools string
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--allowed-tools" {
			allowedTools = args[i+1]
			break
		}
	}
	if allowedTools == "" {
		t.Fatalf("expected --allowed-tools flag in args: %v", args)
	}
	if allowedTools != cfg.LLM.ClaudeCode.ExtraArgs[1] {
		t.Fatalf("expected default --allowed-tools value %q, got %q", cfg.LLM.ClaudeCode.ExtraArgs[1], allowedTools)
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
	cfg.App.Root = stateRoot
	cfg.Agents.Defaults.Workspace = "."
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
	cfg.App.Root = t.TempDir()
	cfg.Agents.Defaults.Workspace = "."
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
	cfg.App.Root = t.TempDir()
	cfg.Agents.Defaults.Workspace = "."
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
	cfg.App.Root = t.TempDir()
	cfg.Agents.Defaults.Workspace = "."
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

func TestResetSessionSameSessionClearsProviderSessionContinuation(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.App.Root = t.TempDir()
	cfg.Agents.Defaults.Workspace = "."
	cfg.Session.Store = "agents/{agentId}/sessions/sessions.json"
	cfg.Cron.Enabled = false
	cfg.Heartbeat.Enabled = false

	app, err := New(cfg)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	if err := app.Run(ctx); err != nil {
		t.Fatalf("run app: %v", err)
	}

	_, err = app.sessions.Update(ctx, "default", "main", func(entry *session.SessionEntry) error {
		session.SetProviderSessionID(entry, "claudecode", "claude-session")
		session.SetProviderSessionID(entry, "codex", "codex-thread")
		return nil
	})
	if err != nil {
		t.Fatalf("seed provider session ids: %v", err)
	}

	current := app.ResetSession(ctx, ResetSessionRequest{
		AgentID:   "default",
		SessionID: "main",
		Source:    "test",
		StartNew:  false,
	})
	if current.SessionID != "main" {
		t.Fatalf("expected same session id after reset, got %q", current.SessionID)
	}

	entry, exists, err := app.sessions.Get(ctx, "default", "main")
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if !exists {
		t.Fatalf("expected session entry to exist")
	}
	if got := session.GetProviderSessionID(entry, "claudecode"); got != "" {
		t.Fatalf("expected claudecode provider session cleared, got %q", got)
	}
	if got := session.GetProviderSessionID(entry, "codex"); got != "" {
		t.Fatalf("expected codex provider session cleared, got %q", got)
	}
}
