package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/llm"
	"github.com/dgriffin831/localclaw/internal/session"
	"github.com/dgriffin831/localclaw/internal/skills"
)

type captureLLMClient struct {
	lastPromptInput string
}

func (c *captureLLMClient) Prompt(ctx context.Context, input string) (string, error) {
	c.lastPromptInput = input
	return "ok", nil
}

func (c *captureLLMClient) PromptStream(ctx context.Context, input string) (<-chan llm.StreamEvent, <-chan error) {
	c.lastPromptInput = input
	events := make(chan llm.StreamEvent)
	errs := make(chan error)
	close(events)
	close(errs)
	return events, errs
}

func (c *captureLLMClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{}
}

type captureRequestLLMClient struct {
	lastRequest llm.Request
	requests    []llm.Request
	streamFn    func(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, <-chan error)
}

func (c *captureRequestLLMClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{SupportsRequestOptions: true}
}

func (c *captureRequestLLMClient) Prompt(ctx context.Context, input string) (string, error) {
	return "ok", nil
}

func (c *captureRequestLLMClient) PromptStream(ctx context.Context, input string) (<-chan llm.StreamEvent, <-chan error) {
	events := make(chan llm.StreamEvent)
	errs := make(chan error)
	close(events)
	close(errs)
	return events, errs
}

func (c *captureRequestLLMClient) PromptRequest(ctx context.Context, req llm.Request) (string, error) {
	c.lastRequest = req
	c.requests = append(c.requests, req)
	return "ok", nil
}

func (c *captureRequestLLMClient) PromptStreamRequest(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, <-chan error) {
	c.lastRequest = req
	c.requests = append(c.requests, req)
	if c.streamFn != nil {
		return c.streamFn(ctx, req)
	}
	events := make(chan llm.StreamEvent, 1)
	errs := make(chan error)
	events <- llm.StreamEvent{Type: llm.StreamEventFinal, Text: "ok"}
	close(events)
	close(errs)
	return events, errs
}

func TestToolDefinitionsIncludeMemoryToolsWhenEnabled(t *testing.T) {
	_, app, _ := newToolTestApp(t, true)

	tools := app.ToolDefinitions("")
	if len(tools) == 0 {
		t.Fatalf("expected runtime tools when memory is enabled")
	}

	toolNames := map[string]bool{}
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}
	if !toolNames[skills.ToolMemorySearch] {
		t.Fatalf("expected %s tool in registry", skills.ToolMemorySearch)
	}
	if !toolNames[skills.ToolMemoryGet] {
		t.Fatalf("expected %s tool in registry", skills.ToolMemoryGet)
	}
	if !toolNames[skills.ToolMemoryGrep] {
		t.Fatalf("expected %s tool in registry", skills.ToolMemoryGrep)
	}
}

func TestToolDefinitionsExcludeMemoryToolWhenDisabledByFlag(t *testing.T) {
	_, app, _ := newToolTestApp(t, true)
	app.cfg.Agents.Defaults.Memory.Tools.Search = false

	tools := app.ToolDefinitions("")
	toolNames := map[string]bool{}
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}
	if toolNames[skills.ToolMemorySearch] {
		t.Fatalf("expected memory_search tool to be hidden when agents.defaults.memory.tools.search=false")
	}
	if !toolNames[skills.ToolMemoryGet] {
		t.Fatalf("expected memory_get to remain enabled")
	}
	if !toolNames[skills.ToolMemoryGrep] {
		t.Fatalf("expected memory_grep to remain enabled")
	}
}

func TestPromptStreamForSessionUsesRequestPathAndSessionMetadata(t *testing.T) {
	ctx := context.Background()
	_, app, _ := newToolTestApp(t, true)
	llmClient := &captureRequestLLMClient{}
	app.llm = llmClient

	events, errs := app.PromptStreamForSession(ctx, "agent-2", "s-42", "hello")
	for range events {
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected stream error: %v", err)
		}
	}

	if llmClient.lastRequest.Session.SessionKey != "agent-2/s-42" {
		t.Fatalf("expected session_key in request metadata, got %q", llmClient.lastRequest.Session.SessionKey)
	}
	if len(llmClient.lastRequest.ToolDefinitions) != 0 {
		t.Fatalf("expected no runtime tool definitions in request, got %d", len(llmClient.lastRequest.ToolDefinitions))
	}
}

func TestPromptStreamForSessionWithOptionsPassesModelOverride(t *testing.T) {
	ctx := context.Background()
	_, app, _ := newToolTestApp(t, true)
	llmClient := &captureRequestLLMClient{}
	app.llm = llmClient

	events, errs := app.PromptStreamForSessionWithOptions(ctx, "agent-2", "s-42", "hello", llm.PromptOptions{
		ModelOverride: "gpt-5-mini",
	})
	for range events {
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected stream error: %v", err)
		}
	}

	if llmClient.lastRequest.Options.ModelOverride != "gpt-5-mini" {
		t.Fatalf("expected model override in request options, got %q", llmClient.lastRequest.Options.ModelOverride)
	}
}

func TestPromptStreamForSessionIncludesPersistedProviderSessionMetadata(t *testing.T) {
	ctx := context.Background()
	_, app, _ := newToolTestApp(t, true)
	llmClient := &captureRequestLLMClient{}
	app.llm = llmClient

	_, err := app.sessions.Update(ctx, "default", "main", func(entry *session.SessionEntry) error {
		entry.Key = "default/main"
		session.SetProviderSessionID(entry, "claudecode", "claude-session-1")
		return nil
	})
	if err != nil {
		t.Fatalf("seed provider session id: %v", err)
	}

	events, errs := app.PromptStreamForSession(ctx, "default", "main", "hello")
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
				t.Fatalf("unexpected stream error: %v", err)
			}
		}
	}

	if llmClient.lastRequest.Session.Provider != "claudecode" {
		t.Fatalf("expected provider metadata claudecode, got %q", llmClient.lastRequest.Session.Provider)
	}
	if llmClient.lastRequest.Session.ProviderSessionID != "claude-session-1" {
		t.Fatalf("expected persisted provider session id, got %q", llmClient.lastRequest.Session.ProviderSessionID)
	}
}

func TestPromptStreamPersistsProviderSessionIDAndRetriesAfterResumeFailure(t *testing.T) {
	ctx := context.Background()
	_, app, _ := newToolTestApp(t, true)
	attempt := 0
	app.llm = &captureRequestLLMClient{
		streamFn: func(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, <-chan error) {
			attempt++
			events := make(chan llm.StreamEvent, 2)
			errs := make(chan error, 1)
			switch attempt {
			case 1:
				if req.Session.ProviderSessionID != "stale-id" {
					errs <- fmt.Errorf("expected stale provider session id on first attempt, got %q", req.Session.ProviderSessionID)
					close(events)
					close(errs)
					return events, errs
				}
				errs <- fmt.Errorf("resume failed: invalid session")
			case 2:
				if req.Session.ProviderSessionID != "" {
					errs <- fmt.Errorf("expected cleared provider session id on retry, got %q", req.Session.ProviderSessionID)
					close(events)
					close(errs)
					return events, errs
				}
				events <- llm.StreamEvent{
					Type: llm.StreamEventProviderMetadata,
					ProviderMetadata: &llm.ProviderMetadata{
						Provider:  "claudecode",
						SessionID: "fresh-session-id",
					},
				}
				events <- llm.StreamEvent{Type: llm.StreamEventFinal, Text: "ok"}
			default:
				errs <- fmt.Errorf("unexpected attempt %d", attempt)
			}
			close(events)
			close(errs)
			return events, errs
		},
	}

	_, err := app.sessions.Update(ctx, "default", "main", func(entry *session.SessionEntry) error {
		session.SetProviderSessionID(entry, "claudecode", "stale-id")
		return nil
	})
	if err != nil {
		t.Fatalf("seed stale provider session id: %v", err)
	}

	events, errs := app.PromptStreamForSession(ctx, "default", "main", "hello")
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
				t.Fatalf("unexpected stream error: %v", err)
			}
		}
	}
	if attempt != 2 {
		t.Fatalf("expected one retry after resume failure, got %d attempts", attempt)
	}

	entry, exists, err := app.sessions.Get(ctx, "default", "main")
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if !exists {
		t.Fatalf("expected session entry to exist")
	}
	if got := session.GetProviderSessionID(entry, "claudecode"); got != "fresh-session-id" {
		t.Fatalf("expected persisted fresh provider session id, got %q", got)
	}
}

func TestPromptStreamForSessionErrorsWhenRequestPathUnsupported(t *testing.T) {
	ctx := context.Background()
	_, app, _ := newToolTestApp(t, true)
	app.llm = &captureLLMClient{}

	events, errs := app.PromptStreamForSession(ctx, "", "", "hello")
	for range events {
	}
	seenErr := ""
	for err := range errs {
		if err != nil {
			seenErr = err.Error()
		}
	}
	if !strings.Contains(seenErr, "request-based prompt streaming") {
		t.Fatalf("expected request-stream support error, got %q", seenErr)
	}
}

func TestPromptIncludesBootstrapContextOnFirstMessageOnly(t *testing.T) {
	ctx := context.Background()
	_, app, workspace := newToolTestApp(t, false)
	llmClient := &captureRequestLLMClient{}
	app.llm = llmClient

	if err := osWriteFile(filepath.Join(workspace, "AGENTS.md"), "# AGENTS\n\nbootstrap-marker\n"); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	if _, err := app.PromptForSession(ctx, "default", "main", "hello"); err != nil {
		t.Fatalf("first prompt: %v", err)
	}
	if !strings.Contains(llmClient.lastRequest.SystemContext, "Workspace bootstrap context") {
		t.Fatalf("expected bootstrap section in first prompt")
	}
	if !strings.Contains(llmClient.lastRequest.SystemContext, "bootstrap-marker") {
		t.Fatalf("expected AGENTS.md content in first prompt")
	}

	if _, err := app.PromptForSession(ctx, "default", "main", "hello again"); err != nil {
		t.Fatalf("second prompt: %v", err)
	}
	if strings.Contains(llmClient.lastRequest.SystemContext, "bootstrap-marker") {
		t.Fatalf("expected bootstrap context to be omitted after initial injection")
	}
}

func TestPromptIncludesBootstrapContextAfterCompaction(t *testing.T) {
	ctx := context.Background()
	_, app, workspace := newToolTestApp(t, false)
	llmClient := &captureRequestLLMClient{}
	app.llm = llmClient

	if err := osWriteFile(filepath.Join(workspace, "AGENTS.md"), "# AGENTS\n\nreinjection-marker\n"); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	if _, err := app.PromptForSession(ctx, "default", "main", "before compaction"); err != nil {
		t.Fatalf("first prompt: %v", err)
	}

	_, err := app.sessions.Update(ctx, "default", "main", func(entry *session.SessionEntry) error {
		entry.CompactionCount++
		return nil
	})
	if err != nil {
		t.Fatalf("increment compaction count: %v", err)
	}

	if _, err := app.PromptForSession(ctx, "default", "main", "after compaction"); err != nil {
		t.Fatalf("second prompt: %v", err)
	}
	if !strings.Contains(llmClient.lastRequest.SystemContext, "reinjection-marker") {
		t.Fatalf("expected AGENTS.md content reinjected after compaction")
	}
}

func TestPromptStreamForSessionPassesThroughProviderToolEvents(t *testing.T) {
	ctx := context.Background()
	_, app, _ := newToolTestApp(t, true)
	respondCalled := false

	app.llm = &captureRequestLLMClient{
		streamFn: func(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, <-chan error) {
			events := make(chan llm.StreamEvent, 2)
			errs := make(chan error)
			events <- llm.StreamEvent{
				Type: llm.StreamEventToolCall,
				ToolCall: &llm.ToolCall{
					ID:   "call-1",
					Name: "Bash",
					Respond: func(ctx context.Context, result llm.ToolResult) error {
						respondCalled = true
						return nil
					},
				},
			}
			events <- llm.StreamEvent{Type: llm.StreamEventFinal, Text: "final output"}
			close(events)
			close(errs)
			return events, errs
		},
	}

	events, errs := app.PromptStreamForSession(ctx, "", "", "hello")
	sawToolCall := false
	sawToolResult := false
	for events != nil || errs != nil {
		select {
		case evt, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if evt.Type == llm.StreamEventToolCall {
				sawToolCall = true
			}
			if evt.Type == llm.StreamEventToolResult {
				sawToolResult = true
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err != nil {
				t.Fatalf("unexpected stream error: %v", err)
			}
		}
	}

	if !sawToolCall {
		t.Fatalf("expected provider tool call event to pass through")
	}
	if sawToolResult {
		t.Fatalf("did not expect runtime to execute tool loop")
	}
	if respondCalled {
		t.Fatalf("did not expect runtime to invoke tool responder")
	}
}

func TestPromptForSessionCachesSkillsSnapshotUntilCompaction(t *testing.T) {
	ctx := context.Background()
	_, app, workspace := newToolTestApp(t, false)

	writeSkillFile(t, workspace, "writer", `---
name: writer
description: Old summary
---
# writer`)

	llmClient := &captureRequestLLMClient{}
	app.llm = llmClient

	if _, err := app.PromptForSession(ctx, "default", "main", "hello"); err != nil {
		t.Fatalf("first prompt: %v", err)
	}
	if !strings.Contains(llmClient.lastRequest.SkillPrompt, "Old summary") {
		t.Fatalf("expected initial skill snapshot in prompt, got %q", llmClient.lastRequest.SkillPrompt)
	}

	writeSkillFile(t, workspace, "writer", `---
name: writer
description: New summary
---
# writer`)

	if _, err := app.PromptForSession(ctx, "default", "main", "hello again"); err != nil {
		t.Fatalf("second prompt: %v", err)
	}
	if strings.Contains(llmClient.lastRequest.SkillPrompt, "New summary") {
		t.Fatalf("expected cached snapshot before compaction, got %q", llmClient.lastRequest.SkillPrompt)
	}

	_, err := app.sessions.Update(ctx, "default", "main", func(entry *session.SessionEntry) error {
		entry.CompactionCount++
		return nil
	})
	if err != nil {
		t.Fatalf("increment compaction count: %v", err)
	}

	if _, err := app.PromptForSession(ctx, "default", "main", "after compaction"); err != nil {
		t.Fatalf("third prompt: %v", err)
	}
	if !strings.Contains(llmClient.lastRequest.SkillPrompt, "New summary") {
		t.Fatalf("expected refreshed skills snapshot after compaction, got %q", llmClient.lastRequest.SkillPrompt)
	}
}

func newToolTestApp(t *testing.T, toolsEnabled bool) (config.Config, *App, string) {
	t.Helper()

	cfg := config.Default()
	cfg.App.Root = t.TempDir()
	cfg.Agents.Defaults.Workspace = "."
	cfg.Session.Store = filepath.Join(cfg.App.Root, "agents", "{agentId}", "sessions", "sessions.json")
	cfg.Agents.Defaults.Memory.Enabled = toolsEnabled
	cfg.Agents.Defaults.Memory.Tools.Get = toolsEnabled
	cfg.Agents.Defaults.Memory.Tools.Search = toolsEnabled
	cfg.Agents.Defaults.Memory.Tools.Grep = toolsEnabled
	cfg.Agents.Defaults.Memory.Sources = []string{"memory"}
	cfg.Agents.Defaults.Memory.Store.Path = filepath.Join("memory", "{agentId}.sqlite")
	cfg.Agents.Defaults.Memory.Sync.OnSearch = true
	cfg.Heartbeat.Enabled = false
	cfg.Cron.Enabled = false

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
	return cfg, app, workspacePath
}

func osWriteFile(path string, body string) error {
	return os.WriteFile(path, []byte(body), 0o600)
}

func writeSkillFile(t *testing.T, workspacePath, skillName, body string) {
	t.Helper()
	skillDir := filepath.Join(workspacePath, "skills", skillName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o600); err != nil {
		t.Fatalf("write skill file: %v", err)
	}
}
