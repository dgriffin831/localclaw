package tui

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/llm"
	"github.com/dgriffin831/localclaw/internal/runtime"
)

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func joinedMessageRaw(messages []chatMessage) string {
	parts := make([]string, 0, len(messages))
	for _, msg := range messages {
		parts = append(parts, msg.Raw)
	}
	return strings.Join(parts, "\n")
}

func countToolCards(messages []chatMessage) int {
	count := 0
	for _, msg := range messages {
		if msg.ToolCard != nil {
			count++
		}
	}
	return count
}

func latestToolCard(messages []chatMessage) *toolCardMessage {
	for idx := len(messages) - 1; idx >= 0; idx-- {
		if messages[idx].ToolCard != nil {
			return messages[idx].ToolCard
		}
	}
	return nil
}

func startupOptionBits(t *testing.T, p *tea.Program) uint64 {
	t.Helper()
	field := reflect.ValueOf(p).Elem().FieldByName("startupOptions")
	if !field.IsValid() {
		t.Fatalf("bubbletea program missing startupOptions field")
	}
	return field.Uint()
}

func TestParseSlash(t *testing.T) {
	name, arg := parseSlash("/thinking on")
	if name != "thinking" || arg != "on" {
		t.Fatalf("unexpected parse result: name=%q arg=%q", name, arg)
	}

	name, arg = parseSlash("/status")
	if name != "status" || arg != "" {
		t.Fatalf("unexpected parse result for no-arg command: name=%q arg=%q", name, arg)
	}
}

func TestFormatElapsed(t *testing.T) {
	if got := formatElapsed(500 * time.Millisecond); got != "0s" {
		t.Fatalf("expected 0s, got %q", got)
	}
	if got := formatElapsed(15 * time.Second); got != "15s" {
		t.Fatalf("expected 15s, got %q", got)
	}
	if got := formatElapsed(125 * time.Second); got != "2m5s" {
		t.Fatalf("expected 2m5s, got %q", got)
	}
}

func TestInputAcceptsTyping(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	next := updated.(model)

	if got := next.input.Value(); got != "h" {
		t.Fatalf("expected typed value to be captured, got %q", got)
	}
}

func TestInputInsertNewlineUsesCtrlJ(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())

	got := m.input.KeyMap.InsertNewline.Keys()
	if !reflect.DeepEqual(got, []string{"ctrl+j"}) {
		t.Fatalf("expected insert newline binding [ctrl+j], got %v", got)
	}
}

func TestCtrlJInsertsNewlineInsteadOfSubmitting(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	next := updated.(model)
	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	next = updated.(model)

	if got := next.input.Value(); got != "h\n" {
		t.Fatalf("expected ctrl+j to insert newline, got %q", got)
	}
}

func TestInputHintMentionsCtrlJ(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.width = 120

	hint := m.inputView()
	if !strings.Contains(hint, "Ctrl+J newline") {
		t.Fatalf("expected input hint to mention Ctrl+J newline, got %q", hint)
	}
	if !strings.Contains(hint, "Ctrl+Y mouse") {
		t.Fatalf("expected input hint to mention Ctrl+Y mouse toggle, got %q", hint)
	}
	if !strings.Contains(hint, "Ctrl+O tools") {
		t.Fatalf("expected input hint to mention Ctrl+O tools toggle, got %q", hint)
	}
	if !strings.Contains(hint, "Ctrl+T thinking") {
		t.Fatalf("expected input hint to mention Ctrl+T thinking toggle, got %q", hint)
	}
	if !strings.Contains(hint, "/shortcuts") {
		t.Fatalf("expected input hint to mention /shortcuts, got %q", hint)
	}
	if strings.Contains(hint, "Enter send") {
		t.Fatalf("expected input hint not to mention Enter send, got %q", hint)
	}
	if strings.Contains(hint, "/help") {
		t.Fatalf("expected input hint not to mention /help, got %q", hint)
	}
}

func TestViewDoesNotOverflowHeight(t *testing.T) {
	cfg := config.Default()
	cfg.Agents.Defaults.Workspace = "/Users/dennis/Documents/GitHub/localclaw/very/long/path/that/used/to/wrap"

	m := newModel(context.Background(), nil, cfg)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	next := updated.(model)

	view := next.View()
	lines := strings.Count(view, "\n") + 1
	if lines > 24 {
		t.Fatalf("expected view lines to fit terminal height, got %d lines for height 24", lines)
	}
}

func TestHeaderUsesResolvedWorkspacePath(t *testing.T) {
	cfg := config.Default()
	cfg.App.Root = t.TempDir()
	cfg.Agents.Defaults.Workspace = "relative/workspace"
	cfg.Session.Store = "agents/{agentId}/sessions/sessions.json"

	app, err := runtime.New(cfg)
	if err != nil {
		t.Fatalf("new runtime app: %v", err)
	}
	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("run runtime app: %v", err)
	}

	m := newModel(context.Background(), app, cfg)
	m.width = 180
	header := m.headerView()
	resolvedWorkspace, err := app.ResolveWorkspacePath("")
	if err != nil {
		t.Fatalf("resolve workspace path: %v", err)
	}
	resolvedWorkspacePath := formatWorkspacePath(resolvedWorkspace)
	if !strings.Contains(header, resolvedWorkspacePath) {
		t.Fatalf("expected header to include resolved workspace %q", resolvedWorkspacePath)
	}
}

func TestHandleSlashResetClearsMessages(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.messages = append(m.messages, chatMessage{Role: roleUser, Raw: "hello"})

	_ = m.handleSlash("/reset")
	if len(m.messages) != 1 {
		t.Fatalf("expected reset to clear transcript and add one system message, got %d messages", len(m.messages))
	}
	if m.messages[0].Role != roleSystem {
		t.Fatalf("expected reset message role system, got %q", m.messages[0].Role)
	}
	if m.messages[0].Raw != "session reset" {
		t.Fatalf("unexpected reset system message %q", m.messages[0].Raw)
	}
}

func TestHandleSlashNewStartsNewSessionMessage(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())

	_ = m.handleSlash("/new")
	if len(m.messages) != 1 {
		t.Fatalf("expected one system message, got %d", len(m.messages))
	}
	if !strings.Contains(m.messages[0].Raw, "started new session") {
		t.Fatalf("unexpected /new system message %q", m.messages[0].Raw)
	}
}

func TestHandleSlashNewShowsWorkspaceWelcomeMessage(t *testing.T) {
	cfg := config.Default()
	workspacePath := t.TempDir()
	cfg.Agents.Defaults.Workspace = workspacePath

	welcomeContent := "# Welcome to localclaw\n\nhello from welcome"
	if err := os.WriteFile(filepath.Join(workspacePath, "WELCOME.md"), []byte(welcomeContent), 0o644); err != nil {
		t.Fatalf("write WELCOME.md: %v", err)
	}

	m := newModel(context.Background(), nil, cfg)
	_ = m.handleSlash("/new")

	if len(m.messages) != 2 {
		t.Fatalf("expected /new to add session + welcome messages, got %d", len(m.messages))
	}
	if !strings.Contains(m.messages[0].Raw, "started new session") {
		t.Fatalf("unexpected first /new message %q", m.messages[0].Raw)
	}
	if m.messages[1].Raw != welcomeContent {
		t.Fatalf("unexpected welcome message %q", m.messages[1].Raw)
	}
}

func TestNewModelShowsWorkspaceWelcomeMessage(t *testing.T) {
	cfg := config.Default()
	workspacePath := t.TempDir()
	cfg.Agents.Defaults.Workspace = workspacePath

	welcomeContent := "# Welcome to localclaw\n\nstartup welcome"
	if err := os.WriteFile(filepath.Join(workspacePath, "WELCOME.md"), []byte(welcomeContent), 0o644); err != nil {
		t.Fatalf("write WELCOME.md: %v", err)
	}

	m := newModel(context.Background(), nil, cfg)
	if len(m.messages) != 2 {
		t.Fatalf("expected startup to include ready + welcome messages, got %d", len(m.messages))
	}
	if m.messages[0].Raw != "localclaw ready. Type /help for commands." {
		t.Fatalf("unexpected startup message %q", m.messages[0].Raw)
	}
	if m.messages[1].Raw != welcomeContent {
		t.Fatalf("unexpected welcome startup message %q", m.messages[1].Raw)
	}
}

func TestWelcomeMessageRendersMarkdownInTranscript(t *testing.T) {
	cfg := config.Default()
	workspacePath := t.TempDir()
	cfg.Agents.Defaults.Workspace = workspacePath

	welcomeContent := "# Heading\n\n- item"
	if err := os.WriteFile(filepath.Join(workspacePath, "WELCOME.md"), []byte(welcomeContent), 0o644); err != nil {
		t.Fatalf("write WELCOME.md: %v", err)
	}

	m := newModel(context.Background(), nil, cfg)
	m.viewport.Width = 80
	rendered := m.renderTranscript()

	if !strings.Contains(rendered, "Heading") {
		t.Fatalf("expected rendered transcript to include heading text, got %q", rendered)
	}
	if strings.Contains(rendered, "# Heading") {
		t.Fatalf("expected markdown heading to be rendered, got raw markdown %q", rendered)
	}
	if strings.Contains(rendered, "- item") {
		t.Fatalf("expected markdown list to be rendered, got raw markdown %q", rendered)
	}
}

func TestWelcomeMessageRendersOrderedListMarkersWithSpacing(t *testing.T) {
	cfg := config.Default()
	workspacePath := t.TempDir()
	cfg.Agents.Defaults.Workspace = workspacePath

	welcomeContent := "# Welcome\n\n1. Run check\n2. Run tui"
	if err := os.WriteFile(filepath.Join(workspacePath, "WELCOME.md"), []byte(welcomeContent), 0o644); err != nil {
		t.Fatalf("write WELCOME.md: %v", err)
	}

	m := newModel(context.Background(), nil, cfg)
	m.viewport.Width = 80
	rendered := ansiEscapePattern.ReplaceAllString(m.renderTranscript(), "")

	if !strings.Contains(rendered, "1. Run check") {
		t.Fatalf("expected ordered list marker to include punctuation and spacing, got %q", rendered)
	}
	if strings.Contains(rendered, "1Run check") {
		t.Fatalf("expected ordered list marker not to run into text, got %q", rendered)
	}
}

func TestSlashAutocompleteTabCompletesCommand(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	next := updated.(model)
	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	next = updated.(model)
	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyTab})
	next = updated.(model)

	if got := next.input.Value(); got != "/help " {
		t.Fatalf("expected tab to complete /help, got %q", got)
	}
}

func TestSlashAutocompleteDownThenTabCompletesNextCommand(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.rememberHistory("prior prompt")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	next := updated.(model)
	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyDown})
	next = updated.(model)
	if got := next.input.Value(); got != "/" {
		t.Fatalf("expected down arrow to keep slash input when menu is visible, got %q", got)
	}
	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyTab})
	next = updated.(model)

	if got := next.input.Value(); got != "/status " {
		t.Fatalf("expected down+tab to complete /status, got %q", got)
	}
}

func TestHistoryNavigationUsesCtrlPAndCtrlN(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.rememberHistory("first")
	m.rememberHistory("second")
	m.input.SetValue("draft")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	next := updated.(model)
	if got := next.input.Value(); got != "second" {
		t.Fatalf("expected ctrl+p to load latest history, got %q", got)
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	next = updated.(model)
	if got := next.input.Value(); got != "first" {
		t.Fatalf("expected second ctrl+p to load older history, got %q", got)
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	next = updated.(model)
	if got := next.input.Value(); got != "second" {
		t.Fatalf("expected ctrl+n to move forward in history, got %q", got)
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	next = updated.(model)
	if got := next.input.Value(); got != "draft" {
		t.Fatalf("expected ctrl+n to restore draft after history end, got %q", got)
	}
}

func TestArrowKeysNavigateHistoryWhenSlashMenuClosed(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.rememberHistory("first")
	m.rememberHistory("second")
	m.input.SetValue("draft")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	next := updated.(model)
	if got := next.input.Value(); got != "second" {
		t.Fatalf("expected up arrow to load latest history, got %q", got)
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyUp})
	next = updated.(model)
	if got := next.input.Value(); got != "first" {
		t.Fatalf("expected second up arrow to load older history, got %q", got)
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyDown})
	next = updated.(model)
	if got := next.input.Value(); got != "second" {
		t.Fatalf("expected down arrow to move forward in history, got %q", got)
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyDown})
	next = updated.(model)
	if got := next.input.Value(); got != "draft" {
		t.Fatalf("expected down arrow to restore draft after history end, got %q", got)
	}
}

func TestArrowKeysDoNotStartHistoryFromEmptyDraft(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.rememberHistory("first")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	next := updated.(model)
	if got := next.input.Value(); got != "" {
		t.Fatalf("expected empty draft to remain unchanged, got %q", got)
	}
}

func TestCtrlYTogglesMouseCapture(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	if !m.mouseEnabled {
		t.Fatalf("expected mouse capture to start enabled")
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlY})
	next := updated.(model)
	if next.mouseEnabled {
		t.Fatalf("expected ctrl+y to disable mouse capture")
	}
	if cmd == nil {
		t.Fatalf("expected ctrl+y to emit disable-mouse command")
	}
	if got := reflect.TypeOf(cmd()).String(); got != "tea.disableMouseMsg" {
		t.Fatalf("expected disable mouse command type, got %s", got)
	}

	updated, cmd = next.Update(tea.KeyMsg{Type: tea.KeyCtrlY})
	next = updated.(model)
	if !next.mouseEnabled {
		t.Fatalf("expected second ctrl+y to enable mouse capture")
	}
	if cmd == nil {
		t.Fatalf("expected second ctrl+y to emit enable-mouse command")
	}
	if got := reflect.TypeOf(cmd()).String(); got != "tea.enableMouseCellMotionMsg" {
		t.Fatalf("expected enable mouse command type, got %s", got)
	}
}

func TestHandleSlashHelpShowsCommandDescriptions(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())

	_ = m.handleSlash("/help")
	if len(m.messages) == 0 {
		t.Fatalf("expected /help to add a system message")
	}
	got := m.messages[len(m.messages)-1].Raw
	if !strings.Contains(got, "/help") || !strings.Contains(got, "show this help") {
		t.Fatalf("expected /help output to include command descriptions, got %q", got)
	}
	if !strings.Contains(got, "/shortcuts") || !strings.Contains(got, "show keyboard shortcuts") {
		t.Fatalf("expected /help output to include /shortcuts command, got %q", got)
	}
}

func TestHandleSlashShortcutsShowsKeyboardShortcuts(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())

	_ = m.handleSlash("/shortcuts")
	if len(m.messages) == 0 {
		t.Fatalf("expected /shortcuts to add a system message")
	}
	got := m.messages[len(m.messages)-1].Raw
	if !strings.Contains(got, "keyboard shortcuts:") {
		t.Fatalf("expected /shortcuts output heading, got %q", got)
	}
	if !strings.Contains(got, "Ctrl+J") || !strings.Contains(got, "insert newline") {
		t.Fatalf("expected /shortcuts output to include Ctrl+J newline shortcut, got %q", got)
	}
	if !strings.Contains(got, "Ctrl+Y") || !strings.Contains(got, "toggle mouse capture") {
		t.Fatalf("expected /shortcuts output to include Ctrl+Y mouse shortcut, got %q", got)
	}
	if !strings.Contains(got, "Ctrl+O") || !strings.Contains(got, "toggle tool-card expansion") {
		t.Fatalf("expected /shortcuts output to include Ctrl+O tools shortcut, got %q", got)
	}
	if !strings.Contains(got, "Ctrl+T") || !strings.Contains(got, "toggle thinking visibility") {
		t.Fatalf("expected /shortcuts output to include Ctrl+T thinking shortcut, got %q", got)
	}
}

func TestSlashMenuShowsKeyboardShortcutColumnWhenAvailable(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.width = 160
	m.input.SetValue("/th")
	m.updateSlashAutocomplete()

	menu := ansiEscapePattern.ReplaceAllString(m.slashMenuView(), "")
	if !regexp.MustCompile(`(?m)/thinking <on\|off>\s+toggle thinking visibility\s+Ctrl\+T`).MatchString(menu) {
		t.Fatalf("expected slash menu /thinking row to include Ctrl+T shortcut column, got %q", menu)
	}

	m.input.SetValue("/st")
	m.updateSlashAutocomplete()
	menu = ansiEscapePattern.ReplaceAllString(m.slashMenuView(), "")
	if regexp.MustCompile(`(?m)/status\s+show current status and session info\s+Ctrl\+`).MatchString(menu) {
		t.Fatalf("expected /status row without shortcut not to render shortcut column text, got %q", menu)
	}
}

func TestHandleSlashMouseTogglesMouseCapture(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())

	cmd := m.handleSlash("/mouse off")
	if m.mouseEnabled {
		t.Fatalf("expected /mouse off to disable mouse capture")
	}
	if cmd == nil {
		t.Fatalf("expected /mouse off to return a disable command")
	}
	if got := reflect.TypeOf(cmd()).String(); got != "tea.disableMouseMsg" {
		t.Fatalf("expected disable mouse command type, got %s", got)
	}

	cmd = m.handleSlash("/mouse on")
	if !m.mouseEnabled {
		t.Fatalf("expected /mouse on to enable mouse capture")
	}
	if cmd == nil {
		t.Fatalf("expected /mouse on to return an enable command")
	}
	if got := reflect.TypeOf(cmd()).String(); got != "tea.enableMouseCellMotionMsg" {
		t.Fatalf("expected enable mouse command type, got %s", got)
	}
}

func TestHandleSlashToolsShowsProviderWhenRuntimeUnavailable(t *testing.T) {
	cfg := config.Default()
	m := newModel(context.Background(), nil, cfg)

	_ = m.handleSlash("/tools")
	if len(m.messages) == 0 {
		t.Fatalf("expected /tools to add a system message")
	}
	got := m.messages[len(m.messages)-1].Raw
	if !strings.Contains(got, "provider="+cfg.LLM.Provider) {
		t.Fatalf("expected /tools output to include provider, got %q", got)
	}
	if !strings.Contains(got, "provider_native:") {
		t.Fatalf("expected /tools output to include provider_native section, got %q", got)
	}
	if !strings.Contains(got, "localclaw_mcp:") {
		t.Fatalf("expected /tools output to include localclaw_mcp section, got %q", got)
	}
	if !strings.Contains(got, "- runtime unavailable") {
		t.Fatalf("expected /tools output to mention runtime availability, got %q", got)
	}
}

func TestHandleSlashToolsShowsLocalclawTools(t *testing.T) {
	cfg := config.Default()
	cfg.App.Root = t.TempDir()
	cfg.Agents.Defaults.Workspace = "."
	cfg.Session.Store = "agents/{agentId}/sessions/sessions.json"
	cfg.Agents.Defaults.Memory.Enabled = true
	cfg.Agents.Defaults.Memory.Tools.Get = true
	cfg.Agents.Defaults.Memory.Tools.Search = true
	cfg.Agents.Defaults.Memory.Tools.Grep = true
	cfg.Agents.Defaults.Memory.Sources = []string{"memory"}
	cfg.Agents.Defaults.Memory.Store.Path = filepath.Join("memory", "{agentId}.sqlite")
	cfg.Heartbeat.Enabled = false
	cfg.Cron.Enabled = false

	app, err := runtime.New(cfg)
	if err != nil {
		t.Fatalf("new runtime app: %v", err)
	}
	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("run runtime app: %v", err)
	}

	m := newModel(context.Background(), app, cfg)
	_ = m.handleSlash("/tools")
	if len(m.messages) == 0 {
		t.Fatalf("expected /tools to add a system message")
	}
	got := m.messages[len(m.messages)-1].Raw
	if !strings.Contains(got, "provider="+cfg.LLM.Provider) {
		t.Fatalf("expected /tools output to include provider, got %q", got)
	}
	if !strings.Contains(got, "provider_native:") || !strings.Contains(got, "localclaw_mcp:") {
		t.Fatalf("expected /tools output to include ownership split sections, got %q", got)
	}
	if !strings.Contains(got, "memory_search") {
		t.Fatalf("expected /tools output to include memory_search, got %q", got)
	}
	if !strings.Contains(got, "memory_get") {
		t.Fatalf("expected /tools output to include memory_get, got %q", got)
	}
}

func TestHandleSlashToolsShowsDiscoveredProviderTools(t *testing.T) {
	cfg := config.Default()
	m := newModel(context.Background(), nil, cfg)
	m.activeRunID = 9

	updated, _ := m.Update(streamEventMsg{
		RunID: 9,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventProviderMetadata,
			ProviderMetadata: &llm.ProviderMetadata{
				Provider: "claudecode",
				Tools:    []string{"Bash", "WebFetch", "Task"},
			},
		},
	})
	m = updated.(model)
	_ = m.handleSlash("/tools")

	got := m.messages[len(m.messages)-1].Raw
	if !strings.Contains(got, "provider_native:") {
		t.Fatalf("expected provider_native section in /tools output, got %q", got)
	}
	if !strings.Contains(got, "- Bash, Task, WebFetch") {
		t.Fatalf("expected discovered provider tools in /tools output, got %q", got)
	}
}
