package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

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
				ID:   "call-1",
				Name: "memory_search",
				Args: map[string]interface{}{
					"query":       "incident summary",
					"max_results": 3,
				},
			},
		},
	})
	m = updated.(model)

	all := joinedMessageRaw(m.messages)
	if !strings.Contains(all, "[verbose] tool call details: id=call-1") {
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
				Name: "memory_search",
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
	if card.HasResult {
		t.Fatalf("expected tool call card to remain running before result")
	}
	rendered := ansiEscapePattern.ReplaceAllString(next.renderTranscript(), "")
	if !strings.Contains(rendered, "tool memory_search") {
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
				Tool: "memory_search",
				OK:   true,
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
	if !card.HasResult {
		t.Fatalf("expected result card to be marked complete")
	}
	rendered := ansiEscapePattern.ReplaceAllString(next.renderTranscript(), "")
	if !strings.Contains(rendered, "completed") {
		t.Fatalf("expected completed status in rendered card, got %q", rendered)
	}
}

func TestToolResultEventPairsByCallIDWhenResultClassMissing(t *testing.T) {
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
				ID:   "call-123",
				Name: "Bash",
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
	if card.CallID != "call-123" {
		t.Fatalf("expected call/result pair to retain call id, got %q", card.CallID)
	}
}

func TestToolResultEventWithoutCallIDStillRendersCard(t *testing.T) {
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
		t.Fatalf("expected tool card payload without call id")
	}
}

func TestFinalResponsePreservesObservedOrderWithToolCards(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.running = true
	m.activeRunID = 7
	m.status = statusWaiting

	updated, _ := m.Update(streamEventMsg{
		RunID: 7,
		OK:    true,
		Event: llm.StreamEvent{Type: llm.StreamEventDelta, Text: "draft"},
	})
	m = updated.(model)
	updated, _ = m.Update(streamEventMsg{
		RunID: 7,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventToolCall,
			ToolCall: &llm.ToolCall{
				ID:   "call-1",
				Name: "memory_search",
			},
		},
	})
	m = updated.(model)
	updated, _ = m.Update(streamEventMsg{
		RunID: 7,
		OK:    true,
		Event: llm.StreamEvent{Type: llm.StreamEventFinal, Text: "final response"},
	})
	m = updated.(model)

	if len(m.messages) < 2 {
		t.Fatalf("expected assistant message and tool card messages, got %d", len(m.messages))
	}
	assistantIdx := -1
	toolIdx := -1
	for idx, msg := range m.messages {
		if assistantIdx == -1 && msg.Role == roleAssistant {
			assistantIdx = idx
		}
		if toolIdx == -1 && msg.ToolCard != nil {
			toolIdx = idx
		}
	}
	if assistantIdx == -1 {
		t.Fatalf("expected assistant update to be present in transcript")
	}
	if toolIdx == -1 {
		t.Fatalf("expected tool card to be present in transcript")
	}
	if assistantIdx >= toolIdx {
		t.Fatalf("expected assistant update to remain before later tool card (assistant=%d tool=%d)", assistantIdx, toolIdx)
	}
	if m.messages[assistantIdx].Raw != "final response" {
		t.Fatalf("expected final assistant text to be preserved in-place, got %q", m.messages[assistantIdx].Raw)
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
				ID:   "call-1",
				Name: "memory_search",
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
	if !strings.Contains(collapsed, "tool memory_search • args: query=incident summary • completed") {
		t.Fatalf("expected collapsed tool card summary to include key args, got %q", collapsed)
	}
	if strings.Contains(collapsed, "arg.query: incident summary") {
		t.Fatalf("expected collapsed tool card to omit expanded arg rows, got %q", collapsed)
	}
	if strings.Contains(collapsed, "data.count: 2") {
		t.Fatalf("expected collapsed tool card to omit result data, got %q", collapsed)
	}

	updated, _ = m.Update(keyCtrl('o'))
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

func TestToolResultBackfillsEmptyArgValuesFromResultData(t *testing.T) {
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
				ID:   "ws-1",
				Name: "web_search",
				Args: map[string]interface{}{
					"query":  "",
					"action": map[string]interface{}{"type": "other"},
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
				CallID: "ws-1",
				Tool:   "web_search",
				OK:     true,
				Status: "completed",
				Data: map[string]interface{}{
					"query": "site:github.blog February 2026 GitHub blog announcements",
					"action": map[string]interface{}{
						"type":  "search",
						"query": "site:github.blog February 2026 GitHub blog announcements",
					},
				},
			},
		},
	})
	next := updated.(model)

	rendered := ansiEscapePattern.ReplaceAllString(next.renderTranscript(), "")
	if strings.Contains(rendered, "arg.query: (empty)") {
		t.Fatalf("expected empty arg.query to be reconciled from result data, got %q", rendered)
	}
	if !strings.Contains(rendered, "arg.query: site:github.blog February 2026 GitHub blog announcements") {
		t.Fatalf("expected reconciled arg.query in expanded card, got %q", rendered)
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
				ID:   "call-content",
				Name: "memory_search",
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

func TestExpandedToolCardFormatsStructuredValuesAndDedupesResultMetadata(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.running = true
	m.activeRunID = 7
	m.status = statusWaiting
	m.toolsExpanded = true

	filter := map[string]interface{}{
		"tags": []interface{}{"security", "incident"},
	}
	updated, _ := m.Update(streamEventMsg{
		RunID: 7,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventToolCall,
			ToolCall: &llm.ToolCall{
				ID:   "call-structured",
				Name: "web_search",
				Args: map[string]interface{}{
					"query":   "OpenClaw news",
					"filters": filter,
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
				CallID: "call-structured",
				Tool:   "web_search",
				OK:     true,
				Status: "completed",
				Data: map[string]interface{}{
					"query":   "OpenClaw news",
					"filters": filter,
					"items": []interface{}{
						map[string]interface{}{
							"title":   "OpenClaw",
							"snippet": strings.Repeat("x", 220) + "TAIL_MARKER",
						},
					},
				},
			},
		},
	})
	next := updated.(model)

	rendered := ansiEscapePattern.ReplaceAllString(next.renderTranscript(), "")
	if !strings.Contains(rendered, "arg.filters:") || !strings.Contains(rendered, "\"tags\": [") {
		t.Fatalf("expected structured arg values to render as pretty JSON block, got %q", rendered)
	}
	if strings.Contains(rendered, "data.query: OpenClaw news") {
		t.Fatalf("expected duplicated data.query to be omitted when shown in args, got %q", rendered)
	}
	if strings.Contains(rendered, "data.filters:") {
		t.Fatalf("expected duplicated data.filters to be omitted when shown in args, got %q", rendered)
	}
	if !strings.Contains(rendered, "data.items:") || !strings.Contains(rendered, "```json") {
		t.Fatalf("expected structured result values to render as pretty JSON block, got %q", rendered)
	}
	if strings.Contains(rendered, "map[") {
		t.Fatalf("expected expanded card to avoid one-line map[...] rendering, got %q", rendered)
	}
	if !strings.Contains(rendered, "TAIL_MARKER") {
		t.Fatalf("expected structured result block to avoid truncation, got %q", rendered)
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
				ID:   "call-err",
				Name: "Bash",
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
				OK:     false,
				Status: "error",
				Error:  "permission denied",
			},
		},
	})
	next := updated.(model)

	rendered := ansiEscapePattern.ReplaceAllString(next.renderTranscript(), "")
	if !strings.Contains(rendered, "tool Bash • failed") {
		t.Fatalf("expected failed summary in expanded card, got %q", rendered)
	}
	if !strings.Contains(rendered, "error: permission denied") {
		t.Fatalf("expected expanded card error text, got %q", rendered)
	}
}

func TestFinishRunClearsToolCardCallIDIndex(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.toolCardIndexByCallID["call-123"] = 1

	m.finishRun(statusIdle)

	if len(m.toolCardIndexByCallID) != 0 {
		t.Fatalf("expected finishRun to clear tool card index cache")
	}
}

func TestAbortRunClearsToolCardCallIDIndex(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.toolCardIndexByCallID["call-123"] = 1

	m.abortRun("aborted")

	if len(m.toolCardIndexByCallID) != 0 {
		t.Fatalf("expected abortRun to clear tool card index cache")
	}
}

func TestViewAlwaysEnablesAltScreen(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	view := m.View()
	if !view.AltScreen {
		t.Fatalf("expected view to request alt screen")
	}
}

func TestViewUsesMouseCellMotion(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	view := m.View()
	if got := view.MouseMode; got != tea.MouseModeCellMotion {
		t.Fatalf("expected view to request cell-motion mouse mode, got %v", got)
	}
}
