package runtime

import (
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
	"github.com/dgriffin831/localclaw/internal/memory"
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
	return "ok", nil
}

func (c *captureRequestLLMClient) PromptStreamRequest(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, <-chan error) {
	c.lastRequest = req
	if c.streamFn != nil {
		return c.streamFn(ctx, req)
	}
	events := make(chan llm.StreamEvent)
	errs := make(chan error)
	close(events)
	close(errs)
	return events, errs
}

type structuredToolLoopClient struct {
	streamFn func(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, <-chan error)
}

func (c *structuredToolLoopClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{
		SupportsRequestOptions: true,
		StructuredToolCalls:    true,
	}
}

func (c *structuredToolLoopClient) Prompt(ctx context.Context, input string) (string, error) {
	return "unsupported", nil
}

func (c *structuredToolLoopClient) PromptStream(ctx context.Context, input string) (<-chan llm.StreamEvent, <-chan error) {
	events := make(chan llm.StreamEvent)
	errs := make(chan error)
	close(events)
	close(errs)
	return events, errs
}

func (c *structuredToolLoopClient) PromptRequest(ctx context.Context, req llm.Request) (string, error) {
	return "unsupported", nil
}

func (c *structuredToolLoopClient) PromptStreamRequest(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, <-chan error) {
	return c.streamFn(ctx, req)
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

func TestToolDefinitionsDenyOverridesAllow(t *testing.T) {
	_, app, _ := newToolTestApp(t, true)
	app.cfg.Tools.Allow = []string{skills.ToolMemorySearch}
	app.cfg.Tools.Deny = []string{skills.ToolMemorySearch}

	tools := app.ToolDefinitions("")
	for _, tool := range tools {
		if tool.Name == skills.ToolMemorySearch {
			t.Fatalf("expected deny to override allow for %s", skills.ToolMemorySearch)
		}
	}
}

func TestToolDefinitionsHonorAgentPolicyPrecedence(t *testing.T) {
	_, app, _ := newToolTestApp(t, true)
	app.cfg.Tools.Allow = []string{skills.ToolMemorySearch, skills.ToolMemoryGet}
	app.cfg.Agents.Defaults.Tools.Deny = []string{skills.ToolMemorySearch}
	app.cfg.Agents.List = []config.AgentConfig{
		{
			ID: "writer",
			Tools: config.ToolsConfig{
				Allow: []string{skills.ToolMemorySearch, skills.ToolMemoryGet},
				Deny:  []string{},
			},
		},
	}

	defaultTools := app.ToolDefinitions("")
	for _, tool := range defaultTools {
		if tool.Name == skills.ToolMemorySearch {
			t.Fatalf("expected default agent policy to deny %s", skills.ToolMemorySearch)
		}
	}

	writerTools := app.ToolDefinitions("writer")
	foundSearch := false
	for _, tool := range writerTools {
		if tool.Name == skills.ToolMemorySearch {
			foundSearch = true
		}
	}
	if !foundSearch {
		t.Fatalf("expected writer agent override to allow %s", skills.ToolMemorySearch)
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

func TestExecuteToolRejectsUnknownTool(t *testing.T) {
	ctx := context.Background()
	_, app, _ := newToolTestApp(t, true)

	res := app.ExecuteTool(ctx, ToolExecutionRequest{
		Name: "unknown_tool",
	})
	if res.OK {
		t.Fatalf("expected unknown tool call to fail")
	}
	if !strings.Contains(res.Error, "unknown tool") {
		t.Fatalf("expected unknown tool error, got %q", res.Error)
	}
}

func TestExecuteToolBlocksDelegatedToolsByDefault(t *testing.T) {
	ctx := context.Background()
	_, app, _ := newToolTestApp(t, true)

	res := app.ExecuteTool(ctx, ToolExecutionRequest{
		Name:  "remote_search",
		Class: llm.ToolClassDelegated,
		Args:  map[string]interface{}{"query": "hello"},
	})
	if res.OK {
		t.Fatalf("expected delegated tool to be blocked by default")
	}
	if !strings.Contains(res.Error, "policy blocked") {
		t.Fatalf("expected policy blocked error, got %q", res.Error)
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
	if strings.Contains(llm.lastPromptInput, "Memory recall is mandatory") {
		t.Fatalf("memory recall policy should be omitted when tools disabled")
	}
	if _, err := app.Prompt(ctx, "hello again"); err != nil {
		t.Fatalf("second prompt: %v", err)
	}
	if llm.lastPromptInput != "hello again" {
		t.Fatalf("expected prompt passthrough after bootstrap load when tools disabled, got %q", llm.lastPromptInput)
	}
}

func TestPromptIncludesBootstrapContextOnFirstMessageOnly(t *testing.T) {
	ctx := context.Background()
	_, app, workspace := newToolTestApp(t, false)
	llm := &captureLLMClient{}
	app.llm = llm

	if err := osWriteFile(filepath.Join(workspace, "AGENTS.md"), "# AGENTS\n\nbootstrap-marker\n"); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	if _, err := app.PromptForSession(ctx, "default", "main", "first input"); err != nil {
		t.Fatalf("first prompt: %v", err)
	}
	if !strings.Contains(llm.lastPromptInput, "Workspace bootstrap context") {
		t.Fatalf("expected bootstrap context in first prompt, got %q", llm.lastPromptInput)
	}
	if !strings.Contains(llm.lastPromptInput, "## AGENTS.md") {
		t.Fatalf("expected AGENTS.md section in first prompt")
	}
	if !strings.Contains(llm.lastPromptInput, "bootstrap-marker") {
		t.Fatalf("expected AGENTS.md content in first prompt")
	}

	if _, err := app.PromptForSession(ctx, "default", "main", "second input"); err != nil {
		t.Fatalf("second prompt: %v", err)
	}
	if strings.Contains(llm.lastPromptInput, "Workspace bootstrap context") {
		t.Fatalf("bootstrap context should not be included on non-first message without compaction")
	}
	if llm.lastPromptInput != "second input" {
		t.Fatalf("expected second prompt passthrough, got %q", llm.lastPromptInput)
	}
}

func TestPromptReinjectsBootstrapAfterCompactionIncrement(t *testing.T) {
	ctx := context.Background()
	_, app, workspace := newToolTestApp(t, false)
	llm := &captureLLMClient{}
	app.llm = llm

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
	if !strings.Contains(llm.lastPromptInput, "Workspace bootstrap context") {
		t.Fatalf("expected bootstrap reinjection after compaction increment")
	}
	if !strings.Contains(llm.lastPromptInput, "reinjection-marker") {
		t.Fatalf("expected AGENTS.md content after compaction reinjection")
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

func TestPromptStreamForSessionHardCutoverDoesNotExecuteLegacyStructuredToolLoop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, app, _ := newToolTestApp(t, true)
	respondCalled := make(chan struct{}, 1)

	app.llm = &structuredToolLoopClient{
		streamFn: func(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, <-chan error) {
			events := make(chan llm.StreamEvent, 2)
			errs := make(chan error, 1)
			go func() {
				defer close(events)
				defer close(errs)
				events <- llm.StreamEvent{
					Type: llm.StreamEventToolCall,
					ToolCall: &llm.ToolCall{
						ID:    "tool-legacy",
						Name:  skills.ToolMemorySearch,
						Args:  map[string]interface{}{"query": "search target"},
						Class: llm.ToolClassLocal,
						Respond: func(ctx context.Context, result llm.ToolResult) error {
							select {
							case respondCalled <- struct{}{}:
							default:
							}
							return nil
						},
					},
				}
				events <- llm.StreamEvent{Type: llm.StreamEventFinal, Text: "final output"}
			}()
			return events, errs
		},
	}

	events, errs := app.PromptStreamForSession(ctx, "", "", "hello")
	final := ""
	sawToolCall := false
	sawToolResult := false

	for events != nil || errs != nil {
		select {
		case evt, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if evt.Type == llm.StreamEventToolCall && evt.ToolCall != nil {
				sawToolCall = true
			}
			if evt.Type == llm.StreamEventToolResult {
				sawToolResult = true
			}
			if evt.Type == llm.StreamEventFinal {
				final = strings.TrimSpace(evt.Text)
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
		t.Fatalf("did not expect runtime to execute legacy structured tool loop")
	}
	select {
	case <-respondCalled:
		t.Fatalf("did not expect tool responder to be called in hard-cutover mode")
	default:
	}
	if final != "final output" {
		t.Fatalf("expected final output after tool loop, got %q", final)
	}
}

func TestHandleToolCallEventTelemetryIncludesOwnershipClassOnAllEvents(t *testing.T) {
	ctx := context.Background()
	_, app, workspace := newToolTestApp(t, true)
	if err := osWriteFile(filepath.Join(workspace, "MEMORY.md"), "incident summary"); err != nil {
		t.Fatalf("write memory file: %v", err)
	}

	outEvents := make(chan llm.StreamEvent, 1)
	outErrs := make(chan error, 1)
	ok := app.handleToolCallEvent(ctx, ResolveSession("", ""), &llm.ToolCall{
		ID:    "tool-1",
		Name:  skills.ToolMemorySearch,
		Class: llm.ToolClassUnspecified,
		Args: map[string]interface{}{
			"query": "incident",
		},
	}, outEvents, outErrs)
	if !ok {
		t.Fatalf("expected handleToolCallEvent to succeed")
	}

	select {
	case err := <-outErrs:
		if err != nil {
			t.Fatalf("unexpected tool call error: %v", err)
		}
	default:
	}

	select {
	case evt := <-outEvents:
		if evt.ToolResult == nil {
			t.Fatalf("expected tool result event payload")
		}
		if evt.ToolResult.Class != llm.ToolClassLocal {
			t.Fatalf("expected tool result class local, got %q", evt.ToolResult.Class)
		}
	default:
		t.Fatalf("expected tool result event")
	}

	transcriptPath, err := app.ResolveTranscriptPath("", "")
	if err != nil {
		t.Fatalf("resolve transcript path: %v", err)
	}

	seenStarted := false
	seenCompleted := false
	for _, payload := range readToolPayloadsFromTranscript(t, transcriptPath) {
		eventType := strings.TrimSpace(fmt.Sprintf("%v", payload["type"]))
		class := strings.TrimSpace(fmt.Sprintf("%v", payload["class"]))
		switch eventType {
		case "tool_call_started":
			seenStarted = true
			if class != string(llm.ToolClassLocal) {
				t.Fatalf("expected started class %q, got %q", llm.ToolClassLocal, class)
			}
		case "tool_call_completed":
			seenCompleted = true
			if class != string(llm.ToolClassLocal) {
				t.Fatalf("expected completed class %q, got %q", llm.ToolClassLocal, class)
			}
		}
	}
	if !seenStarted {
		t.Fatalf("expected tool_call_started telemetry event")
	}
	if !seenCompleted {
		t.Fatalf("expected tool_call_completed telemetry event")
	}
}

func TestHandleToolCallEventTelemetryIncludesOwnershipClassForBlockedAndFailedEvents(t *testing.T) {
	testCases := []struct {
		name          string
		call          llm.ToolCall
		expectedEvent string
		expectedClass llm.ToolClass
	}{
		{
			name: "blocked delegated tool",
			call: llm.ToolCall{
				ID:    "tool-blocked",
				Name:  "Bash",
				Class: llm.ToolClassDelegated,
				Args:  map[string]interface{}{},
			},
			expectedEvent: "tool_call_blocked",
			expectedClass: llm.ToolClassDelegated,
		},
		{
			name: "failed local unknown tool",
			call: llm.ToolCall{
				ID:    "tool-failed",
				Name:  "unknown_tool",
				Class: llm.ToolClassLocal,
				Args:  map[string]interface{}{},
			},
			expectedEvent: "tool_call_failed",
			expectedClass: llm.ToolClassLocal,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			_, app, _ := newToolTestApp(t, true)
			outEvents := make(chan llm.StreamEvent, 1)
			outErrs := make(chan error, 1)

			ok := app.handleToolCallEvent(ctx, ResolveSession("", ""), &tc.call, outEvents, outErrs)
			if !ok {
				t.Fatalf("expected handleToolCallEvent to succeed")
			}

			select {
			case err := <-outErrs:
				if err != nil {
					t.Fatalf("unexpected tool call error: %v", err)
				}
			default:
			}

			select {
			case evt := <-outEvents:
				if evt.ToolResult == nil {
					t.Fatalf("expected tool result event payload")
				}
				if evt.ToolResult.Class != tc.expectedClass {
					t.Fatalf("expected tool result class %q, got %q", tc.expectedClass, evt.ToolResult.Class)
				}
			default:
				t.Fatalf("expected tool result event")
			}

			transcriptPath, err := app.ResolveTranscriptPath("", "")
			if err != nil {
				t.Fatalf("resolve transcript path: %v", err)
			}
			payloads := readToolPayloadsFromTranscript(t, transcriptPath)
			if len(payloads) < 2 {
				t.Fatalf("expected at least two tool telemetry events, got %d", len(payloads))
			}

			seenStarted := false
			seenExpected := false
			for _, payload := range payloads {
				eventType := strings.TrimSpace(fmt.Sprintf("%v", payload["type"]))
				class := strings.TrimSpace(fmt.Sprintf("%v", payload["class"]))
				switch eventType {
				case "tool_call_started":
					seenStarted = true
					if class != string(tc.expectedClass) {
						t.Fatalf("expected started class %q, got %q", tc.expectedClass, class)
					}
				case tc.expectedEvent:
					seenExpected = true
					if class != string(tc.expectedClass) {
						t.Fatalf("expected %s class %q, got %q", tc.expectedEvent, tc.expectedClass, class)
					}
				}
			}
			if !seenStarted {
				t.Fatalf("expected tool_call_started telemetry event")
			}
			if !seenExpected {
				t.Fatalf("expected %s telemetry event", tc.expectedEvent)
			}
		})
	}
}

func readToolPayloadsFromTranscript(t *testing.T, transcriptPath string) []map[string]interface{} {
	t.Helper()
	body, err := os.ReadFile(transcriptPath)
	if err != nil {
		t.Fatalf("read transcript: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) == 0 {
		t.Fatalf("expected transcript lines, got empty transcript")
	}
	payloads := make([]map[string]interface{}, 0, len(lines))
	for _, line := range lines {
		var row map[string]interface{}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		if strings.TrimSpace(fmt.Sprintf("%v", row["role"])) != "tool" {
			continue
		}
		rawContent := strings.TrimSpace(fmt.Sprintf("%v", row["content"]))
		if rawContent == "" {
			continue
		}
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(rawContent), &payload); err != nil {
			t.Fatalf("decode tool payload: %v", err)
		}
		payloads = append(payloads, payload)
	}
	return payloads
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
	cfg.State.Root = t.TempDir()
	cfg.Agents.Defaults.Workspace = "."
	cfg.Workspace.Root = "."
	cfg.Session.Store = filepath.Join(cfg.State.Root, "agents", "{agentId}", "sessions", "sessions.json")
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
