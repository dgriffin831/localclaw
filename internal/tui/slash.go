package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dgriffin831/localclaw/internal/llm"
)

type slashCommandDef struct {
	Name        string
	Args        string
	Description string
	Shortcut    string
}

var slashCommandDefs = []slashCommandDef{
	{Name: "help", Description: "show this help"},
	{Name: "status", Description: "show current status and session info"},
	{Name: "tools", Description: "show provider and available localclaw tools", Shortcut: "Ctrl+O"},
	{Name: "clear", Description: "clear the visible transcript"},
	{Name: "reset", Description: "reset the current session"},
	{Name: "new", Description: "start a new session"},
	{Name: "thinking", Args: "<on|off>", Description: "toggle thinking visibility", Shortcut: "Ctrl+T"},
	{Name: "verbose", Args: "<on|off>", Description: "toggle verbose mode"},
	{Name: "mouse", Args: "<on|off>", Description: "toggle mouse capture (wheel/selection tradeoff)", Shortcut: "Ctrl+Y"},
	{Name: "shortcuts", Description: "show keyboard shortcuts"},
	{Name: "model", Args: "<name>", Description: "set model override for this TUI session"},
	{Name: "exit", Description: "exit the TUI", Shortcut: "Ctrl+D"},
	{Name: "quit", Description: "alias for /exit", Shortcut: "Ctrl+D"},
}

func (m *model) handleSlash(raw string) tea.Cmd {
	name, arg := parseSlash(raw)
	switch name {
	case "help":
		m.addSystem(slashHelpText())
	case "shortcuts":
		m.addSystem(keyboardShortcutsText())
	case "status":
		override := strings.TrimSpace(m.modelOverride)
		if override == "" {
			override = "none"
		}
		m.addSystem(fmt.Sprintf("status=%s provider=%s configured_model=%s effective_model=%s model_override=%s agent=%s session=%s workspace=%s thinking=%s verbose=%s mouse=%s", m.status, m.activeProvider(), valueOrDefault(m.configuredModel(), "n/a"), valueOrDefault(m.effectiveModel(), "n/a"), override, m.agentID, m.sessionID, m.workspacePath, onOff(m.showThinking), onOff(m.verbose), onOff(m.mouseEnabled)))
	case "tools":
		m.addSystem(m.toolsSummary())
	case "clear":
		m.messages = nil
		resetToolCardIndexByCallID(m.toolCardIndexByCallID)
	case "reset":
		m.runSessionReset(false, "/reset")
	case "new":
		m.runSessionReset(true, "/new")
	case "exit", "quit":
		m.abortRun("exiting")
		return tea.Quit
	case "thinking":
		if arg == "on" {
			m.showThinking = true
			m.addSystem("thinking visibility: on")
		} else if arg == "off" {
			m.showThinking = false
			m.addSystem("thinking visibility: off")
		} else {
			m.addSystem("usage: /thinking <on|off>")
		}
	case "verbose":
		if arg == "on" {
			m.verbose = true
			m.addSystem("verbose: on")
		} else if arg == "off" {
			m.verbose = false
			m.addSystem("verbose: off")
		} else {
			m.addSystem("usage: /verbose <on|off>")
		}
	case "mouse":
		if arg == "on" {
			m.mouseEnabled = true
			m.addSystem("mouse capture: on")
			m.refreshViewport(true)
			return tea.EnableMouseCellMotion
		} else if arg == "off" {
			m.mouseEnabled = false
			m.addSystem("mouse capture: off")
			m.refreshViewport(true)
			return tea.DisableMouse
		} else {
			m.addSystem("usage: /mouse <on|off>")
		}
	case "model":
		requested := strings.TrimSpace(arg)
		if requested == "" {
			m.addSystem("usage: /model <name>")
			break
		}
		normalized := strings.ToLower(requested)
		if normalized == "default" || normalized == "off" {
			m.modelOverride = ""
			m.addSystem(fmt.Sprintf("model override cleared; using configured model %s", valueOrDefault(m.configuredModel(), "n/a")))
			break
		}
		if !m.providerSupportsModelOverride() {
			m.modelOverride = ""
			m.addSystem(fmt.Sprintf("provider %s does not support model override; using configured model %s", m.activeProvider(), valueOrDefault(m.configuredModel(), "n/a")))
		} else {
			m.modelOverride = requested
			m.addSystem(fmt.Sprintf("model override set to %s", requested))
		}
	default:
		m.addSystem(fmt.Sprintf("unknown command: /%s", name))
	}
	m.refreshViewport(true)
	return nil
}

func (m *model) toolsSummary() string {
	provider := m.activeProvider()

	lines := []string{fmt.Sprintf("provider=%s", provider)}
	if model := strings.TrimSpace(m.effectiveModel()); model != "" {
		lines = append(lines, "effective model: "+model)
	}
	if strings.TrimSpace(m.providerModel) != "" {
		lines = append(lines, "provider model: "+m.providerModel)
	}
	if override := strings.TrimSpace(m.modelOverride); override != "" {
		lines = append(lines, "model override: "+override)
	}

	lines = append(lines, "provider_native:")
	if len(m.providerTools) == 0 {
		lines = append(lines, "- not discovered yet")
	} else {
		lines = append(lines, "- "+strings.Join(m.providerTools, ", "))
	}

	lines = append(lines, "localclaw_mcp:")

	if m.app == nil {
		lines = append(lines, "- runtime unavailable")
		return strings.Join(lines, "\n")
	}

	tools := m.app.ToolDefinitions(m.agentID)
	if len(tools) == 0 {
		lines = append(lines, "- none enabled")
		return strings.Join(lines, "\n")
	}

	parts := make([]string, 0, len(tools))
	for _, tool := range tools {
		parts = append(parts, tool.Name)
	}
	lines = append(lines, "- "+strings.Join(parts, ", "))
	return strings.Join(lines, "\n")
}

func (m *model) activeProvider() string {
	provider := strings.TrimSpace(m.providerName)
	if provider == "" {
		provider = strings.TrimSpace(m.cfg.LLM.Provider)
	}
	if provider == "" {
		return "unknown"
	}
	return provider
}

func (m *model) configuredModel() string {
	switch strings.ToLower(strings.TrimSpace(m.cfg.LLM.Provider)) {
	case "codex":
		return strings.TrimSpace(m.cfg.LLM.Codex.Model)
	case "claudecode":
		return strings.TrimSpace(m.cfg.LLM.ClaudeCode.Profile)
	default:
		return ""
	}
}

func (m *model) effectiveModel() string {
	if m.providerSupportsModelOverride() {
		if override := strings.TrimSpace(m.modelOverride); override != "" {
			return override
		}
	}
	if model := strings.TrimSpace(m.providerModel); model != "" {
		return model
	}
	if model := strings.TrimSpace(m.configuredModel()); model != "" {
		return model
	}
	return ""
}

func (m *model) providerSupportsModelOverride() bool {
	return strings.EqualFold(m.activeProvider(), "codex")
}

func toolOwnershipLabel(class llm.ToolClass) string {
	switch class {
	case llm.ToolClassDelegated:
		return "provider_native"
	case llm.ToolClassLocal:
		return "localclaw_mcp"
	default:
		return "unspecified"
	}
}

func resetToolCallOwnershipByID(values map[string]llm.ToolClass) {
	for id := range values {
		delete(values, id)
	}
}

func resetToolCardIndexByCallID(values map[string]int) {
	for id := range values {
		delete(values, id)
	}
}

func normalizeProviderToolList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]string{}
	for _, raw := range values {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = name
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for _, name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func parseSlash(raw string) (string, string) {
	trimmed := strings.TrimSpace(strings.TrimPrefix(raw, "/"))
	if trimmed == "" {
		return "", ""
	}
	parts := strings.Fields(trimmed)
	if len(parts) == 1 {
		return strings.ToLower(parts[0]), ""
	}
	return strings.ToLower(parts[0]), strings.TrimSpace(strings.TrimPrefix(trimmed, parts[0]))
}

func (m *model) updateSlashAutocomplete() {
	query, active := parseSlashAutocompleteInput(m.input.Value())
	if !active {
		m.slashQuery = ""
		m.slashMatches = nil
		m.slashSelected = 0
		return
	}

	matches := findSlashMatches(query)
	if len(matches) == 0 {
		m.slashQuery = query
		m.slashMatches = nil
		m.slashSelected = 0
		return
	}

	prevName := ""
	if m.slashSelected >= 0 && m.slashSelected < len(m.slashMatches) {
		prevName = m.slashMatches[m.slashSelected].Name
	}

	m.slashMatches = matches
	if query != m.slashQuery {
		m.slashSelected = 0
	} else if prevName != "" {
		m.slashSelected = indexSlashMatch(matches, prevName)
	}
	m.slashQuery = query
	if m.slashSelected < 0 || m.slashSelected >= len(m.slashMatches) {
		m.slashSelected = 0
	}
}

func (m *model) moveSlashSelection(delta int) bool {
	if len(m.slashMatches) == 0 || delta == 0 {
		return false
	}
	m.slashSelected = (m.slashSelected + delta) % len(m.slashMatches)
	if m.slashSelected < 0 {
		m.slashSelected += len(m.slashMatches)
	}
	return true
}

func (m *model) applySlashCompletion() bool {
	if len(m.slashMatches) == 0 {
		return false
	}
	idx := m.slashSelected
	if idx < 0 || idx >= len(m.slashMatches) {
		idx = 0
	}
	m.input.SetValue(formatSlashInput(m.slashMatches[idx]))
	m.input.CursorEnd()
	m.updateSlashAutocomplete()
	return true
}

func (m *model) slashMenuView() string {
	if len(m.slashMatches) == 0 {
		return ""
	}

	maxWidth := panelInnerWidth(m.width)
	limit := slashMenuLimit
	if len(m.slashMatches) < limit {
		limit = len(m.slashMatches)
	}

	lines := make([]string, 0, limit+1)
	for i := 0; i < limit; i++ {
		cmd := m.slashMatches[i]
		line := formatSlashMenuLine(cmd)
		prefix := "  "
		style := slashMenuItemStyle
		if i == m.slashSelected {
			prefix = "› "
			style = slashMenuSelectedStyle
		}
		lines = append(lines, style.Render(truncateText(prefix+line, maxWidth)))
	}

	if len(m.slashMatches) > limit {
		remaining := len(m.slashMatches) - limit
		more := fmt.Sprintf("  +%d more commands (type to filter)", remaining)
		lines = append(lines, slashMenuMoreStyle.Render(truncateText(more, maxWidth)))
	}
	return strings.Join(lines, "\n")
}

func parseSlashAutocompleteInput(raw string) (string, bool) {
	if strings.Contains(raw, "\n") {
		return "", false
	}
	trimmed := strings.TrimLeft(raw, " \t")
	if !strings.HasPrefix(trimmed, "/") {
		return "", false
	}
	remainder := strings.TrimPrefix(trimmed, "/")
	if remainder == "" {
		return "", true
	}
	if strings.ContainsAny(remainder, " \t") {
		return "", false
	}
	return strings.ToLower(remainder), true
}

func findSlashMatches(query string) []slashCommandDef {
	normalized := strings.ToLower(strings.TrimSpace(query))
	if normalized == "" {
		return append([]slashCommandDef(nil), slashCommandDefs...)
	}

	matches := make([]slashCommandDef, 0, len(slashCommandDefs))
	secondary := make([]slashCommandDef, 0, len(slashCommandDefs))
	for _, cmd := range slashCommandDefs {
		name := strings.ToLower(cmd.Name)
		if strings.HasPrefix(name, normalized) {
			matches = append(matches, cmd)
			continue
		}
		if strings.Contains(name, normalized) {
			secondary = append(secondary, cmd)
		}
	}
	return append(matches, secondary...)
}

func indexSlashMatch(matches []slashCommandDef, name string) int {
	for idx, cmd := range matches {
		if cmd.Name == name {
			return idx
		}
	}
	return 0
}

func slashHelpText() string {
	lines := make([]string, 0, len(slashCommandDefs)+1)
	lines = append(lines, "slash commands:")
	for _, cmd := range slashCommandDefs {
		lines = append(lines, formatSlashMenuLine(cmd))
	}
	return strings.Join(lines, "\n")
}

func keyboardShortcutsText() string {
	lines := []string{
		"keyboard shortcuts:",
		"Enter                  submit input",
		"Ctrl+J                 insert newline",
		"Tab                    autocomplete selected slash command",
		"Shift+Tab              move slash-command selection backward",
		"Up/Down                navigate slash menu; when hidden, use history after non-empty draft",
		"Ctrl+P / Ctrl+N        navigate prompt history",
		"Alt+Up / Alt+Down      history navigation aliases",
		"Mouse wheel            scroll transcript viewport",
		"Esc                    abort active run",
		"Ctrl+T                 toggle thinking visibility",
		"Ctrl+O                 toggle tool-card expansion",
		"Ctrl+Y                 toggle mouse capture",
		"Ctrl+C                 clear input (press twice quickly to exit)",
		"Ctrl+D                 exit when input is empty",
	}
	return strings.Join(lines, "\n")
}

func formatSlashMenuLine(cmd slashCommandDef) string {
	usage := formatSlashUsage(cmd)
	desc := strings.TrimSpace(cmd.Description)
	shortcut := strings.TrimSpace(cmd.Shortcut)
	if shortcut == "" {
		return fmt.Sprintf("%-22s %s", usage, desc)
	}
	return fmt.Sprintf("%-22s %-52s %s", usage, desc, shortcut)
}

func formatSlashUsage(cmd slashCommandDef) string {
	if strings.TrimSpace(cmd.Args) == "" {
		return "/" + cmd.Name
	}
	return fmt.Sprintf("/%s %s", cmd.Name, cmd.Args)
}

func formatSlashInput(cmd slashCommandDef) string {
	return "/" + cmd.Name + " "
}
