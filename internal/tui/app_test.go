package tui

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/runtime"
)

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

func TestViewDoesNotOverflowHeight(t *testing.T) {
	cfg := config.Default()
	cfg.Workspace.Root = "/Users/dennis/Documents/GitHub/localclaw/very/long/path/that/used/to/wrap"

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
	cfg.Workspace.Root = "/tmp/stubbed-workspace"
	cfg.Agents.Defaults.Workspace = "."
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
	resolvedWorkspacePath := formatWorkspacePath(filepath.Join(cfg.State.Root, "workspace"))
	if !strings.Contains(header, resolvedWorkspacePath) {
		t.Fatalf("expected header to include resolved workspace %q", resolvedWorkspacePath)
	}
	if strings.Contains(header, formatWorkspacePath(cfg.Workspace.Root)) {
		t.Fatalf("expected header not to use stubbed workspace root %q", cfg.Workspace.Root)
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
	cfg.Workspace.Root = t.TempDir()

	welcomeContent := "# Welcome to localclaw\n\nhello from welcome"
	if err := os.WriteFile(filepath.Join(cfg.Workspace.Root, "WELCOME.md"), []byte(welcomeContent), 0o644); err != nil {
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
	cfg.Workspace.Root = t.TempDir()

	welcomeContent := "# Welcome to localclaw\n\nstartup welcome"
	if err := os.WriteFile(filepath.Join(cfg.Workspace.Root, "WELCOME.md"), []byte(welcomeContent), 0o644); err != nil {
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
	cfg.Workspace.Root = t.TempDir()

	welcomeContent := "# Heading\n\n- item"
	if err := os.WriteFile(filepath.Join(cfg.Workspace.Root, "WELCOME.md"), []byte(welcomeContent), 0o644); err != nil {
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

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	next := updated.(model)
	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyDown})
	next = updated.(model)
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

func TestUpArrowDoesNotTriggerHistoryNavigation(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.rememberHistory("first")
	m.input.SetValue("draft")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	next := updated.(model)
	if got := next.input.Value(); got != "draft" {
		t.Fatalf("expected up arrow to keep input unchanged, got %q", got)
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
