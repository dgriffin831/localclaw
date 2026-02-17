package claudecode

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dgriffin831/localclaw/internal/llm"
)

func TestBuildCommandArgsUsesStreamJSONVerbose(t *testing.T) {
	client := NewClient(Settings{BinaryPath: "claude"})
	args := client.buildCommandArgs("hello")
	want := []string{"-p", "hello", "--output-format", "stream-json", "--verbose"}
	if len(args) != len(want) {
		t.Fatalf("unexpected arg length: got %d want %d (%v)", len(args), len(want), args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("unexpected arg[%d]: got %q want %q (all=%v)", i, args[i], want[i], args)
		}
	}
}

func TestBuildCommandArgsForRequestIncludesMCPFlagsAndSystemPrompt(t *testing.T) {
	client := NewClient(Settings{
		BinaryPath:           "claude",
		StrictMCPConfig:      true,
		MCPServerBinaryPath:  "localclaw",
		MCPServerArgs:        []string{"mcp", "serve"},
		MCPConfigDir:         t.TempDir(),
		MCPServerEnvironment: map[string]string{"LOCALCLAW_ENV": "1"},
	})

	req := llm.Request{
		Input:         "hello",
		SystemContext: "system guidance",
		SkillPrompt:   "skill guidance",
		ToolDefinitions: []llm.ToolDefinition{
			{Name: "memory_search"},
			{Name: "memory_get"},
		},
	}
	args := client.buildCommandArgsForRequest(req, "/tmp/mcp.json")
	if !containsArgSequence(args, []string{"-p", "hello"}) {
		t.Fatalf("expected prompt args, got %v", args)
	}
	if !containsArgSequence(args, []string{"--output-format", "stream-json"}) {
		t.Fatalf("expected stream-json output mode, got %v", args)
	}
	if !containsArgSequence(args, []string{"--mcp-config", "/tmp/mcp.json"}) {
		t.Fatalf("expected mcp config args, got %v", args)
	}
	if !containsArgSequence(args, []string{"--strict-mcp-config"}) {
		t.Fatalf("expected strict mcp flag, got %v", args)
	}
	if !containsArgSequence(args, []string{"--allowed-tools", "mcp__localclaw__localclaw_memory_search,mcp__localclaw__localclaw_memory_get"}) {
		t.Fatalf("expected allowed-tools args, got %v", args)
	}
	if !containsArgSequence(args, []string{"--append-system-prompt", "system guidance\n\nskill guidance"}) {
		t.Fatalf("expected append-system-prompt args, got %v", args)
	}
	if !containsArgSequence(args, []string{"--session-id"}) {
		t.Fatalf("expected session-id arg for start mode, got %v", args)
	}
}

func TestBuildCommandArgsForRequestOmitsStrictFlagWhenDisabled(t *testing.T) {
	client := NewClient(Settings{BinaryPath: "claude", StrictMCPConfig: false})
	args := client.buildCommandArgsForRequest(llm.Request{Input: "hello"}, "/tmp/mcp.json")
	for _, arg := range args {
		if arg == "--strict-mcp-config" {
			t.Fatalf("expected strict flag to be omitted when disabled")
		}
	}
}

func TestBuildCommandArgsForRequestOmitsAllowedToolsWhenNoToolDefinitions(t *testing.T) {
	client := NewClient(Settings{BinaryPath: "claude", StrictMCPConfig: true})
	args := client.buildCommandArgsForRequest(llm.Request{Input: "hello"}, "/tmp/mcp.json")
	for _, arg := range args {
		if arg == "--allowed-tools" {
			t.Fatalf("expected allowed-tools flag to be omitted when no tool definitions are available")
		}
	}
}

func TestBuildCommandArgsForRequestIncludesConfiguredExtraArgs(t *testing.T) {
	client := NewClient(Settings{
		BinaryPath: "claude",
		ExtraArgs: []string{
			"--dangerously-skip-permissions",
			"--allowed-tools",
			"mcp__localclaw__localclaw_memory_search,mcp__localclaw__localclaw_memory_get",
		},
	})
	args := client.buildCommandArgsForRequest(llm.Request{Input: "hello"}, "/tmp/mcp.json")
	if !containsArgSequence(args, []string{"--dangerously-skip-permissions"}) {
		t.Fatalf("expected --dangerously-skip-permissions in args, got %v", args)
	}
	if !containsArgSequence(args, []string{"--allowed-tools", "mcp__localclaw__localclaw_memory_search,mcp__localclaw__localclaw_memory_get"}) {
		t.Fatalf("expected configured --allowed-tools in args, got %v", args)
	}
}

func TestBuildCommandArgsForRequestAddsStartSessionArgWhenNoPersistedSession(t *testing.T) {
	client := NewClient(Settings{
		BinaryPath:   "claude",
		SessionMode:  "always",
		SessionArg:   "--session-id",
		ResumeArgs:   []string{"--resume", "{sessionId}"},
		MCPConfigDir: t.TempDir(),
	})
	args := client.buildCommandArgsForRequest(llm.Request{Input: "hello"}, "/tmp/mcp.json")
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--session-id" && strings.TrimSpace(args[i+1]) != "" {
			return
		}
	}
	t.Fatalf("expected --session-id <id> args for new session, got %v", args)
}

func TestBuildCommandArgsForRequestUsesResumeArgsWhenPersistedSessionExists(t *testing.T) {
	client := NewClient(Settings{
		BinaryPath:  "claude",
		SessionMode: "always",
		ResumeArgs:  []string{"--resume", "{sessionId}"},
	})
	args := client.buildCommandArgsForRequest(llm.Request{
		Input: "hello",
		Session: llm.SessionMetadata{
			ProviderSessionID: "sess-42",
		},
	}, "/tmp/mcp.json")
	if !containsArgSequence(args, []string{"--resume", "sess-42"}) {
		t.Fatalf("expected resume args with provider session id, got %v", args)
	}
}

func TestBuildCommandArgsForRequestUsesModelOverride(t *testing.T) {
	client := NewClient(Settings{
		BinaryPath: "claude",
	})
	args := client.buildCommandArgsForRequest(llm.Request{
		Input: "hello",
		Options: llm.PromptOptions{
			ModelOverride: "claude-opus-4-6",
		},
	}, "/tmp/mcp.json")
	if !containsArgSequence(args, []string{"--model", "claude-opus-4-6"}) {
		t.Fatalf("expected model override args, got %v", args)
	}
}

func TestGenerateSessionIDProducesUUIDv4(t *testing.T) {
	id := generateSessionID()
	pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !pattern.MatchString(id) {
		t.Fatalf("expected UUIDv4 format, got %q", id)
	}
}

func TestPrepareRunScopedMCPConfigWritesExpectedPayloadAndCleanup(t *testing.T) {
	tmpDir := t.TempDir()
	client := NewClient(Settings{
		BinaryPath:           "claude",
		MCPConfigDir:         tmpDir,
		MCPServerBinaryPath:  "localclaw",
		MCPServerArgs:        []string{"mcp", "serve"},
		MCPServerEnvironment: map[string]string{"LOCALCLAW_ENV": "1"},
	})

	path, cleanup, err := client.prepareRunScopedMCPConfig()
	if err != nil {
		t.Fatalf("prepareRunScopedMCPConfig: %v", err)
	}
	if filepath.Dir(path) != tmpDir {
		t.Fatalf("expected config in %q, got %q", tmpDir, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read mcp config: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal mcp config: %v", err)
	}
	servers, ok := payload["mcpServers"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected mcpServers object")
	}
	localclaw, ok := servers["localclaw"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected mcpServers.localclaw object")
	}
	if localclaw["type"] != "stdio" {
		t.Fatalf("expected stdio type, got %#v", localclaw["type"])
	}
	if localclaw["command"] != "localclaw" {
		t.Fatalf("expected command localclaw, got %#v", localclaw["command"])
	}
	args, ok := localclaw["args"].([]interface{})
	if !ok || len(args) != 2 {
		t.Fatalf("expected args [mcp serve], got %#v", localclaw["args"])
	}
	if args[0] != "mcp" || args[1] != "serve" {
		t.Fatalf("unexpected args: %#v", args)
	}

	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected cleanup to remove %q, stat err=%v", path, err)
	}
}

func TestPrepareRunScopedMCPConfigRejectsInvalidServerConfig(t *testing.T) {
	tests := []struct {
		name     string
		settings Settings
		wantErr  string
	}{
		{
			name: "missing binary",
			settings: Settings{
				BinaryPath:          "claude",
				MCPServerBinaryPath: "",
				MCPServerArgs:       []string{"mcp", "serve"},
			},
			wantErr: "mcp server binary path is required",
		},
		{
			name: "missing mcp serve args",
			settings: Settings{
				BinaryPath:          "claude",
				MCPServerBinaryPath: "localclaw",
				MCPServerArgs:       []string{"serve"},
			},
			wantErr: "mcp server args must include \"mcp serve\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &LocalClient{settings: tt.settings}
			_, cleanup, err := client.prepareRunScopedMCPConfig()
			if cleanup != nil {
				cleanup()
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestPromptStreamRequestReturnsErrorWhenMCPConfigFails(t *testing.T) {
	client := NewClient(Settings{
		BinaryPath:          "claude",
		MCPServerBinaryPath: "localclaw",
		MCPServerArgs:       []string{"serve"},
	})

	events, errs := client.PromptStreamRequest(context.Background(), llm.Request{Input: "hello"})
	if _, ok := <-events; ok {
		t.Fatalf("expected events channel to be closed")
	}
	err := <-errs
	if err == nil || !strings.Contains(err.Error(), "prepare claude mcp config") {
		t.Fatalf("expected mcp config preparation error, got %v", err)
	}
}

func TestParseStreamJSONLineAssistantTextAndFinalResult(t *testing.T) {
	toolNames := map[string]string{}

	assistantLine := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"OK"}]}}`
	events, resultErr, err := parseStreamJSONLine(assistantLine, toolNames)
	if err != nil {
		t.Fatalf("parse assistant line: %v", err)
	}
	if resultErr != "" {
		t.Fatalf("unexpected result error: %q", resultErr)
	}
	if len(events) != 1 {
		t.Fatalf("expected one assistant event, got %d", len(events))
	}
	if events[0].Type != llm.StreamEventDelta {
		t.Fatalf("expected delta event, got %q", events[0].Type)
	}
	if events[0].Text != "OK" {
		t.Fatalf("expected delta text OK, got %q", events[0].Text)
	}

	resultLine := `{"type":"result","subtype":"success","is_error":false,"result":"OK"}`
	events, resultErr, err = parseStreamJSONLine(resultLine, toolNames)
	if err != nil {
		t.Fatalf("parse result line: %v", err)
	}
	if resultErr != "" {
		t.Fatalf("unexpected result error: %q", resultErr)
	}
	if len(events) != 1 {
		t.Fatalf("expected one final event, got %d", len(events))
	}
	if events[0].Type != llm.StreamEventFinal {
		t.Fatalf("expected final event, got %q", events[0].Type)
	}
	if events[0].Text != "OK" {
		t.Fatalf("expected final text OK, got %q", events[0].Text)
	}
}

func TestParseStreamJSONLineToolUseAndToolResult(t *testing.T) {
	toolNames := map[string]string{}

	toolUseLine := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls -1"}}]}}`
	events, resultErr, err := parseStreamJSONLine(toolUseLine, toolNames)
	if err != nil {
		t.Fatalf("parse tool_use line: %v", err)
	}
	if resultErr != "" {
		t.Fatalf("unexpected result error: %q", resultErr)
	}
	if len(events) != 1 {
		t.Fatalf("expected one tool_call event, got %d", len(events))
	}
	if events[0].Type != llm.StreamEventToolCall {
		t.Fatalf("expected tool_call event, got %q", events[0].Type)
	}
	if events[0].ToolCall == nil {
		t.Fatalf("expected tool call payload")
	}
	if events[0].ToolCall.ID != "toolu_1" {
		t.Fatalf("expected call id toolu_1, got %q", events[0].ToolCall.ID)
	}
	if events[0].ToolCall.Name != "Bash" {
		t.Fatalf("expected tool name Bash, got %q", events[0].ToolCall.Name)
	}
	if events[0].ToolCall.Class != llm.ToolClassDelegated {
		t.Fatalf("expected delegated tool class, got %q", events[0].ToolCall.Class)
	}
	command, _ := events[0].ToolCall.Args["command"].(string)
	if command != "ls -1" {
		t.Fatalf("expected command arg ls -1, got %q", command)
	}

	toolResultLine := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"AGENTS.md","is_error":false}]},"tool_use_result":{"stdout":"AGENTS.md"}}`
	events, resultErr, err = parseStreamJSONLine(toolResultLine, toolNames)
	if err != nil {
		t.Fatalf("parse tool_result line: %v", err)
	}
	if resultErr != "" {
		t.Fatalf("unexpected result error: %q", resultErr)
	}
	if len(events) != 1 {
		t.Fatalf("expected one tool_result event, got %d", len(events))
	}
	if events[0].Type != llm.StreamEventToolResult {
		t.Fatalf("expected tool_result event, got %q", events[0].Type)
	}
	if events[0].ToolResult == nil {
		t.Fatalf("expected tool result payload")
	}
	if !events[0].ToolResult.OK {
		t.Fatalf("expected successful tool result")
	}
	if events[0].ToolResult.Tool != "Bash" {
		t.Fatalf("expected tool result name Bash, got %q", events[0].ToolResult.Tool)
	}
	if events[0].ToolResult.CallID != "toolu_1" {
		t.Fatalf("expected tool result call id toolu_1, got %q", events[0].ToolResult.CallID)
	}
	if events[0].ToolResult.Status != "completed" {
		t.Fatalf("expected status completed, got %q", events[0].ToolResult.Status)
	}
	if events[0].ToolResult.Class != llm.ToolClassDelegated {
		t.Fatalf("expected delegated tool class on result, got %q", events[0].ToolResult.Class)
	}
	content, _ := events[0].ToolResult.Data["content"].(string)
	if content != "AGENTS.md" {
		t.Fatalf("expected tool content AGENTS.md, got %q", content)
	}
}

func TestParseStreamJSONLineToolResultError(t *testing.T) {
	toolNames := map[string]string{}
	line := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_404","content":"permission denied","is_error":true}]}}`
	events, resultErr, err := parseStreamJSONLine(line, toolNames)
	if err == nil {
		t.Fatalf("expected parse error when tool mapping is missing")
	}
	if resultErr != "" {
		t.Fatalf("unexpected result error: %q", resultErr)
	}
	if len(events) != 0 {
		t.Fatalf("expected no events on parse error, got %d", len(events))
	}
}

func TestParseStreamJSONLineResultError(t *testing.T) {
	toolNames := map[string]string{}
	line := `{"type":"result","subtype":"error_max_turns","is_error":true,"result":"max turns reached"}`
	events, resultErr, err := parseStreamJSONLine(line, toolNames)
	if err != nil {
		t.Fatalf("parse line: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one final event, got %d", len(events))
	}
	if events[0].Type != llm.StreamEventFinal {
		t.Fatalf("expected final event, got %q", events[0].Type)
	}
	if resultErr != "max turns reached" {
		t.Fatalf("expected result error text, got %q", resultErr)
	}
}

func TestParseStreamJSONLineResultErrorUsesErrorsArray(t *testing.T) {
	toolNames := map[string]string{}
	line := `{"type":"result","subtype":"error_during_execution","is_error":true,"errors":["No conversation found with session ID: stale-id"]}`
	events, resultErr, err := parseStreamJSONLine(line, toolNames)
	if err != nil {
		t.Fatalf("parse line: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected no final events for empty result text, got %d", len(events))
	}
	if resultErr != "No conversation found with session ID: stale-id" {
		t.Fatalf("expected provider error from errors array, got %q", resultErr)
	}
}

func TestPromptStreamRequestSurfacesResultErrorWhenProcessExitsNonZero(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "fake-claude.sh")
	script := `#!/usr/bin/env bash
echo '{"type":"result","subtype":"error_during_execution","is_error":true,"errors":["No conversation found with session ID: stale-id"]}'
exit 1
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude script: %v", err)
	}

	client := NewClient(Settings{
		BinaryPath:          scriptPath,
		MCPConfigDir:        tmpDir,
		MCPServerBinaryPath: "localclaw",
		MCPServerArgs:       []string{"mcp", "serve"},
	})

	events, errs := client.PromptStreamRequest(context.Background(), llm.Request{Input: "hello"})
	for range events {
	}
	var gotErr error
	for err := range errs {
		if err != nil {
			gotErr = err
		}
	}
	if gotErr == nil {
		t.Fatalf("expected error from non-zero fake claude command")
	}
	if !strings.Contains(gotErr.Error(), "No conversation found with session ID") {
		t.Fatalf("expected surfaced result error text, got %v", gotErr)
	}
}

func TestPromptStreamRequestDoesNotEmitProviderSessionMetadataAfterResultError(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "fake-claude-result-then-system.sh")
	script := `#!/usr/bin/env bash
echo '{"type":"result","subtype":"error_during_execution","is_error":true,"errors":["No conversation found with session ID: stale-id"]}'
echo '{"type":"system","subtype":"init","session_id":"non-resumable-id","model":"claude-opus-4-6"}'
exit 1
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude script: %v", err)
	}

	client := NewClient(Settings{
		BinaryPath:          scriptPath,
		MCPConfigDir:        tmpDir,
		MCPServerBinaryPath: "localclaw",
		MCPServerArgs:       []string{"mcp", "serve"},
	})

	events, errs := client.PromptStreamRequest(context.Background(), llm.Request{Input: "hello"})
	providerMetadataCount := 0
	for evt := range events {
		if evt.Type == llm.StreamEventProviderMetadata {
			providerMetadataCount++
		}
	}
	var gotErr error
	for err := range errs {
		if err != nil {
			gotErr = err
		}
	}
	if gotErr == nil {
		t.Fatalf("expected error from non-zero fake claude command")
	}
	if providerMetadataCount != 0 {
		t.Fatalf("expected no provider metadata events after result error, got %d", providerMetadataCount)
	}
}

func TestParseStreamJSONLineExtractsProviderSessionMetadata(t *testing.T) {
	toolNames := map[string]string{}
	line := `{"type":"system","subtype":"init","session_id":"session-abc","model":"claude-opus","tools":["Bash"]}`
	events, resultErr, err := parseStreamJSONLine(line, toolNames)
	if err != nil {
		t.Fatalf("parse line: %v", err)
	}
	if resultErr != "" {
		t.Fatalf("unexpected result error: %q", resultErr)
	}
	if len(events) != 1 {
		t.Fatalf("expected one provider metadata event, got %d", len(events))
	}
	if events[0].Type != llm.StreamEventProviderMetadata || events[0].ProviderMetadata == nil {
		t.Fatalf("expected provider metadata event, got %#v", events[0])
	}
	if events[0].ProviderMetadata.Provider != "claudecode" {
		t.Fatalf("expected provider claudecode, got %q", events[0].ProviderMetadata.Provider)
	}
	if events[0].ProviderMetadata.SessionID != "session-abc" {
		t.Fatalf("expected session id session-abc, got %q", events[0].ProviderMetadata.SessionID)
	}
}

func TestParseClaudeModelCatalogProbeOutput(t *testing.T) {
	raw := "```json\n{\"models\":[\"claude-sonnet-4-5\",{\"name\":\"claude-opus-4-6\"}]}\n```"
	models := parseClaudeModelCatalogProbeOutput(raw)
	if len(models) != 2 {
		t.Fatalf("expected 2 parsed models, got %d", len(models))
	}
	if models[0].Name != "claude-opus-4-6" {
		t.Fatalf("expected sorted first model claude-opus-4-6, got %q", models[0].Name)
	}
	if models[0].Reasoning.Supported {
		t.Fatalf("expected claude models to report reasoning unsupported")
	}
}

func TestBuildFallbackModelDescriptorsUsesProfile(t *testing.T) {
	client := NewClient(Settings{
		BinaryPath: "claude",
		Profile:    "default",
	})
	models := client.buildFallbackModelDescriptors()
	if len(models) != 1 {
		t.Fatalf("expected one fallback model from profile, got %d", len(models))
	}
	if models[0].Name != "default" {
		t.Fatalf("expected fallback model name default, got %q", models[0].Name)
	}
	if models[0].Reasoning.Supported {
		t.Fatalf("expected fallback claude model reasoning unsupported")
	}
}

func containsArgSequence(args []string, seq []string) bool {
	if len(seq) == 0 || len(args) < len(seq) {
		return false
	}
	for i := 0; i <= len(args)-len(seq); i++ {
		matches := true
		for j := range seq {
			if args[i+j] != seq[j] {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}

func TestParseStreamJSONLineInvalidJSON(t *testing.T) {
	toolNames := map[string]string{}
	_, _, err := parseStreamJSONLine("not json", toolNames)
	if err == nil {
		t.Fatalf("expected parse error for invalid JSON line")
	}
}

func TestParseStreamJSONLineSystemInitProviderMetadata(t *testing.T) {
	toolNames := map[string]string{}
	line := `{"type":"system","subtype":"init","tools":["Task","Bash","WebFetch"],"model":"claude-opus-4-6"}`
	events, resultErr, err := parseStreamJSONLine(line, toolNames)
	if err != nil {
		t.Fatalf("parse line: %v", err)
	}
	if resultErr != "" {
		t.Fatalf("unexpected result error: %q", resultErr)
	}
	if len(events) != 1 {
		t.Fatalf("expected one provider metadata event, got %d", len(events))
	}
	if events[0].Type != llm.StreamEventProviderMetadata {
		t.Fatalf("expected provider metadata event, got %q", events[0].Type)
	}
	if events[0].ProviderMetadata == nil {
		t.Fatalf("expected provider metadata payload")
	}
	if events[0].ProviderMetadata.Provider != "claudecode" {
		t.Fatalf("expected provider claudecode, got %q", events[0].ProviderMetadata.Provider)
	}
	if events[0].ProviderMetadata.Model != "claude-opus-4-6" {
		t.Fatalf("expected model claude-opus-4-6, got %q", events[0].ProviderMetadata.Model)
	}
	if len(events[0].ProviderMetadata.Tools) != 3 {
		t.Fatalf("expected provider tool list size 3, got %d", len(events[0].ProviderMetadata.Tools))
	}
}
