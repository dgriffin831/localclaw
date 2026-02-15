package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/llm/claudecode"
	"github.com/dgriffin831/localclaw/internal/memory"
	"github.com/dgriffin831/localclaw/internal/skills"
)

type captureLLMClient struct {
	lastPromptInput string
}

func (c *captureLLMClient) Prompt(ctx context.Context, input string) (string, error) {
	c.lastPromptInput = input
	return "ok", nil
}

func (c *captureLLMClient) PromptStream(ctx context.Context, input string) (<-chan claudecode.StreamEvent, <-chan error) {
	c.lastPromptInput = input
	events := make(chan claudecode.StreamEvent)
	errs := make(chan error)
	close(events)
	close(errs)
	return events, errs
}

func TestToolDefinitionsIncludeMemoryToolsWhenEnabled(t *testing.T) {
	_, app, _ := newToolTestApp(t, true)

	tools := app.ToolDefinitions("")
	if len(tools) == 0 {
		t.Fatalf("expected runtime tools when memory search is enabled")
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
}

func TestExecuteToolMemorySearchReturnsResults(t *testing.T) {
	ctx := context.Background()
	_, app, workspace := newToolTestApp(t, true)

	if err := osWriteFile(filepath.Join(workspace, "MEMORY.md"), "tool memory match\nsecond line"); err != nil {
		t.Fatalf("write memory file: %v", err)
	}

	res := app.ExecuteTool(ctx, ToolExecutionRequest{
		Name: skills.ToolMemorySearch,
		Args: map[string]interface{}{
			"query": "tool memory",
		},
	})
	if !res.OK {
		t.Fatalf("expected memory_search success, got error %q", res.Error)
	}

	rawResults, ok := res.Data["results"]
	if !ok {
		t.Fatalf("expected results payload")
	}
	results, ok := rawResults.([]memory.SearchResult)
	if !ok {
		t.Fatalf("expected []memory.SearchResult payload, got %T", rawResults)
	}
	if len(results) == 0 {
		t.Fatalf("expected at least one memory result")
	}
}

func TestExecuteToolFailureIsGraceful(t *testing.T) {
	ctx := context.Background()
	_, app, _ := newToolTestApp(t, true)
	app.cfg.Agents.Defaults.MemorySearch.Store.Path = ""

	res := app.ExecuteTool(ctx, ToolExecutionRequest{
		Name: skills.ToolMemorySearch,
		Args: map[string]interface{}{"query": "anything"},
	})
	if res.OK {
		t.Fatalf("expected failure result when store path is invalid")
	}
	if res.Error == "" {
		t.Fatalf("expected error payload on tool failure")
	}
}

func TestPromptIncludesMemoryRecallPolicyWhenToolsEnabled(t *testing.T) {
	ctx := context.Background()
	_, app, _ := newToolTestApp(t, true)
	llm := &captureLLMClient{}
	app.llm = llm

	if _, err := app.Prompt(ctx, "hello"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if !strings.Contains(llm.lastPromptInput, "Memory recall is mandatory") {
		t.Fatalf("expected memory recall policy in prompt")
	}
	if !strings.Contains(llm.lastPromptInput, "memory_search") {
		t.Fatalf("expected memory_search tool schema in prompt")
	}
	if !strings.Contains(llm.lastPromptInput, "User input:\nhello") {
		t.Fatalf("expected original user input in composed prompt")
	}
}

func TestPromptOmitsMemoryRecallPolicyWhenToolsDisabled(t *testing.T) {
	ctx := context.Background()
	_, app, _ := newToolTestApp(t, false)
	llm := &captureLLMClient{}
	app.llm = llm

	if _, err := app.Prompt(ctx, "hello"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if llm.lastPromptInput != "hello" {
		t.Fatalf("expected prompt passthrough when tools disabled, got %q", llm.lastPromptInput)
	}
}

func TestPromptStreamForSessionIncludesResolvedSessionKey(t *testing.T) {
	ctx := context.Background()
	_, app, _ := newToolTestApp(t, true)
	llm := &captureLLMClient{}
	app.llm = llm

	events, errs := app.PromptStreamForSession(ctx, "agent-2", "s-42", "hello")
	for range events {
	}
	for range errs {
	}

	if !strings.Contains(llm.lastPromptInput, "Current session_key: agent-2/s-42") {
		t.Fatalf("expected resolved session key in composed prompt, got %q", llm.lastPromptInput)
	}
}

func TestExecuteToolRejectsFractionalMaxResults(t *testing.T) {
	ctx := context.Background()
	_, app, _ := newToolTestApp(t, true)

	res := app.ExecuteTool(ctx, ToolExecutionRequest{
		Name: skills.ToolMemorySearch,
		Args: map[string]interface{}{
			"query":       "tool memory",
			"max_results": 2.5,
		},
	})
	if res.OK {
		t.Fatalf("expected failure for fractional max_results")
	}
	if !strings.Contains(res.Error, "max_results must be an integer") {
		t.Fatalf("expected integer validation error, got %q", res.Error)
	}
}

func newToolTestApp(t *testing.T, toolsEnabled bool) (config.Config, *App, string) {
	t.Helper()

	cfg := config.Default()
	cfg.State.Root = t.TempDir()
	cfg.Agents.Defaults.Workspace = "."
	cfg.Workspace.Root = "."
	cfg.Agents.Defaults.MemorySearch.Enabled = toolsEnabled
	cfg.Agents.Defaults.MemorySearch.Sources = []string{"memory"}
	cfg.Agents.Defaults.MemorySearch.Provider = "none"
	cfg.Agents.Defaults.MemorySearch.Fallback = "none"
	cfg.Agents.Defaults.MemorySearch.Store.Path = filepath.Join("memory", "{agentId}.sqlite")
	cfg.Agents.Defaults.MemorySearch.Store.Vector.Enabled = false
	cfg.Agents.Defaults.MemorySearch.Cache.Enabled = false
	cfg.Agents.Defaults.MemorySearch.Query.Hybrid.Enabled = false
	cfg.Agents.Defaults.MemorySearch.Sync.OnSearch = true
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
