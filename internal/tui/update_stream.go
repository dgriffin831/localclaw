package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/dgriffin831/localclaw/internal/llm"
)

func (m *model) handleSpinnerTick(msg spinner.TickMsg) tea.Cmd {
	if m.isBusy() {
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return cmd
	}
	return nil
}

func (m *model) handleStreamEvent(event llm.StreamEvent) {
	switch event.Type {
	case llm.StreamEventDelta:
		m.streamDeltaEvents++
		m.streamDeltaChars += len(event.Text)
		if m.streamDeltaEvents == 1 {
			m.addVerbose("stream: first delta received")
		}
		m.setStatus(statusStreaming)
		m.hasStreamDelta = true
		m.applyDelta(event.Text)
		m.refreshViewport(true)
	case llm.StreamEventFinal:
		m.applyFinal(event.Text)
		m.addVerbose("stream: final received delta_events=%d delta_chars=%d final_chars=%d", m.streamDeltaEvents, m.streamDeltaChars, len(strings.TrimSpace(event.Text)))
		m.finishRun(statusIdle)
		m.refreshViewport(true)
	case llm.StreamEventToolCall:
		toolName := "tool"
		toolClass := llm.ToolClassUnspecified
		callID := ""
		args := map[string]interface{}{}
		if event.ToolCall != nil && strings.TrimSpace(event.ToolCall.Name) != "" {
			toolName = event.ToolCall.Name
		}
		if event.ToolCall != nil {
			toolClass = event.ToolCall.Class
			class := string(event.ToolCall.Class)
			if class == "" {
				class = "unspecified"
			}
			callID = strings.TrimSpace(event.ToolCall.ID)
			if callID != "" {
				m.toolCallOwnershipByID[callID] = toolClass
			}
			displayCallID := callID
			if displayCallID == "" {
				displayCallID = "n/a"
			}
			args = copyInterfaceMap(event.ToolCall.Args)
			m.addVerbose("tool call details: id=%s class=%s args=%s", displayCallID, class, summarizeVerboseMap(event.ToolCall.Args))
		}
		ownership := toolOwnershipLabel(toolClass)
		m.setStatus(fmt.Sprintf("tool [%s] %s", ownership, toolName))
		m.recordToolCallCard(callID, toolName, ownership, args)
		m.refreshViewport(true)
	case llm.StreamEventToolResult:
		if event.ToolResult != nil {
			toolName := event.ToolResult.Tool
			toolClass := event.ToolResult.Class
			callID := strings.TrimSpace(event.ToolResult.CallID)
			if toolClass == llm.ToolClassUnspecified && callID != "" {
				toolClass = m.toolCallOwnershipByID[callID]
			}
			if callID != "" {
				delete(m.toolCallOwnershipByID, callID)
			}
			if toolName == "" && event.ToolCall != nil {
				toolName = event.ToolCall.Name
			}
			ownership := toolOwnershipLabel(toolClass)
			m.recordToolResultCard(callID, toolName, ownership, event.ToolResult)
			if callID == "" {
				callID = "n/a"
			}
			status := strings.TrimSpace(event.ToolResult.Status)
			if status == "" {
				status = "n/a"
			}
			errText := strings.TrimSpace(event.ToolResult.Error)
			if errText == "" {
				errText = "none"
			}
			m.addVerbose("tool result details: call_id=%s tool=%s ok=%t status=%s error=%s data_keys=%s", callID, toolName, event.ToolResult.OK, status, truncateVerboseText(errText), summarizeVerboseKeys(event.ToolResult.Data))
		}
		if m.running {
			m.setStatus(statusWaiting)
		}
		m.refreshViewport(true)
	case llm.StreamEventProviderMetadata:
		if event.ProviderMetadata != nil {
			metadata := event.ProviderMetadata
			if provider := strings.TrimSpace(metadata.Provider); provider != "" {
				m.providerName = provider
			}
			if model := strings.TrimSpace(metadata.Model); model != "" {
				m.providerModel = model
			}
			m.providerTools = normalizeProviderToolList(metadata.Tools)
			m.addVerbose("provider metadata: provider=%s model=%s tools=%s", valueOrDefault(strings.TrimSpace(m.providerName), "n/a"), valueOrDefault(strings.TrimSpace(m.providerModel), "n/a"), summarizeVerboseList(m.providerTools))
		}
	}
}

func waitStreamEvent(runID int, ch <-chan llm.StreamEvent) tea.Cmd {
	return func() tea.Msg {
		evt, ok := <-ch
		return streamEventMsg{RunID: runID, Event: evt, OK: ok}
	}
}

func waitStreamErr(runID int, ch <-chan error) tea.Cmd {
	return func() tea.Msg {
		err, ok := <-ch
		return streamErrMsg{RunID: runID, Err: err, OK: ok}
	}
}

func tickStatus() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return statusTickMsg(t)
	})
}

func resolveThinkingMessages(messages []string) []string {
	resolved := make([]string, 0, len(messages))
	for _, message := range messages {
		trimmed := strings.TrimSpace(message)
		if trimmed == "" {
			continue
		}
		resolved = append(resolved, trimmed)
	}
	if len(resolved) == 0 {
		return []string{"thinking"}
	}
	return resolved
}

func (m *model) nextThinkingMessage() string {
	if len(m.thinkingMessages) == 0 {
		return "thinking"
	}
	idx := m.thinkingMessageIdx % len(m.thinkingMessages)
	message := m.thinkingMessages[idx]
	m.thinkingMessageIdx = (m.thinkingMessageIdx + 1) % len(m.thinkingMessages)
	return message
}

func (m *model) currentThinkingMessage() string {
	if strings.TrimSpace(m.activeThinkingMessage) != "" {
		return m.activeThinkingMessage
	}
	if len(m.thinkingMessages) > 0 {
		return m.thinkingMessages[0]
	}
	return "thinking"
}

func (m *model) isBusy() bool {
	return m.status == statusSending || m.status == statusWaiting || m.status == statusStreaming
}

func (m *model) setStatus(next string) {
	if next == "" {
		return
	}
	if next != m.status {
		m.status = next
	}
	if m.isBusy() {
		if m.statusStartedAt.IsZero() {
			m.statusStartedAt = time.Now()
		}
		return
	}
	m.statusStartedAt = time.Time{}
}
