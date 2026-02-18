package tui

import (
	"strings"

	"charm.land/bubbles/v2/spinner"
)

func statusIconSpinner(status string) spinner.Spinner {
	_ = status
	return spinner.Dot
}

func isToolStatus(status string) bool {
	normalized := strings.ToLower(strings.TrimSpace(status))
	return normalized == "tool" || strings.HasPrefix(normalized, "tool ")
}

func spinnerConfigEqual(a, b spinner.Spinner) bool {
	if a.FPS != b.FPS || len(a.Frames) != len(b.Frames) {
		return false
	}
	for i := range a.Frames {
		if a.Frames[i] != b.Frames[i] {
			return false
		}
	}
	return true
}

func (m *model) syncStatusIconSpinner(status string) {
	nextSpinner := statusIconSpinner(status)
	if spinnerConfigEqual(m.spinner.Spinner, nextSpinner) {
		return
	}
	style := m.spinner.Style
	next := spinner.New()
	next.Spinner = nextSpinner
	next.Style = style
	m.spinner = next
}
