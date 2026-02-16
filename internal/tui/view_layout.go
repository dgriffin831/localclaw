package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func (m *model) headerView() string {
	provider := m.activeProvider()
	model := valueOrDefault(m.effectiveModel(), "n/a")
	innerWidth := panelInnerWidth(m.width)
	left := "# localclaw"
	right := fmt.Sprintf(
		"provider:%s  model:%s  workspace:%s",
		provider,
		model,
		formatWorkspacePath(m.workspacePath),
	)
	if innerWidth < 70 {
		right = fmt.Sprintf("p:%s m:%s ws:%s", provider, model, formatWorkspacePath(m.workspacePath))
	}
	line := twoColumn(left, right, innerWidth)
	return headerStyle.Width(max(1, m.width)).Render(line)
}

func (m *model) statusView() string {
	elapsed := ""
	if m.isBusy() && !m.statusStartedAt.IsZero() {
		elapsed = fmt.Sprintf(" • %s", formatElapsed(time.Since(m.statusStartedAt)))
	}

	base := m.status
	if base == statusWaiting && m.showThinking && !m.hasStreamDelta {
		base = m.currentThinkingMessage()
	}
	provider := m.activeProvider()
	model := valueOrDefault(m.effectiveModel(), "n/a")
	settings := fmt.Sprintf(
		"provider:%s  model:%s  thinking:%s  verbose:%s  tools:%s  mouse:%s  /status",
		provider,
		model,
		onOff(m.showThinking),
		onOff(m.verbose),
		mapBool(m.toolsExpanded, "expanded", "collapsed"),
		onOff(m.mouseEnabled),
	)
	innerWidth := panelInnerWidth(m.width)
	if innerWidth < 70 {
		settings = fmt.Sprintf("p:%s m:%s t:%s v:%s /status", provider, model, onOff(m.showThinking), onOff(m.verbose))
	}
	if innerWidth < 42 {
		settings = "/status"
	}

	if m.isBusy() {
		left := fmt.Sprintf("%s %s%s", m.spinner.View(), base, elapsed)
		return statusBusyStyle.Width(max(1, m.width)).Render(twoColumn(left, settings, innerWidth))
	}
	if m.status == statusError {
		return statusErrStyle.Width(max(1, m.width)).Render(twoColumn("error", settings, innerWidth))
	}
	return statusIdleStyle.Width(max(1, m.width)).Render(twoColumn(base, settings, innerWidth))
}

func (m *model) inputView() string {
	hintText := "Ctrl+J newline • Ctrl+Y mouse • Ctrl+O tools • Ctrl+T thinking • /shortcuts"
	if panelInnerWidth(m.width) < 90 {
		hintText = "Ctrl+J newline • Ctrl+Y mouse • Ctrl+O tools • /shortcuts"
	}
	if panelInnerWidth(m.width) < 70 {
		hintText = "Ctrl+J newline • Ctrl+Y mouse • /shortcuts"
	}
	if panelInnerWidth(m.width) < 42 {
		hintText = "Ctrl+J • /shortcuts"
	}
	hint := inputHintStyle.Render(truncateText(hintText, panelInnerWidth(m.width)))
	body := m.input.View()
	if menu := m.slashMenuView(); menu != "" {
		body += "\n" + menu
	}
	body += "\n" + hint
	return inputStyle.Width(max(1, m.width)).Render(body)
}

func (m *model) layout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}

	innerWidth := panelInnerWidth(m.width)
	m.input.SetWidth(max(10, innerWidth-2))
	m.adjustInputHeight()

	headerHeight := lipgloss.Height(m.headerView())
	statusHeight := lipgloss.Height(m.statusView())
	inputHeight := lipgloss.Height(m.inputView())

	viewportHeight := m.height - headerHeight - statusHeight - inputHeight
	if viewportHeight < 3 {
		m.input.SetHeight(1)
		inputHeight = lipgloss.Height(m.inputView())
		viewportHeight = m.height - headerHeight - statusHeight - inputHeight
	}
	if viewportHeight < 1 {
		viewportHeight = 1
	}

	m.viewport.Width = max(20, m.width)
	m.viewport.Height = viewportHeight
}

func (m *model) adjustInputHeight() {
	lineCount := 1
	if v := m.input.Value(); v != "" {
		lineCount = strings.Count(v, "\n") + 1
	}
	if lineCount < 1 {
		lineCount = 1
	}
	if lineCount > 8 {
		lineCount = 8
	}
	m.input.SetHeight(lineCount)
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

func mapBool(v bool, yes string, no string) string {
	if v {
		return yes
	}
	return no
}

func formatElapsed(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	seconds := int(d.Seconds())
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := seconds / 60
	remaining := seconds % 60
	return fmt.Sprintf("%dm%ds", minutes, remaining)
}

func max(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func panelInnerWidth(total int) int {
	return max(1, total-4)
}

func twoColumn(left string, right string, width int) string {
	if width <= 0 {
		return ""
	}
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if right == "" {
		return truncateText(left, width)
	}

	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)
	if leftWidth+1+rightWidth > width {
		maxLeft := width - rightWidth - 1
		if maxLeft > 0 {
			left = truncateText(left, maxLeft)
			leftWidth = lipgloss.Width(left)
		} else {
			return truncateText(right, width)
		}
	}

	space := width - leftWidth - rightWidth
	if space < 1 {
		return truncateText(left, width)
	}
	return left + strings.Repeat(" ", space) + right
}

func formatWorkspacePath(root string) string {
	clean := filepath.Clean(root)
	if clean == "." {
		return "."
	}
	return clean
}

func truncateText(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}

	target := width - 1
	var b strings.Builder
	current := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if current+rw > target {
			break
		}
		b.WriteRune(r)
		current += rw
	}
	return b.String() + "…"
}
