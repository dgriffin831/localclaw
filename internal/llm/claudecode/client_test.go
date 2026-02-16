package claudecode

import (
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
	content, _ := events[0].ToolResult.Data["content"].(string)
	if content != "AGENTS.md" {
		t.Fatalf("expected tool content AGENTS.md, got %q", content)
	}
}

func TestParseStreamJSONLineToolResultError(t *testing.T) {
	toolNames := map[string]string{}
	line := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_404","content":"permission denied","is_error":true}]}}`
	events, resultErr, err := parseStreamJSONLine(line, toolNames)
	if err != nil {
		t.Fatalf("parse line: %v", err)
	}
	if resultErr != "" {
		t.Fatalf("unexpected result error: %q", resultErr)
	}
	if len(events) != 1 {
		t.Fatalf("expected one tool_result event, got %d", len(events))
	}
	if events[0].ToolResult == nil {
		t.Fatalf("expected tool result payload")
	}
	if events[0].ToolResult.OK {
		t.Fatalf("expected failing tool result")
	}
	if events[0].ToolResult.Tool != "tool" {
		t.Fatalf("expected fallback tool name tool, got %q", events[0].ToolResult.Tool)
	}
	if events[0].ToolResult.Error != "permission denied" {
		t.Fatalf("expected tool error text, got %q", events[0].ToolResult.Error)
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
