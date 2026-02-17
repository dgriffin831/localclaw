package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/llm"
)

func TestVerboseRunStartDiagnosticsShowPromptAndRuntimeState(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.verbose = true

	m.emitVerboseRunStartDiagnostics("hello\nworld")
	all := joinedMessageRaw(m.messages)

	if !strings.Contains(all, "[verbose] prompt: chars=11 lines=2 session=default/main") {
		t.Fatalf("expected verbose prompt summary, got %q", all)
	}
	if !strings.Contains(all, "[verbose] runtime: unavailable") {
		t.Fatalf("expected verbose runtime summary, got %q", all)
	}
}

func TestVerboseStreamSummaryIncludesDeltaAndFinalStats(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.verbose = true
	m.running = true
	m.activeRunID = 17
	m.status = statusWaiting

	updated, _ := m.Update(streamEventMsg{
		RunID: 17,
		OK:    true,
		Event: llm.StreamEvent{Type: llm.StreamEventDelta, Text: "abc"},
	})
	m = updated.(model)
	updated, _ = m.Update(streamEventMsg{
		RunID: 17,
		OK:    true,
		Event: llm.StreamEvent{Type: llm.StreamEventFinal, Text: "done"},
	})
	m = updated.(model)

	all := joinedMessageRaw(m.messages)
	if !strings.Contains(all, "[verbose] stream: first delta received") {
		t.Fatalf("expected verbose first-delta message, got %q", all)
	}
	if !strings.Contains(all, "[verbose] stream: final received") {
		t.Fatalf("expected verbose final summary, got %q", all)
	}
	if !strings.Contains(all, "delta_events=1") || !strings.Contains(all, "delta_chars=3") {
		t.Fatalf("expected verbose stream counters, got %q", all)
	}
}

func TestVerboseToolCallShowsDetailedMetadata(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.verbose = true
	m.running = true
	m.activeRunID = 7
	m.status = statusWaiting

	updated, _ := m.Update(streamEventMsg{
		RunID: 7,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventToolCall,
			ToolCall: &llm.ToolCall{
				ID:    "call-1",
				Name:  "memory_search",
				Class: llm.ToolClassLocal,
				Args: map[string]interface{}{
					"query":       "incident summary",
					"max_results": 3,
				},
			},
		},
	})
	m = updated.(model)

	all := joinedMessageRaw(m.messages)
	if !strings.Contains(all, "[verbose] tool call details: id=call-1 class=local") {
		t.Fatalf("expected verbose tool call details, got %q", all)
	}
	if !strings.Contains(all, "query=incident summary") || !strings.Contains(all, "max_results=3") {
		t.Fatalf("expected verbose tool args summary, got %q", all)
	}
}

func TestVerboseToolResultShowsDetailedMetadata(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.verbose = true
	m.running = true
	m.activeRunID = 7
	m.status = "tool memory_search"

	updated, _ := m.Update(streamEventMsg{
		RunID: 7,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventToolResult,
			ToolResult: &llm.ToolResult{
				CallID: "call-1",
				Tool:   "memory_search",
				OK:     true,
				Status: "completed",
				Data: map[string]interface{}{
					"count": 2,
				},
			},
		},
	})
	m = updated.(model)

	all := joinedMessageRaw(m.messages)
	if !strings.Contains(all, "[verbose] tool result details: call_id=call-1 tool=memory_search ok=true status=completed") {
		t.Fatalf("expected verbose tool result details, got %q", all)
	}
	if !strings.Contains(all, "data_keys=count") {
		t.Fatalf("expected verbose tool result data key summary, got %q", all)
	}
}

func TestVerboseProviderMetadataShowsDetails(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.verbose = true
	m.running = true
	m.activeRunID = 22

	updated, _ := m.Update(streamEventMsg{
		RunID: 22,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventProviderMetadata,
			ProviderMetadata: &llm.ProviderMetadata{
				Provider: "claudecode",
				Model:    "claude-opus-4-6",
				Tools:    []string{"Bash", "WebFetch"},
			},
		},
	})
	m = updated.(model)

	all := joinedMessageRaw(m.messages)
	if !strings.Contains(all, "[verbose] provider metadata: provider=claudecode model=claude-opus-4-6 tools=Bash, WebFetch") {
		t.Fatalf("expected verbose provider metadata summary, got %q", all)
	}
}

func TestResolveThinkingMessagesFallsBackToThinking(t *testing.T) {
	got := resolveThinkingMessages(nil)
	if len(got) != 1 || got[0] != "thinking" {
		t.Fatalf("expected fallback thinking messages [thinking], got %v", got)
	}
}

func TestNextThinkingMessageRotatesConfiguredMessages(t *testing.T) {
	cfg := config.Default()
	cfg.App.ThinkingMessages = []string{"thinking", "checking memory"}
	m := newModel(context.Background(), nil, cfg)

	if got := m.nextThinkingMessage(); got != "thinking" {
		t.Fatalf("expected first message thinking, got %q", got)
	}
	if got := m.nextThinkingMessage(); got != "checking memory" {
		t.Fatalf("expected second message checking memory, got %q", got)
	}
	if got := m.nextThinkingMessage(); got != "thinking" {
		t.Fatalf("expected third message to wrap to thinking, got %q", got)
	}
}

func TestStatusViewShowsCurrentThinkingMessageWhileWaiting(t *testing.T) {
	cfg := config.Default()
	cfg.App.ThinkingMessages = []string{"checking memory"}
	m := newModel(context.Background(), nil, cfg)
	m.width = 120
	m.status = statusWaiting
	m.running = true
	m.hasStreamDelta = false
	m.activeThinkingMessage = "checking memory"

	got := m.statusView()
	if !strings.Contains(got, "checking memory") {
		t.Fatalf("expected status line to include current thinking message, got %q", got)
	}
}

func TestToolCallEventSurfacesToolActivity(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.running = true
	m.activeRunID = 7
	m.status = statusWaiting

	updated, _ := m.Update(streamEventMsg{
		RunID: 7,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventToolCall,
			ToolCall: &llm.ToolCall{
				Name:  "memory_search",
				Class: llm.ToolClassLocal,
			},
		},
	})
	next := updated.(model)

	if !strings.Contains(next.status, "tool") {
		t.Fatalf("expected status to reflect tool activity, got %q", next.status)
	}
	if got := countToolCards(next.messages); got != 1 {
		t.Fatalf("expected tool call event to append one tool card, got %d", got)
	}
	card := latestToolCard(next.messages)
	if card == nil {
		t.Fatalf("expected tool card payload after tool call")
	}
	if card.ToolName != "memory_search" {
		t.Fatalf("expected tool card tool name memory_search, got %q", card.ToolName)
	}
	if card.Ownership != "localclaw_mcp" {
		t.Fatalf("expected tool card ownership localclaw_mcp, got %q", card.Ownership)
	}
	if card.HasResult {
		t.Fatalf("expected tool call card to remain running before result")
	}
	rendered := ansiEscapePattern.ReplaceAllString(next.renderTranscript(), "")
	if !strings.Contains(rendered, "tool [localclaw_mcp] memory_search") {
		t.Fatalf("expected rendered card summary, got %q", rendered)
	}
	if !strings.Contains(rendered, "running") {
		t.Fatalf("expected running status in rendered card, got %q", rendered)
	}
}

func TestToolResultEventReturnsToWaitingState(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.running = true
	m.activeRunID = 7
	m.status = "tool memory_search"

	updated, _ := m.Update(streamEventMsg{
		RunID: 7,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventToolResult,
			ToolResult: &llm.ToolResult{
				Tool:  "memory_search",
				Class: llm.ToolClassDelegated,
				OK:    true,
			},
		},
	})
	next := updated.(model)

	if next.status != statusWaiting {
		t.Fatalf("expected tool result to return status to waiting, got %q", next.status)
	}
	if got := countToolCards(next.messages); got != 1 {
		t.Fatalf("expected tool result event to append one tool card, got %d", got)
	}
	card := latestToolCard(next.messages)
	if card == nil {
		t.Fatalf("expected tool card payload after tool result")
	}
	if card.ToolName != "memory_search" {
		t.Fatalf("expected tool card tool name memory_search, got %q", card.ToolName)
	}
	if card.Ownership != "provider_native" {
		t.Fatalf("expected tool card ownership provider_native, got %q", card.Ownership)
	}
	if !card.HasResult {
		t.Fatalf("expected result card to be marked complete")
	}
	rendered := ansiEscapePattern.ReplaceAllString(next.renderTranscript(), "")
	if !strings.Contains(rendered, "completed") {
		t.Fatalf("expected completed status in rendered card, got %q", rendered)
	}
}

func TestToolResultEventUsesCallOwnershipWhenResultClassMissing(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.running = true
	m.activeRunID = 7
	m.status = statusWaiting

	updated, _ := m.Update(streamEventMsg{
		RunID: 7,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventToolCall,
			ToolCall: &llm.ToolCall{
				ID:    "call-123",
				Name:  "Bash",
				Class: llm.ToolClassDelegated,
			},
		},
	})
	m = updated.(model)

	updated, _ = m.Update(streamEventMsg{
		RunID: 7,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventToolResult,
			ToolResult: &llm.ToolResult{
				CallID: "call-123",
				Tool:   "Bash",
				OK:     true,
			},
		},
	})
	next := updated.(model)
	if got := countToolCards(next.messages); got != 1 {
		t.Fatalf("expected call/result pair to render as one card, got %d cards", got)
	}
	card := latestToolCard(next.messages)
	if card == nil {
		t.Fatalf("expected tool card payload after call/result pair")
	}
	if card.Ownership != "provider_native" {
		t.Fatalf("expected ownership to resolve from prior call id, got %q", card.Ownership)
	}
}

func TestToolResultEventWithoutOwnershipShowsUnspecified(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.running = true
	m.activeRunID = 7
	m.status = statusWaiting

	updated, _ := m.Update(streamEventMsg{
		RunID: 7,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventToolResult,
			ToolResult: &llm.ToolResult{
				Tool: "mystery_tool",
				OK:   true,
			},
		},
	})
	next := updated.(model)
	card := latestToolCard(next.messages)
	if card == nil {
		t.Fatalf("expected tool card payload for ownership fallback")
	}
	if card.Ownership != "unspecified" {
		t.Fatalf("expected unknown ownership label, got %q", card.Ownership)
	}
}

func TestToolCardsExpandWithCtrlO(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.running = true
	m.activeRunID = 7
	m.status = statusWaiting

	updated, _ := m.Update(streamEventMsg{
		RunID: 7,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventToolCall,
			ToolCall: &llm.ToolCall{
				ID:    "call-1",
				Name:  "memory_search",
				Class: llm.ToolClassLocal,
				Args: map[string]interface{}{
					"query": "incident summary",
				},
			},
		},
	})
	m = updated.(model)
	updated, _ = m.Update(streamEventMsg{
		RunID: 7,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventToolResult,
			ToolResult: &llm.ToolResult{
				CallID: "call-1",
				Tool:   "memory_search",
				Class:  llm.ToolClassLocal,
				OK:     true,
				Status: "completed",
				Data: map[string]interface{}{
					"count": 2,
				},
			},
		},
	})
	m = updated.(model)

	collapsed := ansiEscapePattern.ReplaceAllString(m.renderTranscript(), "")
	if strings.Contains(collapsed, "arg.query: incident summary") {
		t.Fatalf("expected collapsed tool card to omit args, got %q", collapsed)
	}
	if strings.Contains(collapsed, "data.count: 2") {
		t.Fatalf("expected collapsed tool card to omit result data, got %q", collapsed)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	next := updated.(model)
	if !next.toolsExpanded {
		t.Fatalf("expected ctrl+o to expand tool cards")
	}
	expanded := ansiEscapePattern.ReplaceAllString(next.renderTranscript(), "")
	if !strings.Contains(expanded, "arg.query: incident summary") {
		t.Fatalf("expected expanded tool card to show args, got %q", expanded)
	}
	if !strings.Contains(expanded, "data.count: 2") {
		t.Fatalf("expected expanded tool card to show result data, got %q", expanded)
	}
}

func TestExpandedToolCardFormatsContentAndHidesProviderResult(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.running = true
	m.activeRunID = 7
	m.status = statusWaiting
	m.toolsExpanded = true

	longTail := strings.Repeat("x", 220) + "TAIL_MARKER"
	contentJSON := `{"count":1,"ok":true,"results":[{"snippet":"` + longTail + `"}]}`

	updated, _ := m.Update(streamEventMsg{
		RunID: 7,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventToolCall,
			ToolCall: &llm.ToolCall{
				ID:    "call-content",
				Name:  "memory_search",
				Class: llm.ToolClassLocal,
			},
		},
	})
	m = updated.(model)

	updated, _ = m.Update(streamEventMsg{
		RunID: 7,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventToolResult,
			ToolResult: &llm.ToolResult{
				CallID: "call-content",
				Tool:   "memory_search",
				Class:  llm.ToolClassLocal,
				OK:     true,
				Status: "completed",
				Data: map[string]interface{}{
					"content":         contentJSON,
					"provider_result": map[string]interface{}{"content": contentJSON},
				},
			},
		},
	})
	next := updated.(model)

	rendered := ansiEscapePattern.ReplaceAllString(next.renderTranscript(), "")
	if strings.Contains(rendered, "data.provider_result:") {
		t.Fatalf("expected expanded card to hide provider_result, got %q", rendered)
	}
	if !strings.Contains(rendered, "data.content:") || !strings.Contains(rendered, "```json") {
		t.Fatalf("expected expanded card to render content as fenced JSON block, got %q", rendered)
	}
	if !strings.Contains(rendered, "\"count\": 1") {
		t.Fatalf("expected expanded card to pretty-print JSON content, got %q", rendered)
	}
	if !strings.Contains(rendered, "TAIL_MARKER") {
		t.Fatalf("expected expanded card content to avoid truncation, got %q", rendered)
	}
}

func TestExpandedToolCardShowsErrorForFailedResult(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.running = true
	m.activeRunID = 7
	m.status = statusWaiting
	m.toolsExpanded = true

	updated, _ := m.Update(streamEventMsg{
		RunID: 7,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventToolCall,
			ToolCall: &llm.ToolCall{
				ID:    "call-err",
				Name:  "Bash",
				Class: llm.ToolClassDelegated,
				Args: map[string]interface{}{
					"command": "ls denied",
				},
			},
		},
	})
	m = updated.(model)

	updated, _ = m.Update(streamEventMsg{
		RunID: 7,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventToolResult,
			ToolResult: &llm.ToolResult{
				CallID: "call-err",
				Tool:   "Bash",
				Class:  llm.ToolClassDelegated,
				OK:     false,
				Status: "error",
				Error:  "permission denied",
			},
		},
	})
	next := updated.(model)

	rendered := ansiEscapePattern.ReplaceAllString(next.renderTranscript(), "")
	if !strings.Contains(rendered, "tool [provider_native] Bash • failed") {
		t.Fatalf("expected failed summary in expanded card, got %q", rendered)
	}
	if !strings.Contains(rendered, "error: permission denied") {
		t.Fatalf("expected expanded card error text, got %q", rendered)
	}
}

func TestFinishRunClearsToolCallOwnershipCache(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.toolCallOwnershipByID["call-123"] = llm.ToolClassDelegated

	m.finishRun(statusIdle)

	if len(m.toolCallOwnershipByID) != 0 {
		t.Fatalf("expected finishRun to clear call ownership cache")
	}
}

func TestAbortRunClearsToolCallOwnershipCache(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.toolCallOwnershipByID["call-123"] = llm.ToolClassDelegated

	m.abortRun("aborted")

	if len(m.toolCallOwnershipByID) != 0 {
		t.Fatalf("expected abortRun to clear call ownership cache")
	}
}

func TestNewProgramSkipsMouseCellMotionByDefault(t *testing.T) {
	got := startupOptionBits(t, newProgram(newModel(context.Background(), nil, config.Default())))

	expectedProgram := tea.NewProgram(nil)
	tea.WithAltScreen()(expectedProgram)
	expected := startupOptionBits(t, expectedProgram)

	if got != expected {
		t.Fatalf("unexpected startup options: got=%d want=%d", got, expected)
	}
}

func TestNewProgramEnablesMouseCellMotionWhenConfiguredOn(t *testing.T) {
	cfg := config.Default()
	cfg.App.Default.Mouse = true

	got := startupOptionBits(t, newProgram(newModel(context.Background(), nil, cfg)))

	expectedProgram := tea.NewProgram(nil)
	tea.WithAltScreen()(expectedProgram)
	tea.WithMouseCellMotion()(expectedProgram)
	expected := startupOptionBits(t, expectedProgram)

	if got != expected {
		t.Fatalf("unexpected startup options: got=%d want=%d", got, expected)
	}
}
