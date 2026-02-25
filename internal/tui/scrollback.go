package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

func (m *model) queueScrollbackMirrorLines() {
	if m.mouseEnabled {
		return
	}
	for i := range m.messages {
		msg := &m.messages[i]
		if msg.MirroredToScrollback {
			continue
		}
		line, ok := m.scrollbackMirrorLine(*msg)
		if !ok {
			continue
		}
		msg.MirroredToScrollback = true
		m.pendingScrollbackLines = append(m.pendingScrollbackLines, line)
	}
}

func (m *model) scrollbackMirrorLine(msg chatMessage) (string, bool) {
	if msg.Streaming {
		return "", false
	}
	if msg.ToolCard != nil {
		if !msg.ToolCard.HasResult {
			return "", false
		}
		text := strings.TrimSpace(m.renderToolCard(msg.ToolCard, m.toolsExpanded))
		if text == "" {
			return "", false
		}
		return formatScrollbackBlock("tool> ", text), true
	}
	if msg.Role != roleAssistant {
		return "", false
	}
	text := strings.TrimSpace(msg.Raw)
	if text == "" {
		return "", false
	}
	return formatScrollbackBlock("assistant> ", text), true
}

func (m *model) scrollbackMirrorCmd() tea.Cmd {
	if len(m.pendingScrollbackLines) == 0 {
		return nil
	}
	lines := append([]string(nil), m.pendingScrollbackLines...)
	m.pendingScrollbackLines = nil
	cmds := make([]tea.Cmd, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		cmds = append(cmds, tea.Println(line))
	}
	if len(cmds) == 0 {
		return nil
	}
	if len(cmds) == 1 {
		return cmds[0]
	}
	return tea.Batch(cmds...)
}

func combineWithScrollbackCmd(primary tea.Cmd, scrollback tea.Cmd) tea.Cmd {
	if primary == nil {
		return scrollback
	}
	if scrollback == nil {
		return primary
	}
	return tea.Sequence(scrollback, primary)
}

func formatScrollbackBlock(prefix string, text string) string {
	normalized := normalizeInputNewlines(strings.TrimSpace(text))
	if normalized == "" {
		return ""
	}
	lines := strings.Split(normalized, "\n")
	indent := strings.Repeat(" ", len(prefix))
	for i, line := range lines {
		if i == 0 {
			lines[i] = prefix + line
			continue
		}
		lines[i] = indent + line
	}
	return strings.Join(lines, "\n")
}
