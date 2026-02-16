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
	cfg.State.Root = t.TempDir()
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
	cfg.State.Root = t.TempDir()
	cfg.Agents.Defaults.Workspace = "."
	cfg.Session.Store = "agents/{agentId}/sessions/sessions.json"
	cfg.Agents.Defaults.MemorySearch.Enabled = true
	cfg.Agents.Defaults.MemorySearch.Sources = []string{"memory"}
	cfg.Agents.Defaults.MemorySearch.Provider = "none"
	cfg.Agents.Defaults.MemorySearch.Fallback = "none"
	cfg.Agents.Defaults.MemorySearch.Store.Path = filepath.Join("memory", "{agentId}.sqlite")
	cfg.Agents.Defaults.MemorySearch.Store.Vector.Enabled = false
	cfg.Agents.Defaults.MemorySearch.Cache.Enabled = false
	cfg.Agents.Defaults.MemorySearch.Query.Hybrid.Enabled = false
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

func TestVerboseRunStartDiagnosticsShowPromptAndRuntimeState(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.verbose = true

	m.emitVerboseRunStartDiagnostics("hello\nworld")
	all := joinedMessageRaw(m.messages)

	if !strings.Contains(all, "[verbose] prompt: chars=11 lines=2 session=default/main") {
		t.Fatalf("expected verbose prompt summary, got %q", all)
	}
	if !strings.Contains(all, "[verbose] runtime: unavailable") {
		t.Fatalf("expected verbose runtime summary, got %q", all)
	}
}

func TestVerboseStreamSummaryIncludesDeltaAndFinalStats(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.verbose = true
	m.running = true
	m.activeRunID = 17
	m.status = statusWaiting

	updated, _ := m.Update(streamEventMsg{
		RunID: 17,
		OK:    true,
		Event: llm.StreamEvent{Type: llm.StreamEventDelta, Text: "abc"},
	})
	m = updated.(model)
	updated, _ = m.Update(streamEventMsg{
		RunID: 17,
		OK:    true,
		Event: llm.StreamEvent{Type: llm.StreamEventFinal, Text: "done"},
	})
	m = updated.(model)

	all := joinedMessageRaw(m.messages)
	if !strings.Contains(all, "[verbose] stream: first delta received") {
		t.Fatalf("expected verbose first-delta message, got %q", all)
	}
	if !strings.Contains(all, "[verbose] stream: final received") {
		t.Fatalf("expected verbose final summary, got %q", all)
	}
	if !strings.Contains(all, "delta_events=1") || !strings.Contains(all, "delta_chars=3") {
		t.Fatalf("expected verbose stream counters, got %q", all)
	}
}

func TestVerboseToolCallShowsDetailedMetadata(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.verbose = true
	m.running = true
	m.activeRunID = 7
	m.status = statusWaiting

	updated, _ := m.Update(streamEventMsg{
		RunID: 7,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventToolCall,
			ToolCall: &llm.ToolCall{
				ID:    "call-1",
				Name:  "memory_search",
				Class: llm.ToolClassLocal,
				Args: map[string]interface{}{
					"query":       "incident summary",
					"max_results": 3,
				},
			},
		},
	})
	m = updated.(model)

	all := joinedMessageRaw(m.messages)
	if !strings.Contains(all, "[verbose] tool call details: id=call-1 class=local") {
		t.Fatalf("expected verbose tool call details, got %q", all)
	}
	if !strings.Contains(all, "query=incident summary") || !strings.Contains(all, "max_results=3") {
		t.Fatalf("expected verbose tool args summary, got %q", all)
	}
}

func TestVerboseToolResultShowsDetailedMetadata(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.verbose = true
	m.running = true
	m.activeRunID = 7
	m.status = "tool memory_search"

	updated, _ := m.Update(streamEventMsg{
		RunID: 7,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventToolResult,
			ToolResult: &llm.ToolResult{
				CallID: "call-1",
				Tool:   "memory_search",
				OK:     true,
				Status: "completed",
				Data: map[string]interface{}{
					"count": 2,
				},
			},
		},
	})
	m = updated.(model)

	all := joinedMessageRaw(m.messages)
	if !strings.Contains(all, "[verbose] tool result details: call_id=call-1 tool=memory_search ok=true status=completed") {
		t.Fatalf("expected verbose tool result details, got %q", all)
	}
	if !strings.Contains(all, "data_keys=count") {
		t.Fatalf("expected verbose tool result data key summary, got %q", all)
	}
}

func TestVerboseProviderMetadataShowsDetails(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.verbose = true
	m.running = true
	m.activeRunID = 22

	updated, _ := m.Update(streamEventMsg{
		RunID: 22,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventProviderMetadata,
			ProviderMetadata: &llm.ProviderMetadata{
				Provider: "claudecode",
				Model:    "claude-opus-4-6",
				Tools:    []string{"Bash", "WebFetch"},
			},
		},
	})
	m = updated.(model)

	all := joinedMessageRaw(m.messages)
	if !strings.Contains(all, "[verbose] provider metadata: provider=claudecode model=claude-opus-4-6 tools=Bash, WebFetch") {
		t.Fatalf("expected verbose provider metadata summary, got %q", all)
	}
}

func TestResolveThinkingMessagesFallsBackToThinking(t *testing.T) {
	got := resolveThinkingMessages(nil)
	if len(got) != 1 || got[0] != "thinking" {
		t.Fatalf("expected fallback thinking messages [thinking], got %v", got)
	}
}

func TestNextThinkingMessageRotatesConfiguredMessages(t *testing.T) {
	cfg := config.Default()
	cfg.App.ThinkingMessages = []string{"thinking", "checking memory"}
	m := newModel(context.Background(), nil, cfg)

	if got := m.nextThinkingMessage(); got != "thinking" {
		t.Fatalf("expected first message thinking, got %q", got)
	}
	if got := m.nextThinkingMessage(); got != "checking memory" {
		t.Fatalf("expected second message checking memory, got %q", got)
	}
	if got := m.nextThinkingMessage(); got != "thinking" {
		t.Fatalf("expected third message to wrap to thinking, got %q", got)
	}
}

func TestStatusViewShowsCurrentThinkingMessageWhileWaiting(t *testing.T) {
	cfg := config.Default()
	cfg.App.ThinkingMessages = []string{"checking memory"}
	m := newModel(context.Background(), nil, cfg)
	m.width = 120
	m.status = statusWaiting
	m.running = true
	m.hasStreamDelta = false
	m.activeThinkingMessage = "checking memory"

	got := m.statusView()
	if !strings.Contains(got, "checking memory") {
		t.Fatalf("expected status line to include current thinking message, got %q", got)
	}
}

func TestToolCallEventSurfacesToolActivity(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.running = true
	m.activeRunID = 7
	m.status = statusWaiting

	updated, _ := m.Update(streamEventMsg{
		RunID: 7,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventToolCall,
			ToolCall: &llm.ToolCall{
				Name:  "memory_search",
				Class: llm.ToolClassLocal,
			},
		},
	})
	next := updated.(model)

	if !strings.Contains(next.status, "tool") {
		t.Fatalf("expected status to reflect tool activity, got %q", next.status)
	}
	if got := countToolCards(next.messages); got != 1 {
		t.Fatalf("expected tool call event to append one tool card, got %d", got)
	}
	card := latestToolCard(next.messages)
	if card == nil {
		t.Fatalf("expected tool card payload after tool call")
	}
	if card.ToolName != "memory_search" {
		t.Fatalf("expected tool card tool name memory_search, got %q", card.ToolName)
	}
	if card.Ownership != "localclaw_mcp" {
		t.Fatalf("expected tool card ownership localclaw_mcp, got %q", card.Ownership)
	}
	if card.HasResult {
		t.Fatalf("expected tool call card to remain running before result")
	}
	rendered := ansiEscapePattern.ReplaceAllString(next.renderTranscript(), "")
	if !strings.Contains(rendered, "tool [localclaw_mcp] memory_search") {
		t.Fatalf("expected rendered card summary, got %q", rendered)
	}
	if !strings.Contains(rendered, "running") {
		t.Fatalf("expected running status in rendered card, got %q", rendered)
	}
}

func TestToolResultEventReturnsToWaitingState(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.running = true
	m.activeRunID = 7
	m.status = "tool memory_search"

	updated, _ := m.Update(streamEventMsg{
		RunID: 7,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventToolResult,
			ToolResult: &llm.ToolResult{
				Tool:  "memory_search",
				Class: llm.ToolClassDelegated,
				OK:    true,
			},
		},
	})
	next := updated.(model)

	if next.status != statusWaiting {
		t.Fatalf("expected tool result to return status to waiting, got %q", next.status)
	}
	if got := countToolCards(next.messages); got != 1 {
		t.Fatalf("expected tool result event to append one tool card, got %d", got)
	}
	card := latestToolCard(next.messages)
	if card == nil {
		t.Fatalf("expected tool card payload after tool result")
	}
	if card.ToolName != "memory_search" {
		t.Fatalf("expected tool card tool name memory_search, got %q", card.ToolName)
	}
	if card.Ownership != "provider_native" {
		t.Fatalf("expected tool card ownership provider_native, got %q", card.Ownership)
	}
	if !card.HasResult {
		t.Fatalf("expected result card to be marked complete")
	}
	rendered := ansiEscapePattern.ReplaceAllString(next.renderTranscript(), "")
	if !strings.Contains(rendered, "completed") {
		t.Fatalf("expected completed status in rendered card, got %q", rendered)
	}
}

func TestToolResultEventUsesCallOwnershipWhenResultClassMissing(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.running = true
	m.activeRunID = 7
	m.status = statusWaiting

	updated, _ := m.Update(streamEventMsg{
		RunID: 7,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventToolCall,
			ToolCall: &llm.ToolCall{
				ID:    "call-123",
				Name:  "Bash",
				Class: llm.ToolClassDelegated,
			},
		},
	})
	m = updated.(model)

	updated, _ = m.Update(streamEventMsg{
		RunID: 7,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventToolResult,
			ToolResult: &llm.ToolResult{
				CallID: "call-123",
				Tool:   "Bash",
				OK:     true,
			},
		},
	})
	next := updated.(model)
	if got := countToolCards(next.messages); got != 1 {
		t.Fatalf("expected call/result pair to render as one card, got %d cards", got)
	}
	card := latestToolCard(next.messages)
	if card == nil {
		t.Fatalf("expected tool card payload after call/result pair")
	}
	if card.Ownership != "provider_native" {
		t.Fatalf("expected ownership to resolve from prior call id, got %q", card.Ownership)
	}
}

func TestToolResultEventWithoutOwnershipShowsUnspecified(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.running = true
	m.activeRunID = 7
	m.status = statusWaiting

	updated, _ := m.Update(streamEventMsg{
		RunID: 7,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventToolResult,
			ToolResult: &llm.ToolResult{
				Tool: "mystery_tool",
				OK:   true,
			},
		},
	})
	next := updated.(model)
	card := latestToolCard(next.messages)
	if card == nil {
		t.Fatalf("expected tool card payload for ownership fallback")
	}
	if card.Ownership != "unspecified" {
		t.Fatalf("expected unknown ownership label, got %q", card.Ownership)
	}
}

func TestToolCardsExpandWithCtrlO(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.running = true
	m.activeRunID = 7
	m.status = statusWaiting

	updated, _ := m.Update(streamEventMsg{
		RunID: 7,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventToolCall,
			ToolCall: &llm.ToolCall{
				ID:    "call-1",
				Name:  "memory_search",
				Class: llm.ToolClassLocal,
				Args: map[string]interface{}{
					"query": "incident summary",
				},
			},
		},
	})
	m = updated.(model)
	updated, _ = m.Update(streamEventMsg{
		RunID: 7,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventToolResult,
			ToolResult: &llm.ToolResult{
				CallID: "call-1",
				Tool:   "memory_search",
				Class:  llm.ToolClassLocal,
				OK:     true,
				Status: "completed",
				Data: map[string]interface{}{
					"count": 2,
				},
			},
		},
	})
	m = updated.(model)

	collapsed := ansiEscapePattern.ReplaceAllString(m.renderTranscript(), "")
	if strings.Contains(collapsed, "arg.query: incident summary") {
		t.Fatalf("expected collapsed tool card to omit args, got %q", collapsed)
	}
	if strings.Contains(collapsed, "data.count: 2") {
		t.Fatalf("expected collapsed tool card to omit result data, got %q", collapsed)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	next := updated.(model)
	if !next.toolsExpanded {
		t.Fatalf("expected ctrl+o to expand tool cards")
	}
	expanded := ansiEscapePattern.ReplaceAllString(next.renderTranscript(), "")
	if !strings.Contains(expanded, "arg.query: incident summary") {
		t.Fatalf("expected expanded tool card to show args, got %q", expanded)
	}
	if !strings.Contains(expanded, "data.count: 2") {
		t.Fatalf("expected expanded tool card to show result data, got %q", expanded)
	}
}

func TestExpandedToolCardFormatsContentAndHidesProviderResult(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.running = true
	m.activeRunID = 7
	m.status = statusWaiting
	m.toolsExpanded = true

	longTail := strings.Repeat("x", 220) + "TAIL_MARKER"
	contentJSON := `{"count":1,"ok":true,"results":[{"snippet":"` + longTail + `"}]}`

	updated, _ := m.Update(streamEventMsg{
		RunID: 7,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventToolCall,
			ToolCall: &llm.ToolCall{
				ID:    "call-content",
				Name:  "memory_search",
				Class: llm.ToolClassLocal,
			},
		},
	})
	m = updated.(model)

	updated, _ = m.Update(streamEventMsg{
		RunID: 7,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventToolResult,
			ToolResult: &llm.ToolResult{
				CallID: "call-content",
				Tool:   "memory_search",
				Class:  llm.ToolClassLocal,
				OK:     true,
				Status: "completed",
				Data: map[string]interface{}{
					"content":         contentJSON,
					"provider_result": map[string]interface{}{"content": contentJSON},
				},
			},
		},
	})
	next := updated.(model)

	rendered := ansiEscapePattern.ReplaceAllString(next.renderTranscript(), "")
	if strings.Contains(rendered, "data.provider_result:") {
		t.Fatalf("expected expanded card to hide provider_result, got %q", rendered)
	}
	if !strings.Contains(rendered, "data.content:") || !strings.Contains(rendered, "```json") {
		t.Fatalf("expected expanded card to render content as fenced JSON block, got %q", rendered)
	}
	if !strings.Contains(rendered, "\"count\": 1") {
		t.Fatalf("expected expanded card to pretty-print JSON content, got %q", rendered)
	}
	if !strings.Contains(rendered, "TAIL_MARKER") {
		t.Fatalf("expected expanded card content to avoid truncation, got %q", rendered)
	}
}

func TestExpandedToolCardShowsErrorForFailedResult(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.running = true
	m.activeRunID = 7
	m.status = statusWaiting
	m.toolsExpanded = true

	updated, _ := m.Update(streamEventMsg{
		RunID: 7,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventToolCall,
			ToolCall: &llm.ToolCall{
				ID:    "call-err",
				Name:  "Bash",
				Class: llm.ToolClassDelegated,
				Args: map[string]interface{}{
					"command": "ls denied",
				},
			},
		},
	})
	m = updated.(model)

	updated, _ = m.Update(streamEventMsg{
		RunID: 7,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventToolResult,
			ToolResult: &llm.ToolResult{
				CallID: "call-err",
				Tool:   "Bash",
				Class:  llm.ToolClassDelegated,
				OK:     false,
				Status: "error",
				Error:  "permission denied",
			},
		},
	})
	next := updated.(model)

	rendered := ansiEscapePattern.ReplaceAllString(next.renderTranscript(), "")
	if !strings.Contains(rendered, "tool [provider_native] Bash • failed") {
		t.Fatalf("expected failed summary in expanded card, got %q", rendered)
	}
	if !strings.Contains(rendered, "error: permission denied") {
		t.Fatalf("expected expanded card error text, got %q", rendered)
	}
}

func TestFinishRunClearsToolCallOwnershipCache(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.toolCallOwnershipByID["call-123"] = llm.ToolClassDelegated

	m.finishRun(statusIdle)

	if len(m.toolCallOwnershipByID) != 0 {
		t.Fatalf("expected finishRun to clear call ownership cache")
	}
}

func TestAbortRunClearsToolCallOwnershipCache(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.toolCallOwnershipByID["call-123"] = llm.ToolClassDelegated

	m.abortRun("aborted")

	if len(m.toolCallOwnershipByID) != 0 {
		t.Fatalf("expected abortRun to clear call ownership cache")
	}
}

func TestNewProgramEnablesMouseCellMotion(t *testing.T) {
	got := startupOptionBits(t, newProgram(newModel(context.Background(), nil, config.Default())))

	expectedProgram := tea.NewProgram(nil)
	tea.WithAltScreen()(expectedProgram)
	tea.WithMouseCellMotion()(expectedProgram)
	expected := startupOptionBits(t, expectedProgram)

	if got != expected {
		t.Fatalf("unexpected startup options: got=%d want=%d", got, expected)
	}
}
