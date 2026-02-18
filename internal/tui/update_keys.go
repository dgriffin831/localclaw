package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

func (m *model) handleKeyMsg(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+c"))) {
		if strings.TrimSpace(m.input.Value()) != "" {
			m.input.Reset()
			m.updateSlashAutocomplete()
			m.adjustInputHeight()
			m.setStatus("cleared input")
			return true, nil
		}
		if !m.lastCtrlC.IsZero() && time.Since(m.lastCtrlC) <= time.Second {
			m.abortRun("exiting")
			return true, tea.Quit
		}
		m.lastCtrlC = time.Now()
		m.setStatus("press ctrl+c again to exit")
		return true, nil
	}

	if key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+d"))) {
		if strings.TrimSpace(m.input.Value()) == "" {
			m.abortRun("exiting")
			return true, tea.Quit
		}
	}

	if key.Matches(msg, key.NewBinding(key.WithKeys("esc"))) {
		if m.running {
			m.abortRun("run aborted")
			m.addSystem("run aborted")
			m.refreshViewport(true)
			if cmd := m.startNextQueuedInput(); cmd != nil {
				return true, cmd
			}
			return true, nil
		}
	}

	if key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+o"))) {
		m.toolsExpanded = !m.toolsExpanded
		m.addSystem(fmt.Sprintf("tool cards: %s", mapBool(m.toolsExpanded, "expanded", "collapsed")))
		m.refreshViewport(true)
		return true, nil
	}

	if key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+y"))) {
		m.mouseEnabled = !m.mouseEnabled
		m.addSystem(fmt.Sprintf("mouse capture: %s", onOff(m.mouseEnabled)))
		m.refreshViewport(true)
		return true, nil
	}

	if key.Matches(msg, key.NewBinding(key.WithKeys("shift+tab"))) {
		if m.moveSlashSelection(-1) {
			return true, nil
		}
	}

	if msg.Code == tea.KeyTab {
		if m.applySlashCompletion() {
			m.adjustInputHeight()
			m.layout()
			return true, nil
		}
	}

	if m.handleMultilinePaste(msg) {
		m.updateSlashAutocomplete()
		m.adjustInputHeight()
		m.layout()
		return true, nil
	}

	if msg.Code == tea.KeyEnter && !key.Matches(msg, m.input.KeyMap.InsertNewline) {
		cmd := m.submitInput()
		return true, cmd
	}

	if key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+p", "alt+up"))) {
		if m.useHistory(-1) {
			m.updateSlashAutocomplete()
			m.adjustInputHeight()
			return true, nil
		}
	}

	if key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+n", "alt+down"))) {
		if m.useHistory(1) {
			m.updateSlashAutocomplete()
			m.adjustInputHeight()
			return true, nil
		}
	}

	if msg.Code == tea.KeyUp {
		if m.moveSlashSelection(-1) {
			return true, nil
		}
		if m.canUseArrowHistory() && m.useHistory(-1) {
			m.updateSlashAutocomplete()
			m.adjustInputHeight()
			return true, nil
		}
	}

	if msg.Code == tea.KeyDown {
		if m.moveSlashSelection(1) {
			return true, nil
		}
		if m.canUseArrowHistory() && m.useHistory(1) {
			m.updateSlashAutocomplete()
			m.adjustInputHeight()
			return true, nil
		}
	}

	return false, nil
}

func (m *model) rememberHistory(value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	if len(m.history) > 0 && m.history[len(m.history)-1] == value {
		m.historyIdx = -1
		m.historyDraft = ""
		return
	}
	m.history = append(m.history, value)
	if len(m.history) > 200 {
		m.history = m.history[len(m.history)-200:]
	}
	m.historyIdx = -1
	m.historyDraft = ""
}

func (m *model) useHistory(direction int) bool {
	if len(m.history) == 0 {
		return false
	}

	value := m.input.Value()
	if strings.Contains(value, "\n") {
		return false
	}

	if direction < 0 {
		if m.historyIdx == -1 {
			m.historyDraft = value
			m.historyIdx = len(m.history) - 1
		} else if m.historyIdx > 0 {
			m.historyIdx--
		}
		m.input.SetValue(m.history[m.historyIdx])
		m.input.CursorEnd()
		return true
	}

	if m.historyIdx == -1 {
		return false
	}
	if m.historyIdx < len(m.history)-1 {
		m.historyIdx++
		m.input.SetValue(m.history[m.historyIdx])
		m.input.CursorEnd()
		return true
	}

	m.historyIdx = -1
	m.input.SetValue(m.historyDraft)
	m.input.CursorEnd()
	return true
}

func (m *model) canUseArrowHistory() bool {
	if m.historyIdx != -1 {
		return true
	}
	return m.input.Value() != ""
}

func (m *model) handleMultilinePaste(msg tea.KeyPressMsg) bool {
	raw := msg.Text
	if raw == "" {
		return false
	}
	if !strings.ContainsAny(raw, "\r\n") {
		return false
	}
	m.input.InsertString(normalizeInputNewlines(raw))
	return true
}

func normalizeInputNewlines(value string) string {
	normalized := strings.ReplaceAll(value, "\r\n", "\n")
	return strings.ReplaceAll(normalized, "\r", "\n")
}
