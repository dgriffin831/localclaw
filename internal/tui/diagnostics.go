package tui

import (
	"fmt"
	"sort"
	"strings"
)

func (m *model) addVerbose(format string, args ...interface{}) {
	if !m.verbose {
		return
	}
	m.addSystem("[verbose] " + fmt.Sprintf(format, args...))
}

func (m *model) emitVerboseRunStartDiagnostics(input string) {
	lineCount := strings.Count(input, "\n") + 1
	m.addVerbose("prompt: chars=%d lines=%d session=%s", len(strings.TrimSpace(input)), lineCount, m.sessionKey)
	if m.app == nil {
		m.addVerbose("runtime: unavailable")
		return
	}
	tools := m.app.ToolDefinitions(m.agentID)
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	m.addVerbose("runtime: agent=%s workspace=%s local_tools=%s", m.agentID, formatWorkspacePath(m.workspacePath), summarizeVerboseList(names))
}

func summarizeVerboseMap(values map[string]interface{}) string {
	if len(values) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", key, truncateVerboseText(fmt.Sprint(values[key]))))
	}
	return strings.Join(parts, ", ")
}

func summarizeVerboseKeys(values map[string]interface{}) string {
	if len(values) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func summarizeVerboseList(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return truncateVerboseText(strings.Join(values, ", "))
}

func truncateVerboseText(value string) string {
	trimmed := strings.TrimSpace(strings.Join(strings.Fields(value), " "))
	if len(trimmed) <= 80 {
		return trimmed
	}
	return trimmed[:77] + "..."
}
