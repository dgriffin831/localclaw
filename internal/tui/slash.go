package tui

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/dgriffin831/localclaw/internal/llm"
	"github.com/dgriffin831/localclaw/internal/runtime"
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
	{Name: "tools", Description: "show provider-reported tools", Shortcut: "Ctrl+O"},
	{Name: "models", Args: "[refresh]", Description: "show discovered provider models"},
	{Name: "clear", Description: "clear the visible transcript"},
	{Name: "reset", Description: "reset the current session"},
	{Name: "new", Description: "start a new session"},
	{Name: "sessions", Description: "list sessions for the current agent"},
	{Name: "resume", Args: "<session_id>", Description: "resume a specific session"},
	{Name: "delete", Args: "<session_id>", Description: "delete a non-active session"},
	{Name: "verbose", Args: "<on|off>", Description: "toggle verbose mode"},
	{Name: "mouse", Args: "<on|off>", Description: "toggle mouse capture (wheel/selection tradeoff)", Shortcut: "Ctrl+Y"},
	{Name: "shortcuts", Description: "show keyboard shortcuts"},
	{Name: "model", Args: "<provider>/<model>[/<reasoning>]", Description: "set active selector for this TUI session"},
	{Name: "exit", Description: "exit the TUI", Shortcut: "Ctrl+D"},
	{Name: "quit", Description: "alias for /exit", Shortcut: "Ctrl+D"},
}

func (m *model) handleSlash(raw string) tea.Cmd {
	name, arg := parseSlash(raw)
	var followUp tea.Cmd
	switch name {
	case "help":
		m.addSystem(slashHelpText())
	case "shortcuts":
		m.addSystem(keyboardShortcutsText())
	case "status":
		override := m.selectorOverride()
		if strings.TrimSpace(override) == "" {
			override = "none"
		}
		m.addSystem(fmt.Sprintf("status=%s provider=%s configured_model=%s effective_model=%s effective_selector=%s selector_override=%s agent=%s session=%s workspace=%s verbose=%s mouse=%s", m.status, m.activeProvider(), valueOrDefault(m.configuredModel(), "n/a"), valueOrDefault(m.effectiveModel(), "n/a"), valueOrDefault(m.effectiveSelector(), "n/a"), override, m.agentID, m.sessionID, m.workspacePath, onOff(m.verbose), onOff(m.mouseEnabled)))
	case "tools":
		followUp = m.startProviderToolsDiscoveryIfNeeded()
		m.addSystem(m.toolsSummary())
	case "models":
		requested := strings.ToLower(strings.TrimSpace(arg))
		refresh := false
		if requested == "" {
			refresh = false
		} else if requested == "refresh" {
			refresh = true
		} else {
			m.addSystem("usage: /models [refresh]")
			break
		}
		if m.app == nil {
			m.addSystem("runtime unavailable")
			break
		}
		followUp = m.startProviderModelDiscovery(refresh)
		m.addSystem(m.modelsSummary())
	case "clear":
		m.messages = nil
		resetToolCardIndexByCallID(m.toolCardIndexByCallID)
	case "reset":
		m.runSessionReset(false, "/reset")
	case "new":
		m.runSessionReset(true, "/new")
		if m.bootstrapSeedPendingForSession() {
			followUp = emitBootstrapSeedTrigger()
		}
	case "sessions":
		m.handleSessionsList()
	case "resume":
		m.handleSessionResume(arg)
	case "delete":
		m.handleSessionDelete(arg)
	case "exit", "quit":
		m.abortRun("exiting")
		return tea.Quit
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
		} else if arg == "off" {
			m.mouseEnabled = false
			m.addSystem("mouse capture: off")
			m.refreshViewport(true)
		} else {
			m.addSystem("usage: /mouse <on|off>")
		}
	case "model":
		m.handleModelSelectionSlash(arg)
	default:
		m.addSystem(fmt.Sprintf("unknown command: /%s", name))
	}
	m.refreshViewport(true)
	return followUp
}

func (m *model) handleSessionsList() {
	if m.app == nil {
		m.addSystem("runtime unavailable")
		return
	}

	result, err := m.app.MCPSessionsList(m.ctx, m.agentID, 100, 0)
	if err != nil {
		m.addSystem(fmt.Sprintf("sessions list failed: %v", err))
		return
	}

	lines := []string{fmt.Sprintf("sessions (%d):", result.Total)}
	for _, entry := range result.Sessions {
		label := entry.ID
		if strings.TrimSpace(label) == "" {
			label = "(unknown)"
		}
		if entry.ID == m.sessionID {
			label += " (current)"
		}
		updatedAt := strings.TrimSpace(entry.UpdatedAt)
		if updatedAt == "" {
			updatedAt = "n/a"
		}
		lines = append(lines, fmt.Sprintf("- %s updated=%s tokens=%d", label, updatedAt, entry.TotalTokens))
	}
	if len(result.Sessions) == 0 {
		lines = append(lines, "- none")
	}
	m.addSystem(strings.Join(lines, "\n"))
}

func (m *model) handleSessionResume(rawSessionID string) {
	sessionID := strings.TrimSpace(rawSessionID)
	if sessionID == "" {
		m.addSystem("usage: /resume <session_id>")
		return
	}
	if m.app == nil {
		m.addSystem("runtime unavailable")
		return
	}

	resolution := runtime.ResolveSession(m.agentID, sessionID)
	entry, err := m.app.MCPSessionStatus(m.ctx, resolution.AgentID, resolution.SessionID)
	if err != nil {
		if errors.Is(err, runtime.ErrMCPNotFound) {
			m.addSystem(fmt.Sprintf("session %s not found", resolution.SessionID))
			return
		}
		m.addSystem(fmt.Sprintf("resume failed: %v", err))
		return
	}

	m.abortRun("")
	m.clearQueuedInputs()
	m.agentID = resolution.AgentID
	m.sessionID = resolution.SessionID
	m.sessionKey = resolution.SessionKey
	m.sessionTokens = max(0, entry.TotalTokens)
	m.messages = nil
	m.providerOverride = ""
	m.modelOverride = ""
	m.reasoningOverride = ""
	m.providerTools = nil
	m.providerToolsDiscoveryInFlight = false
	resetToolCardIndexByCallID(m.toolCardIndexByCallID)

	history, err := m.app.MCPSessionsHistory(m.ctx, resolution.AgentID, resolution.SessionID, 0, 0)
	if err != nil {
		m.addSystem(fmt.Sprintf("resumed session %s (history unavailable: %v)", resolution.SessionID, err))
		return
	}

	loaded := 0
	for _, item := range history.Items {
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(item.Role)) {
		case "user":
			m.messages = append(m.messages, chatMessage{Role: roleUser, Raw: content})
		case "assistant":
			m.messages = append(m.messages, chatMessage{Role: roleAssistant, Raw: content})
		case "system":
			m.messages = append(m.messages, chatMessage{Role: roleSystem, Raw: content})
		default:
			m.messages = append(m.messages, chatMessage{Role: roleSystem, Raw: content})
		}
		loaded++
	}
	m.addSystem(fmt.Sprintf("resumed session %s (%d transcript messages loaded)", resolution.SessionID, loaded))
}

func (m *model) handleSessionDelete(rawSessionID string) {
	sessionID := strings.TrimSpace(rawSessionID)
	if sessionID == "" {
		m.addSystem("usage: /delete <session_id>")
		return
	}
	resolution := runtime.ResolveSession(m.agentID, sessionID)
	if resolution.SessionID == m.sessionID {
		m.addSystem(fmt.Sprintf("cannot delete active session %s; resume a different session first", resolution.SessionID))
		return
	}
	if m.app == nil {
		m.addSystem("runtime unavailable")
		return
	}

	removed, err := m.app.MCPSessionDelete(m.ctx, resolution.AgentID, resolution.SessionID)
	if err != nil {
		m.addSystem(fmt.Sprintf("delete failed: %v", err))
		return
	}
	if !removed {
		m.addSystem(fmt.Sprintf("session %s not found", resolution.SessionID))
		return
	}
	m.addSystem(fmt.Sprintf("deleted session %s", resolution.SessionID))
}

func (m *model) toolsSummary() string {
	lines := []string{
		fmt.Sprintf("provider=%s", m.activeProvider()),
		"tools:",
	}
	if len(m.providerTools) == 0 {
		if m.app == nil {
			lines = append(lines, "- runtime unavailable")
		} else if m.providerToolsDiscoveryInFlight {
			lines = append(lines, "- discovering...")
		} else {
			lines = append(lines, "- not discovered yet")
		}
		return strings.Join(lines, "\n")
	}
	for _, name := range m.providerTools {
		lines = append(lines, "- "+name)
	}
	return strings.Join(lines, "\n")
}

func (m *model) modelsSummary() string {
	lines := []string{
		"models:",
		"active: " + m.activeSummary(),
	}
	if m.app == nil {
		lines = append(lines, "- runtime unavailable")
		return strings.Join(lines, "\n")
	}
	if m.providerModelsDiscoveryInFlight {
		lines = append(lines, "- discovering...")
	}

	providers := m.knownProviders()
	if len(providers) == 0 {
		lines = append(lines, "- none")
		return strings.Join(lines, "\n")
	}
	for _, provider := range providers {
		lines = append(lines, provider+":")
		if errText, ok := m.providerModelCatalogErrors[provider]; ok && strings.TrimSpace(errText) != "" {
			lines = append(lines, "- discovery failed: "+errText)
			continue
		}
		catalog, ok := m.providerModelCatalogs[provider]
		if !ok || len(catalog.Models) == 0 {
			lines = append(lines, "- no models discovered")
			continue
		}
		for _, model := range catalog.Models {
			reasoning := "reasoning: n/a"
			if model.Reasoning.Supported {
				levels := strings.Join(model.Reasoning.Levels, ",")
				if levels == "" {
					levels = "supported"
				}
				reasoning = "reasoning: " + levels
				if defaultLevel := strings.TrimSpace(model.Reasoning.Default); defaultLevel != "" {
					reasoning += " (default: " + defaultLevel + ")"
				}
			}
			marker := "-"
			if strings.EqualFold(provider, m.activeProvider()) && strings.EqualFold(model.Name, m.effectiveModel()) {
				marker = "*"
			}
			lines = append(lines, fmt.Sprintf("%s %s (%s)", marker, model.Name, reasoning))
		}
	}
	return strings.Join(lines, "\n")
}

func (m *model) startProviderToolsDiscoveryIfNeeded() tea.Cmd {
	if m.app == nil {
		return nil
	}
	if m.providerToolsDiscoveryInFlight || len(m.providerTools) > 0 {
		return nil
	}
	m.providerToolsDiscoveryInFlight = true
	opts := llm.PromptOptions{
		ProviderOverride:  strings.TrimSpace(m.providerOverride),
		ModelOverride:     strings.TrimSpace(m.modelOverride),
		ReasoningOverride: strings.TrimSpace(m.reasoningOverride),
	}
	app := m.app
	ctx := m.ctx
	agentID := m.agentID
	return func() tea.Msg {
		meta, err := app.DiscoverProviderMetadata(ctx, agentID, opts)
		return providerToolsDiscoveredMsg{
			Provider: strings.TrimSpace(meta.Provider),
			Model:    strings.TrimSpace(meta.Model),
			Tools:    append([]string{}, meta.Tools...),
			Err:      err,
		}
	}
}

func (m *model) startProviderModelDiscovery(refresh bool) tea.Cmd {
	if m.app == nil {
		return nil
	}
	if m.providerModelsDiscoveryInFlight {
		return nil
	}
	if !refresh && len(m.providerModelCatalogs) > 0 {
		return nil
	}
	m.providerModelsDiscoveryInFlight = true
	app := m.app
	ctx := m.ctx
	return func() tea.Msg {
		catalogs, errs := app.DiscoverProviderModelCatalogs(ctx, refresh)
		serializedErrs := map[string]string{}
		for provider, err := range errs {
			if err == nil {
				continue
			}
			serializedErrs[provider] = err.Error()
		}
		return providerModelsDiscoveredMsg{
			Catalogs: catalogs,
			Errors:   serializedErrs,
		}
	}
}

func (m *model) activeProvider() string {
	provider := strings.TrimSpace(m.providerOverride)
	if provider == "" {
		provider = strings.TrimSpace(m.providerName)
	}
	if provider == "" {
		provider = m.configuredProvider()
	}
	if provider == "" {
		return "unknown"
	}
	return strings.ToLower(provider)
}

func (m *model) configuredProvider() string {
	provider := strings.TrimSpace(m.cfg.LLM.Provider)
	if provider == "" {
		return "unknown"
	}
	return strings.ToLower(provider)
}

func (m *model) configuredModel() string {
	return m.configuredModelForProvider(m.configuredProvider())
}

func (m *model) configuredModelForProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "codex":
		return strings.TrimSpace(m.cfg.LLM.Codex.Model)
	case "claudecode":
		return strings.TrimSpace(m.cfg.LLM.ClaudeCode.Profile)
	default:
		return ""
	}
}

func (m *model) effectiveModel() string {
	if override := strings.TrimSpace(m.modelOverride); override != "" {
		return override
	}
	if strings.EqualFold(m.providerName, m.activeProvider()) {
		if model := strings.TrimSpace(m.providerModel); model != "" {
			return model
		}
	}
	if model := strings.TrimSpace(m.configuredModelForProvider(m.activeProvider())); model != "" {
		return model
	}
	if model := strings.TrimSpace(m.configuredModel()); model != "" {
		return model
	}
	return ""
}

func (m *model) effectiveReasoning() string {
	if override := strings.TrimSpace(m.reasoningOverride); override != "" {
		return strings.ToLower(override)
	}
	if m.hasReasoningSupport(m.activeProvider()) {
		return strings.ToLower(strings.TrimSpace(m.configuredReasoningDefault(m.activeProvider())))
	}
	return ""
}

func (m *model) effectiveSelector() string {
	provider := strings.TrimSpace(m.activeProvider())
	model := strings.TrimSpace(m.effectiveModel())
	reasoning := strings.TrimSpace(m.effectiveReasoning())
	if provider == "" || model == "" {
		return ""
	}
	if reasoning == "" {
		return fmt.Sprintf("%s/%s", provider, model)
	}
	return fmt.Sprintf("%s/%s/%s", provider, model, reasoning)
}

func (m *model) activeSummary() string {
	provider := strings.TrimSpace(m.activeProvider())
	if provider == "" {
		provider = "unknown"
	}
	model := strings.TrimSpace(m.effectiveModel())
	reasoning := strings.TrimSpace(m.effectiveReasoning())
	if model != "" {
		if reasoning == "" {
			return fmt.Sprintf("%s/%s", provider, model)
		}
		return fmt.Sprintf("%s/%s/%s", provider, model, reasoning)
	}
	summary := provider + " model=unknown"
	if reasoning != "" {
		summary += " reasoning=" + reasoning
	}
	return summary
}

func (m *model) selectorOverride() string {
	provider := strings.TrimSpace(m.providerOverride)
	model := strings.TrimSpace(m.modelOverride)
	reasoning := strings.TrimSpace(m.reasoningOverride)
	if provider == "" || model == "" {
		return ""
	}
	if reasoning == "" {
		return fmt.Sprintf("%s/%s", provider, model)
	}
	return fmt.Sprintf("%s/%s/%s", provider, model, reasoning)
}

func (m *model) knownProviders() []string {
	seen := map[string]struct{}{}
	add := func(value string) {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if normalized == "" {
			return
		}
		seen[normalized] = struct{}{}
	}
	add("claudecode")
	add("codex")
	add(m.cfg.LLM.Provider)
	for provider := range m.providerModelCatalogs {
		add(provider)
	}
	for provider := range m.providerModelCatalogErrors {
		add(provider)
	}
	out := make([]string, 0, len(seen))
	for provider := range seen {
		out = append(out, provider)
	}
	sort.Strings(out)
	return out
}

func (m *model) hasReasoningSupport(provider string) bool {
	return strings.EqualFold(strings.TrimSpace(provider), "codex")
}

func (m *model) configuredReasoningDefault(provider string) string {
	if strings.EqualFold(strings.TrimSpace(provider), "codex") {
		return strings.TrimSpace(m.cfg.LLM.Codex.ReasoningDefault)
	}
	return ""
}

func (m *model) handleModelSelectionSlash(raw string) {
	if m.running {
		m.addSystem("cannot change selector while a run is active; abort first")
		return
	}

	requested := strings.TrimSpace(raw)
	if requested == "" {
		m.addSystem("usage: /model <provider>/<model>[/<reasoning>]")
		return
	}
	switch strings.ToLower(requested) {
	case "default", "off":
		m.providerOverride = ""
		m.modelOverride = ""
		m.reasoningOverride = ""
		m.providerTools = nil
		m.providerToolsDiscoveryInFlight = false
		m.addSystem("selector reset to defaults")
		return
	}

	provider, modelName, reasoning, err := parseModelSelector(requested, m.activeProvider())
	if err != nil {
		m.addSystem("invalid selector: " + err.Error() + " (usage: /model <provider>/<model>[/<reasoning>])")
		return
	}
	if !containsString(m.knownProviders(), provider) {
		m.addSystem(fmt.Sprintf("unknown provider %q", provider))
		return
	}
	if reasoning != "" && !m.hasReasoningSupport(provider) {
		m.addSystem(fmt.Sprintf("provider %s does not support reasoning selectors", provider))
		return
	}

	notValidated := false
	if catalog, ok := m.providerModelCatalogs[provider]; ok && len(catalog.Models) > 0 {
		found := false
		for _, discovered := range catalog.Models {
			if !strings.EqualFold(discovered.Name, modelName) {
				continue
			}
			found = true
			if discovered.Reasoning.Supported {
				if reasoning == "" {
					if defaultLevel := strings.TrimSpace(discovered.Reasoning.Default); defaultLevel != "" {
						reasoning = strings.ToLower(defaultLevel)
					}
				}
				if reasoning == "" {
					reasoning = strings.ToLower(strings.TrimSpace(m.configuredReasoningDefault(provider)))
				}
				if len(discovered.Reasoning.Levels) > 0 && reasoning != "" && !containsString(discovered.Reasoning.Levels, reasoning) {
					m.addSystem(fmt.Sprintf("reasoning %q is not supported for %s/%s", reasoning, provider, discovered.Name))
					return
				}
			} else if reasoning != "" {
				m.addSystem(fmt.Sprintf("reasoning is not supported for %s/%s", provider, discovered.Name))
				return
			}
			modelName = discovered.Name
			break
		}
		if !found {
			m.addSystem(fmt.Sprintf("model %q is not available for provider %s", modelName, provider))
			return
		}
	} else {
		notValidated = true
	}

	if reasoning == "" && m.hasReasoningSupport(provider) {
		reasoning = strings.ToLower(strings.TrimSpace(m.configuredReasoningDefault(provider)))
	}

	m.providerOverride = provider
	m.modelOverride = modelName
	m.reasoningOverride = strings.ToLower(strings.TrimSpace(reasoning))
	m.providerTools = nil
	m.providerToolsDiscoveryInFlight = false
	selector := m.selectorOverride()
	if selector == "" {
		selector = m.effectiveSelector()
	}
	if notValidated {
		m.addSystem(fmt.Sprintf("active selector set to %s (provider catalog unavailable; not validated)", selector))
		return
	}
	m.addSystem(fmt.Sprintf("active selector set to %s", selector))
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

func parseModelSelector(raw, currentProvider string) (string, string, string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", "", fmt.Errorf("selector is required")
	}
	if strings.Contains(trimmed, " ") {
		return "", "", "", fmt.Errorf("selector cannot contain spaces")
	}
	parts := strings.Split(trimmed, "/")
	switch len(parts) {
	case 1:
		provider := strings.ToLower(strings.TrimSpace(currentProvider))
		model := strings.TrimSpace(parts[0])
		if provider == "" {
			return "", "", "", fmt.Errorf("provider is required")
		}
		if model == "" {
			return "", "", "", fmt.Errorf("model is required")
		}
		return provider, model, "", nil
	case 2:
		provider := strings.ToLower(strings.TrimSpace(parts[0]))
		model := strings.TrimSpace(parts[1])
		if provider == "" {
			return "", "", "", fmt.Errorf("provider is required")
		}
		if model == "" {
			return "", "", "", fmt.Errorf("model is required")
		}
		return provider, model, "", nil
	case 3:
		provider := strings.ToLower(strings.TrimSpace(parts[0]))
		model := strings.TrimSpace(parts[1])
		reasoning := strings.ToLower(strings.TrimSpace(parts[2]))
		if provider == "" {
			return "", "", "", fmt.Errorf("provider is required")
		}
		if model == "" {
			return "", "", "", fmt.Errorf("model is required")
		}
		if reasoning == "" {
			return "", "", "", fmt.Errorf("reasoning is required when selector includes third segment")
		}
		return provider, model, reasoning, nil
	default:
		return "", "", "", fmt.Errorf("selector must be <model> or <provider>/<model>[/<reasoning>]")
	}
}

func containsString(values []string, value string) bool {
	needle := strings.ToLower(strings.TrimSpace(value))
	if needle == "" {
		return false
	}
	for _, candidate := range values {
		if strings.EqualFold(strings.TrimSpace(candidate), needle) {
			return true
		}
	}
	return false
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
		"Shift+Enter            insert newline",
		"Tab                    autocomplete selected slash command",
		"Shift+Tab              move slash-command selection backward",
		"Up/Down                navigate slash menu; when hidden, navigate prompt history",
		"Ctrl+P / Ctrl+N        navigate prompt history",
		"Alt+Up / Alt+Down      history navigation aliases",
		"PgUp / PgDn            scroll transcript viewport by page",
		"Ctrl+Up / Ctrl+Down    scroll transcript viewport by line",
		"Mouse wheel            scroll transcript viewport",
		"Esc                    abort active run",
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
