package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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

func cmdEmitsBootstrapSeedTrigger(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	msg := cmd()
	switch msg.(type) {
	case bootstrapSeedTriggerMsg:
		return true
	}
	value := reflect.ValueOf(msg)
	if value.IsValid() && value.Kind() == reflect.Slice {
		for i := 0; i < value.Len(); i++ {
			nested, ok := value.Index(i).Interface().(tea.Cmd)
			if !ok {
				continue
			}
			if cmdEmitsBootstrapSeedTrigger(nested) {
				return true
			}
		}
	}
	return false
}

func newRuntimeBackedModel(t *testing.T) (model, *runtime.App) {
	t.Helper()

	cfg := config.Default()
	cfg.App.Root = t.TempDir()
	cfg.Agents.Defaults.Workspace = t.TempDir()
	cfg.Session.Store = "agents/{agentId}/sessions/sessions.json"
	cfg.Heartbeat.Enabled = false
	cfg.Cron.Enabled = false

	app, err := runtime.New(cfg)
	if err != nil {
		t.Fatalf("new runtime app: %v", err)
	}
	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("run runtime app: %v", err)
	}
	return newModel(context.Background(), app, cfg), app
}

func TestParseSlash(t *testing.T) {
	name, arg := parseSlash("/mouse on")
	if name != "mouse" || arg != "on" {
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

func TestStatusRowsUseBlackBackgroundAndWhiteText(t *testing.T) {
	if got := fmt.Sprint(statusIdleStyle.GetForeground()); got != fmt.Sprint(colorText) {
		t.Fatalf("expected idle status foreground %v, got %v", colorText, statusIdleStyle.GetForeground())
	}
	if got := fmt.Sprint(statusIdleStyle.GetBackground()); got != fmt.Sprint(colorBackground) {
		t.Fatalf("expected idle status background %v, got %v", colorBackground, statusIdleStyle.GetBackground())
	}
	if got := fmt.Sprint(statusBusyStyle.GetForeground()); got != fmt.Sprint(colorText) {
		t.Fatalf("expected busy status foreground %v, got %v", colorText, statusBusyStyle.GetForeground())
	}
	if got := fmt.Sprint(statusBusyStyle.GetBackground()); got != fmt.Sprint(colorBackground) {
		t.Fatalf("expected busy status background %v, got %v", colorBackground, statusBusyStyle.GetBackground())
	}
}

func TestSpinnerUsesWhiteForeground(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	if got := fmt.Sprint(m.spinner.Style.GetForeground()); got != fmt.Sprint(colorText) {
		t.Fatalf("expected spinner foreground %v, got %v", colorText, m.spinner.Style.GetForeground())
	}
}

func TestHeaderViewHiddenWhenMouseOff(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.width = 160
	m.mouseEnabled = false

	if got := m.headerView(); got != "" {
		t.Fatalf("expected header to be hidden when mouse capture is off, got %q", got)
	}
}

func TestHeaderViewShownWhenMouseOn(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.width = 160
	m.mouseEnabled = true

	got := ansiEscapePattern.ReplaceAllString(m.headerView(), "")
	if !strings.Contains(got, "localclaw") || !strings.Contains(got, "session:") || !strings.Contains(got, "tokens:0") || !strings.Contains(got, "workspace:") {
		t.Fatalf("expected header to be shown when mouse capture is on, got %q", got)
	}
	if strings.Contains(got, "#") {
		t.Fatalf("expected header to omit hash prefix, got %q", got)
	}
	if strings.Contains(got, "provider:") || strings.Contains(got, "model:") {
		t.Fatalf("expected header to omit provider/model metadata when mouse capture is on, got %q", got)
	}
}

func TestHeaderViewUsesConfiguredAppName(t *testing.T) {
	cfg := config.Default()
	cfg.App.Name = "clawbox"
	m := newModel(context.Background(), nil, cfg)
	m.width = 160
	m.mouseEnabled = true

	got := ansiEscapePattern.ReplaceAllString(m.headerView(), "")
	if !strings.Contains(got, "clawbox") {
		t.Fatalf("expected configured app name in header, got %q", got)
	}
	if strings.Contains(got, "#") {
		t.Fatalf("expected header to omit hash prefix, got %q", got)
	}
}

func TestStatusViewOmitsSlashStatusHint(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.width = 120

	got := ansiEscapePattern.ReplaceAllString(m.statusView(), "")
	if strings.Contains(got, "/status") {
		t.Fatalf("expected status row to omit /status hint, got %q", got)
	}
}

func TestStatusViewHidesRightMetadataWhenMouseOff(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.width = 120
	m.mouseEnabled = false

	got := ansiEscapePattern.ReplaceAllString(m.statusView(), "")
	if strings.Contains(got, "provider:") || strings.Contains(got, "model:") || strings.Contains(got, "mouse:") {
		t.Fatalf("expected status row right metadata to be hidden when mouse capture is off, got %q", got)
	}
	if !strings.Contains(got, statusIdle) {
		t.Fatalf("expected status row to keep lifecycle status text, got %q", got)
	}
}

func TestStatusViewHidesRightMetadataWhenMouseOn(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.width = 120
	m.mouseEnabled = true

	got := ansiEscapePattern.ReplaceAllString(m.statusView(), "")
	if strings.Contains(got, "provider:") || strings.Contains(got, "model:") || strings.Contains(got, "mouse:") {
		t.Fatalf("expected status row right metadata to be moved to footer row, got %q", got)
	}
	if !strings.Contains(got, statusIdle) {
		t.Fatalf("expected status row to keep lifecycle status text, got %q", got)
	}
}

func TestComposerFooterShowsShortcutsAndRuntimeSettings(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.width = 220
	m.mouseEnabled = false

	got := ansiEscapePattern.ReplaceAllString(m.composerFooterView(), "")
	if !strings.Contains(got, "Ctrl+J newline") || !strings.Contains(got, "/shortcuts") {
		t.Fatalf("expected composer footer left side to include shortcuts hint, got %q", got)
	}
	if !strings.Contains(got, "provider:") || !strings.Contains(got, "model:") || !strings.Contains(got, "reasoning:") || !strings.Contains(got, "mouse:off") {
		t.Fatalf("expected composer footer right side to include runtime settings, got %q", got)
	}
}

func TestLayoutReservesGapBetweenTranscriptAndComposer(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.width = 100
	m.height = 30
	m.mouseEnabled = true

	m.layout()

	headerHeight := lipgloss.Height(m.headerView())
	statusHeight := lipgloss.Height(m.statusView())
	inputHeight := lipgloss.Height(m.inputView())
	gapHeight := lipgloss.Height(m.composerGapView())
	footerHeight := lipgloss.Height(m.composerFooterView())
	expected := m.height - headerHeight - statusHeight - inputHeight - gapHeight - footerHeight
	if expected < 1 {
		expected = 1
	}
	if m.viewport.Height != expected {
		t.Fatalf("expected viewport height %d with one-row transcript/composer gap, got %d", expected, m.viewport.Height)
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
	m.width = 220

	hint := m.composerFooterView()
	if !strings.Contains(hint, "Ctrl+J newline") {
		t.Fatalf("expected input hint to mention Ctrl+J newline, got %q", hint)
	}
	if !strings.Contains(hint, "Ctrl+Y mouse") {
		t.Fatalf("expected input hint to mention Ctrl+Y mouse toggle, got %q", hint)
	}
	if !strings.Contains(hint, "Ctrl+O tools") {
		t.Fatalf("expected input hint to mention Ctrl+O tools toggle, got %q", hint)
	}
	if strings.Contains(hint, "Ctrl+T") {
		t.Fatalf("expected input hint not to mention removed Ctrl+T thinking toggle, got %q", hint)
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
	ctx := context.Background()
	if err := app.Run(ctx); err != nil {
		t.Fatalf("run runtime app: %v", err)
	}
	if err := app.AddSessionTokens(ctx, "default", "main", 12); err != nil {
		t.Fatalf("seed main session tokens: %v", err)
	}

	m := newModel(ctx, app, cfg)
	m.mouseEnabled = true
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
	if !strings.Contains(header, "tokens:12") {
		t.Fatalf("expected header to include persisted token count, got %q", header)
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

func TestInitSchedulesBootstrapSeedWhenPendingAndSessionIsEmpty(t *testing.T) {
	m, _ := newRuntimeBackedModel(t)

	if !cmdEmitsBootstrapSeedTrigger(m.Init()) {
		t.Fatalf("expected init command batch to include bootstrap seed trigger")
	}
}

func TestInitDoesNotScheduleBootstrapSeedWhenSessionHasTranscript(t *testing.T) {
	m, app := newRuntimeBackedModel(t)
	if err := app.AppendSessionTranscriptMessage(context.Background(), m.agentID, m.sessionID, "user", "already initialized"); err != nil {
		t.Fatalf("append transcript: %v", err)
	}

	if cmdEmitsBootstrapSeedTrigger(m.Init()) {
		t.Fatalf("did not expect init command batch to include bootstrap seed trigger when transcript exists")
	}
}

func TestHandleSlashNewSchedulesBootstrapSeedWhenPending(t *testing.T) {
	m, _ := newRuntimeBackedModel(t)

	cmd := m.handleSlash("/new")
	if !cmdEmitsBootstrapSeedTrigger(cmd) {
		t.Fatalf("expected /new to schedule bootstrap seed trigger when bootstrap is pending")
	}
}

func TestHandleSlashNewDoesNotScheduleBootstrapSeedWhenBootstrapMissing(t *testing.T) {
	m, _ := newRuntimeBackedModel(t)
	bootstrapPath := filepath.Join(m.workspacePath, bootstrapFileName)
	if err := os.Remove(bootstrapPath); err != nil {
		t.Fatalf("remove BOOTSTRAP.md: %v", err)
	}

	cmd := m.handleSlash("/new")
	if cmdEmitsBootstrapSeedTrigger(cmd) {
		t.Fatalf("did not expect /new bootstrap seed trigger when BOOTSTRAP.md is missing")
	}
}

func TestHandleSlashSessionsListsKnownSessions(t *testing.T) {
	m, app := newRuntimeBackedModel(t)
	ctx := context.Background()

	if err := app.AddSessionTokens(ctx, "default", "main", 5); err != nil {
		t.Fatalf("seed main session: %v", err)
	}
	if err := app.AddSessionTokens(ctx, "default", "archive-1", 10); err != nil {
		t.Fatalf("seed archive session: %v", err)
	}

	_ = m.handleSlash("/sessions")
	got := m.messages[len(m.messages)-1].Raw
	if !strings.Contains(got, "sessions (") {
		t.Fatalf("expected sessions listing heading, got %q", got)
	}
	if !strings.Contains(got, "main (current)") {
		t.Fatalf("expected current-session marker in listing, got %q", got)
	}
	if !strings.Contains(got, "archive-1") {
		t.Fatalf("expected archive session in listing, got %q", got)
	}
}

func TestHandleSlashResumeSwitchesSessionAndLoadsTranscript(t *testing.T) {
	m, app := newRuntimeBackedModel(t)
	ctx := context.Background()

	if err := app.AddSessionTokens(ctx, "default", "archive-2", 20); err != nil {
		t.Fatalf("seed archive metadata: %v", err)
	}
	if err := app.AppendSessionTranscriptMessage(ctx, "default", "archive-2", "user", "hello"); err != nil {
		t.Fatalf("seed user transcript: %v", err)
	}
	if err := app.AppendSessionTranscriptMessage(ctx, "default", "archive-2", "assistant", "hi there"); err != nil {
		t.Fatalf("seed assistant transcript: %v", err)
	}

	m.modelOverride = "gpt-5-mini"
	_ = m.handleSlash("/resume archive-2")

	if m.sessionID != "archive-2" {
		t.Fatalf("expected session to switch to archive-2, got %q", m.sessionID)
	}
	if m.modelOverride != "" {
		t.Fatalf("expected resume to clear model override, got %q", m.modelOverride)
	}
	if len(m.messages) != 3 {
		t.Fatalf("expected 3 messages (2 transcript + status), got %d", len(m.messages))
	}
	if m.messages[0].Role != roleUser || m.messages[0].Raw != "hello" {
		t.Fatalf("expected first restored transcript row to be user hello, got role=%q raw=%q", m.messages[0].Role, m.messages[0].Raw)
	}
	if m.messages[1].Role != roleAssistant || m.messages[1].Raw != "hi there" {
		t.Fatalf("expected second restored transcript row to be assistant response, got role=%q raw=%q", m.messages[1].Role, m.messages[1].Raw)
	}
	if m.messages[2].Role != roleSystem || !strings.Contains(m.messages[2].Raw, "resumed session archive-2") {
		t.Fatalf("expected resume confirmation message, got role=%q raw=%q", m.messages[2].Role, m.messages[2].Raw)
	}
}

func TestHandleSlashResumeRequiresSessionID(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())

	_ = m.handleSlash("/resume")
	if got := m.messages[len(m.messages)-1].Raw; got != "usage: /resume <session_id>" {
		t.Fatalf("unexpected /resume usage response %q", got)
	}
}

func TestHandleSlashResumeRejectsMissingSession(t *testing.T) {
	m, _ := newRuntimeBackedModel(t)

	_ = m.handleSlash("/resume does-not-exist")
	got := m.messages[len(m.messages)-1].Raw
	if !strings.Contains(got, "session does-not-exist not found") {
		t.Fatalf("expected missing-session error, got %q", got)
	}
}

func TestHandleSlashDeleteRemovesSessionAndTranscript(t *testing.T) {
	m, app := newRuntimeBackedModel(t)
	ctx := context.Background()

	if err := app.AddSessionTokens(ctx, "default", "archive-delete", 1); err != nil {
		t.Fatalf("seed archive metadata: %v", err)
	}
	if err := app.AppendSessionTranscriptMessage(ctx, "default", "archive-delete", "user", "cleanup me"); err != nil {
		t.Fatalf("seed archive transcript: %v", err)
	}
	transcriptPath, err := app.ResolveTranscriptPath("default", "archive-delete")
	if err != nil {
		t.Fatalf("resolve transcript path: %v", err)
	}

	_ = m.handleSlash("/delete archive-delete")

	got := m.messages[len(m.messages)-1].Raw
	if !strings.Contains(got, "deleted session archive-delete") {
		t.Fatalf("expected delete confirmation, got %q", got)
	}
	if _, err := app.MCPSessionStatus(ctx, "default", "archive-delete"); !errors.Is(err, runtime.ErrMCPNotFound) {
		t.Fatalf("expected deleted session status to be not found, got %v", err)
	}
	if _, err := os.Stat(transcriptPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected deleted transcript to be removed, got %v", err)
	}
}

func TestHandleSlashDeleteRejectsCurrentSession(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())

	_ = m.handleSlash("/delete main")
	if got := m.messages[len(m.messages)-1].Raw; got != "cannot delete active session main; resume a different session first" {
		t.Fatalf("unexpected current-session delete response %q", got)
	}
}

func TestHandleSlashDeleteRequiresSessionID(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())

	_ = m.handleSlash("/delete")
	if got := m.messages[len(m.messages)-1].Raw; got != "usage: /delete <session_id>" {
		t.Fatalf("unexpected /delete usage response %q", got)
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

func TestNewModelAppliesAppDefaultFlags(t *testing.T) {
	cfg := config.Default()
	cfg.App.Default.Verbose = true
	cfg.App.Default.Mouse = false
	cfg.App.Default.Tools = true

	m := newModel(context.Background(), nil, cfg)
	if !m.verbose {
		t.Fatalf("expected verbose=true from app.default.verbose")
	}
	if m.mouseEnabled {
		t.Fatalf("expected mouseEnabled=false from app.default.mouse")
	}
	if !m.toolsExpanded {
		t.Fatalf("expected toolsExpanded=true from app.default.tools")
	}
}

func TestCtrlYTogglesMouseCapture(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	if m.mouseEnabled {
		t.Fatalf("expected mouse capture to start disabled")
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlY})
	next := updated.(model)
	if !next.mouseEnabled {
		t.Fatalf("expected ctrl+y to enable mouse capture")
	}
	if cmd == nil {
		t.Fatalf("expected ctrl+y to emit enable-mouse command")
	}
	if got := reflect.TypeOf(cmd()).String(); got != "tea.enableMouseCellMotionMsg" {
		t.Fatalf("expected enable mouse command type, got %s", got)
	}

	updated, cmd = next.Update(tea.KeyMsg{Type: tea.KeyCtrlY})
	next = updated.(model)
	if next.mouseEnabled {
		t.Fatalf("expected second ctrl+y to disable mouse capture")
	}
	if cmd == nil {
		t.Fatalf("expected second ctrl+y to emit disable-mouse command")
	}
	if got := reflect.TypeOf(cmd()).String(); got != "tea.disableMouseMsg" {
		t.Fatalf("expected disable mouse command type, got %s", got)
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
	if strings.Contains(got, "Ctrl+T") || strings.Contains(got, "toggle thinking visibility") {
		t.Fatalf("expected /shortcuts output to omit removed thinking shortcut, got %q", got)
	}
}

func TestSlashMenuShowsKeyboardShortcutColumnWhenAvailable(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.width = 160
	m.input.SetValue("/mo")
	m.updateSlashAutocomplete()

	menu := ansiEscapePattern.ReplaceAllString(m.slashMenuView(), "")
	if !regexp.MustCompile(`(?m)/mouse <on\|off>\s+toggle mouse capture \(wheel/selection tradeoff\)\s+Ctrl\+Y`).MatchString(menu) {
		t.Fatalf("expected slash menu /mouse row to include Ctrl+Y shortcut column, got %q", menu)
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

func TestHandleSlashThinkingReturnsUnknownCommand(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())

	_ = m.handleSlash("/thinking off")
	if len(m.messages) == 0 {
		t.Fatalf("expected /thinking to add a system message")
	}
	got := m.messages[len(m.messages)-1].Raw
	if got != "unknown command: /thinking" {
		t.Fatalf("expected unknown command for removed /thinking, got %q", got)
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
	if !strings.Contains(got, "tools:") {
		t.Fatalf("expected /tools output to include tools list heading, got %q", got)
	}
	if !strings.Contains(got, "- runtime unavailable") {
		t.Fatalf("expected /tools output to mention runtime availability, got %q", got)
	}
}

func TestHandleSlashToolsStartsDiscoveryWhenProviderToolsUndiscovered(t *testing.T) {
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
	cmd := m.handleSlash("/tools")
	if len(m.messages) == 0 {
		t.Fatalf("expected /tools to add a system message")
	}
	if cmd == nil {
		t.Fatalf("expected /tools to trigger provider tool discovery when tools are not discovered")
	}
	if !m.providerToolsDiscoveryInFlight {
		t.Fatalf("expected provider tool discovery state to be in-flight")
	}
	got := m.messages[len(m.messages)-1].Raw
	if !strings.Contains(got, "provider="+cfg.LLM.Provider) {
		t.Fatalf("expected /tools output to include provider, got %q", got)
	}
	if !strings.Contains(got, "tools:") {
		t.Fatalf("expected /tools output to include tools heading, got %q", got)
	}
	if !strings.Contains(got, "- discovering...") {
		t.Fatalf("expected /tools output to indicate discovery in progress, got %q", got)
	}
	if strings.Contains(got, "memory_search") || strings.Contains(got, "memory_get") {
		t.Fatalf("expected /tools output to avoid runtime fallback tools while provider tools are undiscovered, got %q", got)
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
	if !strings.Contains(got, "tools:") {
		t.Fatalf("expected tools heading in /tools output, got %q", got)
	}
	if !strings.Contains(got, "- Bash\n- Task\n- WebFetch") {
		t.Fatalf("expected provider tools to render one per line in /tools output, got %q", got)
	}
}

func TestHandleSlashToolsUsesProviderToolsAsSourceOfTruth(t *testing.T) {
	m, _ := newRuntimeBackedModel(t)
	m.activeRunID = 11

	updated, _ := m.Update(streamEventMsg{
		RunID: 11,
		OK:    true,
		Event: llm.StreamEvent{
			Type: llm.StreamEventProviderMetadata,
			ProviderMetadata: &llm.ProviderMetadata{
				Provider: "codex",
				Tools: []string{
					"AskUserQuestion",
					"Bash",
					"mcp__localclaw__localclaw_cron_add",
				},
			},
		},
	})
	m = updated.(model)
	_ = m.handleSlash("/tools")

	got := m.messages[len(m.messages)-1].Raw
	if !strings.Contains(got, "tools:") {
		t.Fatalf("expected tools heading in /tools output, got %q", got)
	}
	if !strings.Contains(got, "- AskUserQuestion") || !strings.Contains(got, "- Bash") {
		t.Fatalf("expected provider tools in merged tools list, got %q", got)
	}
	if !strings.Contains(got, "- mcp__localclaw__localclaw_cron_add") {
		t.Fatalf("expected provider-discovered localclaw MCP tool in merged tools list, got %q", got)
	}
	if strings.Contains(got, "- memory_search") {
		t.Fatalf("expected /tools to use provider tools only and omit runtime fallback tools, got %q", got)
	}
}

func TestProviderToolsDiscoveredMessageUpdatesToolsList(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	m.providerToolsDiscoveryInFlight = true

	updated, _ := m.Update(providerToolsDiscoveredMsg{
		Provider: "claudecode",
		Model:    "claude-opus-4-6",
		Tools:    []string{"Bash", "mcp__localclaw__localclaw_memory_search"},
	})
	m = updated.(model)

	if m.providerToolsDiscoveryInFlight {
		t.Fatalf("expected provider tool discovery state to be cleared after discovery result")
	}
	if len(m.providerTools) != 2 {
		t.Fatalf("expected discovered provider tools to be stored, got %v", m.providerTools)
	}
	got := m.messages[len(m.messages)-1].Raw
	if !strings.Contains(got, "tools:") || !strings.Contains(got, "- Bash") {
		t.Fatalf("expected discovery result to append refreshed /tools summary, got %q", got)
	}
}

func TestHandleSlashModelSetsCanonicalSelector(t *testing.T) {
	cfg := config.Default()
	cfg.LLM.Provider = "codex"
	cfg.LLM.Codex.Model = "gpt-5-codex"
	cfg.LLM.Codex.ReasoningDefault = "medium"

	m := newModel(context.Background(), nil, cfg)
	_ = m.handleSlash("/model codex/gpt-5-mini/high")

	if m.providerOverride != "codex" {
		t.Fatalf("expected provider override to be set, got %q", m.providerOverride)
	}
	if m.modelOverride != "gpt-5-mini" {
		t.Fatalf("expected model override to be set, got %q", m.modelOverride)
	}
	if m.reasoningOverride != "high" {
		t.Fatalf("expected reasoning override to be set, got %q", m.reasoningOverride)
	}
	got := m.messages[len(m.messages)-1].Raw
	if !strings.Contains(got, "active selector set to codex/gpt-5-mini/high") {
		t.Fatalf("expected model set acknowledgement, got %q", got)
	}
}

func TestHandleSlashModelShorthandKeepsCurrentProvider(t *testing.T) {
	cfg := config.Default()
	cfg.LLM.Provider = "codex"
	cfg.LLM.Codex.Model = "gpt-5-codex"
	cfg.LLM.Codex.ReasoningDefault = "medium"

	m := newModel(context.Background(), nil, cfg)
	_ = m.handleSlash("/model gpt-5-mini")

	if m.providerOverride != "codex" {
		t.Fatalf("expected shorthand selector to keep codex provider, got %q", m.providerOverride)
	}
	if m.modelOverride != "gpt-5-mini" {
		t.Fatalf("expected shorthand selector to set model override, got %q", m.modelOverride)
	}
	if m.reasoningOverride != "medium" {
		t.Fatalf("expected shorthand selector to apply default reasoning, got %q", m.reasoningOverride)
	}
}

func TestHandleSlashModelClearsSelectorWithDefault(t *testing.T) {
	cfg := config.Default()
	cfg.LLM.Provider = "codex"
	cfg.LLM.Codex.Model = "gpt-5-codex"
	cfg.LLM.Codex.ReasoningDefault = "medium"

	m := newModel(context.Background(), nil, cfg)
	_ = m.handleSlash("/model codex/gpt-5-mini/high")
	_ = m.handleSlash("/model default")

	if m.providerOverride != "" || m.modelOverride != "" || m.reasoningOverride != "" {
		t.Fatalf("expected selector overrides to be cleared, got provider=%q model=%q reasoning=%q", m.providerOverride, m.modelOverride, m.reasoningOverride)
	}
	got := m.messages[len(m.messages)-1].Raw
	if !strings.Contains(got, "selector reset to defaults") {
		t.Fatalf("expected selector clear acknowledgement, got %q", got)
	}
}

func TestHandleSlashModelRejectsUnknownProvider(t *testing.T) {
	cfg := config.Default()
	cfg.LLM.Provider = "claudecode"

	m := newModel(context.Background(), nil, cfg)
	_ = m.handleSlash("/model unknown/model-a")

	if m.providerOverride != "" || m.modelOverride != "" || m.reasoningOverride != "" {
		t.Fatalf("expected selector overrides to remain unset for invalid provider")
	}
	got := m.messages[len(m.messages)-1].Raw
	if !strings.Contains(got, "unknown provider") {
		t.Fatalf("expected unknown provider notice, got %q", got)
	}
}

func TestHandleSlashModelsReportsRuntimeUnavailable(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	_ = m.handleSlash("/models")

	if len(m.messages) == 0 {
		t.Fatalf("expected /models to add a system message")
	}
	got := m.messages[len(m.messages)-1].Raw
	if !strings.Contains(got, "runtime unavailable") {
		t.Fatalf("expected /models runtime unavailable message, got %q", got)
	}
}

func TestHandleSlashModelsActiveSummaryUsesKnownStateWhenModelUnknown(t *testing.T) {
	m, _ := newRuntimeBackedModel(t)
	m.cfg.LLM.Provider = "codex"
	m.cfg.LLM.Codex.Model = ""
	m.cfg.LLM.Codex.ReasoningDefault = "medium"
	m.providerOverride = "codex"
	m.providerName = "codex"
	m.providerModel = ""
	_ = m.handleSlash("/models")

	if len(m.messages) == 0 {
		t.Fatalf("expected /models to add a system message")
	}
	got := m.messages[len(m.messages)-1].Raw
	if !strings.Contains(got, "active: codex model=unknown reasoning=medium") {
		t.Fatalf("expected /models active summary to show known provider/reasoning state, got %q", got)
	}
	if strings.Contains(got, "active selector:") || strings.Contains(got, "n/a") {
		t.Fatalf("expected /models active summary to avoid active selector/n/a fallback text, got %q", got)
	}
}

func TestHandleSlashStatusIncludesEffectiveSelector(t *testing.T) {
	cfg := config.Default()
	cfg.LLM.Provider = "codex"
	cfg.LLM.Codex.Model = "gpt-5-codex"
	cfg.LLM.Codex.ReasoningDefault = "medium"

	m := newModel(context.Background(), nil, cfg)
	_ = m.handleSlash("/model codex/gpt-5-mini/high")
	_ = m.handleSlash("/status")

	got := m.messages[len(m.messages)-1].Raw
	if !strings.Contains(got, "provider=codex") {
		t.Fatalf("expected status to include provider, got %q", got)
	}
	if !strings.Contains(got, "configured_model=gpt-5-codex") {
		t.Fatalf("expected status to include configured model, got %q", got)
	}
	if !strings.Contains(got, "effective_model=gpt-5-mini") {
		t.Fatalf("expected status to include effective model override, got %q", got)
	}
	if !strings.Contains(got, "effective_selector=codex/gpt-5-mini/high") {
		t.Fatalf("expected status to include effective selector, got %q", got)
	}
}

func TestComposerFooterShowsKnownStateWhenModelUnknown(t *testing.T) {
	cfg := config.Default()
	cfg.LLM.Provider = "codex"
	cfg.LLM.Codex.Model = ""
	cfg.LLM.Codex.ReasoningDefault = "medium"

	m := newModel(context.Background(), nil, cfg)
	m.width = 220
	m.mouseEnabled = false

	got := ansiEscapePattern.ReplaceAllString(m.composerFooterView(), "")
	if !strings.Contains(got, "provider:codex") {
		t.Fatalf("expected footer to include provider codex, got %q", got)
	}
	if !strings.Contains(got, "model:unknown") {
		t.Fatalf("expected footer to show unknown model instead of n/a, got %q", got)
	}
	if !strings.Contains(got, "reasoning:medium") {
		t.Fatalf("expected footer to include known reasoning default, got %q", got)
	}
	if strings.Contains(got, "model:n/a") {
		t.Fatalf("expected footer to avoid model n/a fallback, got %q", got)
	}
}

func TestRunSessionResetClearsSelectorOverrides(t *testing.T) {
	cfg := config.Default()
	cfg.LLM.Provider = "codex"
	cfg.LLM.Codex.Model = "gpt-5-codex"
	cfg.LLM.Codex.ReasoningDefault = "medium"

	m := newModel(context.Background(), nil, cfg)
	m.providerOverride = "codex"
	m.modelOverride = "gpt-5-mini"
	m.reasoningOverride = "high"

	m.runSessionReset(false, "/reset")
	if m.providerOverride != "" || m.modelOverride != "" || m.reasoningOverride != "" {
		t.Fatalf("expected /reset to clear selector overrides, got provider=%q model=%q reasoning=%q", m.providerOverride, m.modelOverride, m.reasoningOverride)
	}

	m.providerOverride = "codex"
	m.modelOverride = "gpt-5-mini"
	m.reasoningOverride = "high"
	m.runSessionReset(true, "/new")
	if m.providerOverride != "" || m.modelOverride != "" || m.reasoningOverride != "" {
		t.Fatalf("expected /new to clear selector overrides, got provider=%q model=%q reasoning=%q", m.providerOverride, m.modelOverride, m.reasoningOverride)
	}
}
