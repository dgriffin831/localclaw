package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dgriffin831/localclaw/internal/llm"
	"github.com/dgriffin831/localclaw/internal/memory"
	"github.com/dgriffin831/localclaw/internal/runtime"
)

func (m *model) submitInput() tea.Cmd {
	value := strings.TrimSpace(m.input.Value())
	if value == "" {
		return nil
	}
	if m.running {
		m.addSystem("run already in progress; press Esc to abort")
		m.refreshViewport(true)
		return nil
	}

	m.rememberHistory(value)
	m.input.Reset()
	m.updateSlashAutocomplete()
	m.adjustInputHeight()

	if strings.HasPrefix(value, "/") {
		return m.handleSlash(value)
	}
	m.startRun(value)
	m.refreshViewport(true)

	cmds := []tea.Cmd{m.spinner.Tick, tickStatus()}
	if m.streamEvents != nil {
		cmds = append(cmds, waitStreamEvent(m.activeRunID, m.streamEvents))
	}
	if m.streamErrs != nil {
		cmds = append(cmds, waitStreamErr(m.activeRunID, m.streamErrs))
	}
	return tea.Batch(cmds...)
}

func (m *model) startRun(input string) {
	resetToolCallOwnershipByID(m.toolCallOwnershipByID)
	m.runSeq++
	m.activeRunID = m.runSeq
	m.running = true
	m.hasStreamDelta = false
	m.streamDeltaEvents = 0
	m.streamDeltaChars = 0
	m.activeAssistantIdx = -1
	m.activeThinkingMessage = m.nextThinkingMessage()
	m.setStatus(statusSending)
	m.emitVerboseRunStartDiagnostics(input)

	m.addUser(input)
	if m.app == nil {
		m.addSystem("runtime unavailable")
		m.finishRun(statusError)
		return
	}

	userTokenDelta := memory.EstimateTokensFromText(input)
	if m.app != nil {
		if err := m.app.AddSessionTokens(m.ctx, m.agentID, m.sessionID, userTokenDelta); err != nil {
			m.addVerbose("transcript write: role=user token_update_error=%s", truncateVerboseText(err.Error()))
		} else {
			m.sessionTokens += userTokenDelta
		}
		if err := m.app.AppendSessionTranscriptMessage(m.ctx, m.agentID, m.sessionID, "user", input); err != nil {
			m.addVerbose("transcript write: role=user append_error=%s", truncateVerboseText(err.Error()))
		} else {
			m.addVerbose("transcript write: role=user chars=%d tokens=%d", len(strings.TrimSpace(input)), userTokenDelta)
		}
		m.app.RunMemoryFlushIfNeededAsync(m.ctx, m.agentID, m.sessionID)
		m.addVerbose("runtime: memory flush check queued")
	}

	runCtx, cancel := context.WithCancel(m.ctx)
	m.runCancel = cancel
	opts := llm.PromptOptions{
		ProviderOverride:  strings.TrimSpace(m.providerOverride),
		ModelOverride:     strings.TrimSpace(m.modelOverride),
		ReasoningOverride: strings.TrimSpace(m.reasoningOverride),
	}
	m.streamEvents, m.streamErrs = m.app.PromptStreamForSessionWithOptions(runCtx, m.agentID, m.sessionID, input, opts)
	m.setStatus(statusWaiting)
}

func (m *model) finishRun(finalStatus string) {
	m.running = false
	m.setStatus(finalStatus)
	m.activeThinkingMessage = ""
	resetToolCallOwnershipByID(m.toolCallOwnershipByID)
	resetToolCardIndexByCallID(m.toolCardIndexByCallID)
	m.activeRunID = 0
	m.activeAssistantIdx = -1
	m.streamEvents = nil
	m.streamErrs = nil
	if m.runCancel != nil {
		m.runCancel()
		m.runCancel = nil
	}
}

func (m *model) abortRun(message string) {
	if m.runCancel != nil {
		m.runCancel()
		m.runCancel = nil
	}
	resetToolCallOwnershipByID(m.toolCallOwnershipByID)
	resetToolCardIndexByCallID(m.toolCardIndexByCallID)
	if m.running {
		m.running = false
		m.activeRunID = 0
		m.activeAssistantIdx = -1
		m.activeThinkingMessage = ""
		m.streamEvents = nil
		m.streamErrs = nil
		m.setStatus(statusAborted)
	}
	if strings.TrimSpace(message) != "" {
		m.setStatus(message)
	}
}

func (m *model) applyDelta(chunk string) {
	if m.activeAssistantIdx < 0 || m.activeAssistantIdx >= len(m.messages) {
		m.addAssistant("", false)
	}
	msg := &m.messages[m.activeAssistantIdx]
	if msg.ThinkingPlaceholder {
		msg.Raw = ""
		msg.ThinkingPlaceholder = false
	}
	msg.Raw += chunk
	msg.Streaming = true
}

func (m *model) applyFinal(final string) {
	if m.activeAssistantIdx < 0 || m.activeAssistantIdx >= len(m.messages) {
		m.addAssistant("", false)
	}
	msg := &m.messages[m.activeAssistantIdx]
	trimmed := strings.TrimSpace(final)
	if trimmed != "" {
		msg.Raw = trimmed
	} else if strings.TrimSpace(msg.Raw) == "" {
		msg.Raw = "(no output)"
	}
	if m.app != nil {
		assistantTokenDelta := memory.EstimateTokensFromText(msg.Raw)
		if err := m.app.AddSessionTokens(m.ctx, m.agentID, m.sessionID, assistantTokenDelta); err != nil {
			m.addVerbose("transcript write: role=assistant token_update_error=%s", truncateVerboseText(err.Error()))
		} else {
			m.sessionTokens += assistantTokenDelta
		}
		if err := m.app.AppendSessionTranscriptMessage(m.ctx, m.agentID, m.sessionID, "assistant", msg.Raw); err != nil {
			m.addVerbose("transcript write: role=assistant append_error=%s", truncateVerboseText(err.Error()))
		} else {
			m.addVerbose("transcript write: role=assistant chars=%d tokens=%d", len(strings.TrimSpace(msg.Raw)), assistantTokenDelta)
		}
	}
	msg.Streaming = false
	msg.ThinkingPlaceholder = false
}

func (m *model) runSessionReset(startNew bool, source string) {
	m.abortRun("")
	if m.app != nil {
		next := m.app.ResetSession(m.ctx, runtime.ResetSessionRequest{
			AgentID:   m.agentID,
			SessionID: m.sessionID,
			Source:    source,
			StartNew:  startNew,
		})
		m.agentID = next.AgentID
		m.sessionID = next.SessionID
		m.sessionKey = next.SessionKey
		m.syncSessionMetadata()
	} else if startNew {
		m.sessionTokens = 0
	}
	m.messages = nil
	m.providerOverride = ""
	m.modelOverride = ""
	m.reasoningOverride = ""
	m.providerTools = nil
	m.providerToolsDiscoveryInFlight = false
	resetToolCardIndexByCallID(m.toolCardIndexByCallID)
	if startNew {
		m.addSystem(fmt.Sprintf("started new session %s", m.sessionID))
		if welcome := m.loadWelcomeMessage(); welcome != "" {
			m.addSystemMarkdown(welcome)
		}
	} else {
		m.addSystem("session reset")
	}
}
