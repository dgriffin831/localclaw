package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/charmbracelet/glamour"

	"github.com/dgriffin831/localclaw/internal/llm"
)

func (m *model) loadWelcomeMessage() string {
	if strings.TrimSpace(m.workspacePath) == "" {
		return ""
	}
	content, err := os.ReadFile(filepath.Join(m.workspacePath, welcomeFileName))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(content))
}

func (m *model) addSystem(text string) {
	m.messages = append(m.messages, chatMessage{Role: roleSystem, Raw: text})
}

func (m *model) addSystemMarkdown(text string) {
	m.messages = append(m.messages, chatMessage{
		Role:           roleSystem,
		Raw:            text,
		RenderMarkdown: true,
	})
}

func (m *model) addUser(text string) {
	m.messages = append(m.messages, chatMessage{Role: roleUser, Raw: text})
}

func (m *model) addAssistant(text string, thinkingPlaceholder bool) {
	m.messages = append(m.messages, chatMessage{
		Role:                roleAssistant,
		Raw:                 text,
		Streaming:           true,
		ThinkingPlaceholder: thinkingPlaceholder,
	})
	m.activeAssistantIdx = len(m.messages) - 1
}

func (m *model) recordToolCallCard(callID, toolName string, args map[string]interface{}) {
	card := &toolCardMessage{
		CallID:   strings.TrimSpace(callID),
		ToolName: valueOrDefault(strings.TrimSpace(toolName), "tool"),
		Args:     copyInterfaceMap(args),
	}
	idx := m.appendToolCard(card)
	if card.CallID != "" {
		m.toolCardIndexByCallID[card.CallID] = idx
	}
}

func (m *model) recordToolResultCard(callID, toolName string, result *llm.ToolResult) {
	if result == nil {
		return
	}
	targetIdx := -1
	trimmedCallID := strings.TrimSpace(callID)
	if trimmedCallID != "" {
		if idx, ok := m.toolCardIndexByCallID[trimmedCallID]; ok && idx >= 0 && idx < len(m.messages) && m.messages[idx].ToolCard != nil {
			targetIdx = idx
		}
		delete(m.toolCardIndexByCallID, trimmedCallID)
	}
	if targetIdx == -1 {
		targetIdx = m.findOpenToolCardIndex(toolName)
	}
	if targetIdx == -1 {
		targetIdx = m.appendToolCard(&toolCardMessage{})
	}
	card := m.messages[targetIdx].ToolCard
	if card == nil {
		return
	}
	if card.CallID == "" {
		card.CallID = trimmedCallID
	}
	if strings.TrimSpace(toolName) != "" {
		card.ToolName = strings.TrimSpace(toolName)
	}
	if strings.TrimSpace(card.ToolName) == "" {
		card.ToolName = "tool"
	}
	card.HasResult = true
	card.OK = result.OK
	card.Status = strings.TrimSpace(result.Status)
	card.Error = strings.TrimSpace(result.Error)
	card.Data = copyInterfaceMap(result.Data)
}

func (m *model) appendToolCard(card *toolCardMessage) int {
	if card == nil {
		card = &toolCardMessage{}
	}
	m.messages = append(m.messages, chatMessage{
		Role:     roleSystem,
		ToolCard: card,
	})
	return len(m.messages) - 1
}

func (m *model) findOpenToolCardIndex(toolName string) int {
	targetName := strings.TrimSpace(toolName)
	for idx := len(m.messages) - 1; idx >= 0; idx-- {
		card := m.messages[idx].ToolCard
		if card == nil || card.HasResult {
			continue
		}
		if targetName == "" || strings.EqualFold(strings.TrimSpace(card.ToolName), targetName) {
			return idx
		}
	}
	return -1
}

func (m *model) refreshViewport(forceBottom bool) {
	if m.viewport.Width <= 0 {
		return
	}

	atBottom := forceBottom || m.viewport.AtBottom()
	content := m.renderTranscript()
	m.viewport.SetContent(content)
	if atBottom {
		m.viewport.GotoBottom()
	}
}

func (m *model) renderTranscript() string {
	if len(m.messages) == 0 {
		return ""
	}

	contentWidth := m.viewport.Width - 6
	if contentWidth < 20 {
		contentWidth = 20
	}

	blocks := make([]string, 0, len(m.messages))
	for _, msg := range m.messages {
		text := strings.TrimSpace(msg.Raw)
		if text == "" && msg.ToolCard == nil {
			continue
		}

		switch msg.Role {
		case roleSystem:
			if msg.ToolCard != nil {
				blocks = append(blocks, systemStyle.Render(m.renderToolCard(msg.ToolCard, m.toolsExpanded)))
			} else if msg.RenderMarkdown {
				rendered := m.renderMarkdown(text, contentWidth-3)
				blocks = append(blocks, systemStyle.Render(rendered))
			} else {
				blocks = append(blocks, systemStyle.Render(text))
			}
		case roleUser:
			rendered := m.renderMarkdown(text, contentWidth-4)
			blocks = append(blocks, userStyle.Width(contentWidth).Render(rendered))
		case roleAssistant:
			rendered := m.renderMarkdown(text, contentWidth-3)
			blocks = append(blocks, assistantStyle.Width(contentWidth).Render(rendered))
		}
	}

	return strings.Join(blocks, "\n\n")
}

func (m *model) renderToolCard(card *toolCardMessage, expanded bool) string {
	if card == nil {
		return ""
	}
	toolName := valueOrDefault(strings.TrimSpace(card.ToolName), "tool")
	status := resolveToolCardStatus(card)
	header := fmt.Sprintf("tool %s • %s", toolName, status)
	if !expanded {
		return header
	}
	lines := []string{
		header,
		"call_id: " + valueOrDefault(strings.TrimSpace(card.CallID), "n/a"),
	}
	argLines := formatToolCardMap("arg.", card.Args)
	if len(argLines) == 0 {
		lines = append(lines, "arg: none")
	} else {
		lines = append(lines, argLines...)
	}
	lines = append(lines, "status: "+status)
	if card.HasResult {
		if strings.TrimSpace(card.Error) != "" {
			lines = append(lines, "error: "+truncateToolCardText(card.Error))
		}
		dataLines := formatToolCardData(card.ToolName, card.Args, card.Data)
		if len(dataLines) == 0 {
			lines = append(lines, "data: none")
		} else {
			lines = append(lines, dataLines...)
		}
	}
	return strings.Join(lines, "\n")
}

func resolveToolCardStatus(card *toolCardMessage) string {
	if card == nil || !card.HasResult {
		return "running"
	}
	switch strings.ToLower(strings.TrimSpace(card.Status)) {
	case "completed":
		return "completed"
	case "blocked":
		return "blocked"
	case "failed", "error", "cancelled", "canceled":
		return "failed"
	}
	if card.OK {
		return "completed"
	}
	return "failed"
}

func copyInterfaceMap(values map[string]interface{}) map[string]interface{} {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func formatToolCardMap(prefix string, values map[string]interface{}) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, formatToolCardValue(prefix+key, values[key])...)
	}
	return lines
}

func formatToolCardData(toolName string, args map[string]interface{}, values map[string]interface{}) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		if key == "provider_result" {
			continue
		}
		if shouldSkipToolCardDataKey(toolName, args, key, values[key]) {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, formatToolCardValue("data."+key, values[key])...)
	}
	return lines
}

func formatToolCardValue(label string, raw interface{}) []string {
	language, content, block := formatToolCardContent(raw)
	if !block {
		return []string{fmt.Sprintf("%s: %s", label, content)}
	}
	lines := []string{label + ":"}
	fence := "```"
	if language != "" {
		fence += language
	}
	lines = append(lines, fence)
	if content == "" {
		lines = append(lines, "(empty)")
	} else {
		lines = append(lines, content)
	}
	lines = append(lines, "```")
	return lines
}

func formatToolCardContent(raw interface{}) (language string, content string, block bool) {
	switch value := raw.(type) {
	case nil:
		return "", "(empty)", false
	case string:
		if pretty, ok := prettyToolCardJSON(value); ok {
			return "json", pretty, true
		}
		trimmed := strings.TrimRight(value, "\n")
		if shouldRenderToolCardBlock(trimmed) {
			return "text", trimmed, true
		}
		return "", truncateToolCardText(trimmed), false
	default:
		if isStructuredToolCardValue(value) {
			if marshaled, err := json.MarshalIndent(value, "", "  "); err == nil {
				return "json", string(marshaled), true
			}
		}
		text := fmt.Sprint(value)
		if pretty, ok := prettyToolCardJSON(text); ok {
			return "json", pretty, true
		}
		trimmed := strings.TrimRight(text, "\n")
		if shouldRenderToolCardBlock(trimmed) {
			return "text", trimmed, true
		}
		return "", truncateToolCardText(trimmed), false
	}
}

func prettyToolCardJSON(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || (trimmed[0] != '{' && trimmed[0] != '[') || !json.Valid([]byte(trimmed)) {
		return "", false
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(trimmed), "", "  "); err != nil {
		return "", false
	}
	return buf.String(), true
}

func shouldSkipToolCardDataKey(toolName string, args map[string]interface{}, key string, value interface{}) bool {
	if key == "tool" && strings.EqualFold(strings.TrimSpace(fmt.Sprint(value)), strings.TrimSpace(toolName)) {
		return true
	}
	if key == "arguments" && len(args) > 0 && reflect.DeepEqual(value, args) {
		return true
	}
	if len(args) == 0 {
		return false
	}
	argValue, ok := args[key]
	if !ok {
		return false
	}
	return reflect.DeepEqual(argValue, value)
}

func shouldRenderToolCardBlock(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	return strings.Contains(trimmed, "\n") || len(trimmed) > 180
}

func isStructuredToolCardValue(value interface{}) bool {
	typed := reflect.TypeOf(value)
	if typed == nil {
		return false
	}
	switch typed.Kind() {
	case reflect.Map, reflect.Slice, reflect.Array:
		return true
	default:
		return false
	}
}

func truncateToolCardText(value string) string {
	normalized := strings.TrimSpace(strings.Join(strings.Fields(value), " "))
	if normalized == "" {
		return "(empty)"
	}
	if len(normalized) <= 180 {
		return normalized
	}
	return normalized[:177] + "..."
}

func (m *model) renderMarkdown(input string, width int) string {
	if strings.TrimSpace(input) == "" {
		return ""
	}
	if width < 20 {
		width = 20
	}

	if m.renderer == nil || m.rendererWidth != width {
		renderer, err := glamour.NewTermRenderer(
			glamour.WithWordWrap(width),
			glamour.WithStyles(localclawMarkdownStyles()),
		)
		if err != nil {
			return input
		}
		m.renderer = renderer
		m.rendererWidth = width
	}

	out, err := m.renderer.Render(input)
	if err != nil {
		return input
	}
	return strings.TrimRight(out, "\n")
}
