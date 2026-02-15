package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/lipgloss"

	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/llm/claudecode"
	"github.com/dgriffin831/localclaw/internal/memory"
	"github.com/dgriffin831/localclaw/internal/runtime"
)

const (
	statusIdle      = "idle"
	statusSending   = "sending"
	statusWaiting   = "waiting"
	statusStreaming = "streaming"
	statusAborted   = "aborted"
	statusError     = "error"
)

type messageRole string

const (
	roleUser      messageRole = "user"
	roleAssistant messageRole = "assistant"
	roleSystem    messageRole = "system"
)

type chatMessage struct {
	Role                messageRole
	Raw                 string
	Streaming           bool
	ThinkingPlaceholder bool
}

type streamEventMsg struct {
	RunID int
	Event claudecode.StreamEvent
	OK    bool
}

type streamErrMsg struct {
	RunID int
	Err   error
	OK    bool
}

type statusTickMsg time.Time
type ctxDoneMsg struct{}

type model struct {
	ctx context.Context
	app *runtime.App
	cfg config.Config
	// Runtime-resolved identity and paths shared across runtime/TUI/CLI.
	agentID       string
	sessionID     string
	sessionKey    string
	workspacePath string

	width  int
	height int

	viewport viewport.Model
	input    textarea.Model
	spinner  spinner.Model

	messages []chatMessage

	status          string
	statusStartedAt time.Time
	running         bool
	hasStreamDelta  bool

	showThinking  bool
	verbose       bool
	toolsExpanded bool

	runSeq             int
	activeRunID        int
	activeAssistantIdx int
	runCancel          context.CancelFunc
	streamEvents       <-chan claudecode.StreamEvent
	streamErrs         <-chan error

	renderer      *glamour.TermRenderer
	rendererWidth int

	history      []string
	historyIdx   int
	historyDraft string

	lastCtrlC time.Time
}

var (
	colorPrimary        = lipgloss.Color("#fab283")
	colorSecondary      = lipgloss.Color("#5c9cf5")
	colorAccent         = lipgloss.Color("#9d7cd8")
	colorError          = lipgloss.Color("#e06c75")
	colorWarning        = lipgloss.Color("#f5a742")
	colorSuccess        = lipgloss.Color("#7fd88f")
	colorInfo           = lipgloss.Color("#56b6c2")
	colorText           = lipgloss.Color("#eeeeee")
	colorTextMuted      = lipgloss.Color("#808080")
	colorBackground     = lipgloss.Color("#0a0a0a")
	colorBackgroundPane = lipgloss.Color("#141414")
	colorBackgroundElem = lipgloss.Color("#1e1e1e")
	colorBorder         = lipgloss.Color("#484848")
	colorBorderSubtle   = lipgloss.Color("#3c3c3c")

	splitBorder = lipgloss.Border{
		Left:  "┃",
		Right: "┃",
	}

	panelRowStyle = lipgloss.NewStyle().
			Background(colorBackgroundPane).
			BorderStyle(splitBorder).
			BorderLeft(true).
			BorderRight(true).
			BorderTop(false).
			BorderBottom(false).
			BorderForeground(colorBorder).
			Padding(0, 1)

	headerStyle = panelRowStyle.Copy().Foreground(colorText)

	assistantStyle = lipgloss.NewStyle().
			Foreground(colorText).
			PaddingLeft(3)

	userStyle = lipgloss.NewStyle().
			Background(colorBackgroundPane).
			BorderStyle(lipgloss.Border{Left: "┃"}).
			BorderLeft(true).
			BorderForeground(colorPrimary).
			Padding(0, 2)

	systemStyle = lipgloss.NewStyle().
			Foreground(colorTextMuted).
			PaddingLeft(3)

	statusIdleStyle = panelRowStyle.Copy().
			Foreground(colorTextMuted)

	statusBusyStyle = panelRowStyle.Copy().
			Foreground(colorWarning)

	statusErrStyle = panelRowStyle.Copy().
			Foreground(colorError).
			Bold(true)

	inputStyle = panelRowStyle.Copy().
			BorderForeground(colorBorderSubtle).
			Background(colorBackgroundPane)

	inputHintStyle = lipgloss.NewStyle().
			Foreground(colorTextMuted)
)

func newModel(ctx context.Context, app *runtime.App, cfg config.Config) model {
	resolution := runtime.ResolveSession("", "")
	workspacePath := cfg.Workspace.Root
	if app != nil {
		if resolvedPath, err := app.ResolveWorkspacePath(resolution.AgentID); err == nil {
			workspacePath = resolvedPath
		}
	}

	input := textarea.New()
	input.Placeholder = "Ask localclaw..."
	input.Focus()
	input.ShowLineNumbers = false
	// bubbles/textarea v0.13.0 treats CharLimit <= 0 as effectively no input.
	// Use a high practical ceiling instead of 0.
	input.CharLimit = 100000
	input.Prompt = "❯ "
	input.SetHeight(1)
	input.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter"))

	focused, blurred := textarea.DefaultStyles()
	focused.Base = focused.Base.Background(colorBackgroundPane).Foreground(colorText)
	focused.Text = lipgloss.NewStyle().Foreground(colorText)
	focused.Prompt = lipgloss.NewStyle().Foreground(colorPrimary)
	focused.Placeholder = lipgloss.NewStyle().Foreground(colorTextMuted)
	focused.CursorLine = lipgloss.NewStyle().Background(colorBackgroundPane)
	focused.CursorLineNumber = lipgloss.NewStyle().Foreground(colorTextMuted)
	focused.LineNumber = lipgloss.NewStyle().Foreground(colorTextMuted)
	focused.EndOfBuffer = lipgloss.NewStyle().Foreground(colorBorderSubtle)
	blurred = focused
	blurred.Prompt = lipgloss.NewStyle().Foreground(colorTextMuted)
	input.FocusedStyle = focused
	input.BlurredStyle = blurred

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colorTextMuted)

	vp := viewport.New(0, 0)
	vp.MouseWheelEnabled = true

	m := model{
		ctx:                ctx,
		app:                app,
		cfg:                cfg,
		agentID:            resolution.AgentID,
		sessionID:          resolution.SessionID,
		sessionKey:         resolution.SessionKey,
		workspacePath:      workspacePath,
		viewport:           vp,
		input:              input,
		spinner:            sp,
		status:             statusIdle,
		showThinking:       true,
		historyIdx:         -1,
		activeAssistantIdx: -1,
	}
	m.addSystem("localclaw ready. Type /help for commands.")
	return m
}

func Run(ctx context.Context, app *runtime.App, cfg config.Config) error {
	m := newModel(ctx, app, cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())

	go func() {
		<-ctx.Done()
		p.Send(ctxDoneMsg{})
	}()

	return p.Start()
}

func (m model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, tickStatus())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case ctxDoneMsg:
		m.abortRun("context cancelled")
		return m, tea.Quit

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layout()
		m.refreshViewport(true)
		return m, nil

	case tea.KeyMsg:
		if key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+c"))) {
			if strings.TrimSpace(m.input.Value()) != "" {
				m.input.Reset()
				m.adjustInputHeight()
				m.setStatus("cleared input")
				return m, nil
			}
			if !m.lastCtrlC.IsZero() && time.Since(m.lastCtrlC) <= time.Second {
				m.abortRun("exiting")
				return m, tea.Quit
			}
			m.lastCtrlC = time.Now()
			m.setStatus("press ctrl+c again to exit")
			return m, nil
		}

		if key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+d"))) {
			if strings.TrimSpace(m.input.Value()) == "" {
				m.abortRun("exiting")
				return m, tea.Quit
			}
		}

		if key.Matches(msg, key.NewBinding(key.WithKeys("esc"))) {
			if m.running {
				m.abortRun("run aborted")
				m.addSystem("run aborted")
				m.refreshViewport(true)
				return m, nil
			}
		}

		if key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+t"))) {
			m.showThinking = !m.showThinking
			m.addSystem(fmt.Sprintf("thinking visibility: %s", onOff(m.showThinking)))
			m.refreshViewport(true)
			return m, nil
		}

		if key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+o"))) {
			m.toolsExpanded = !m.toolsExpanded
			m.addSystem(fmt.Sprintf("tool cards: %s", mapBool(m.toolsExpanded, "expanded", "collapsed")))
			m.refreshViewport(true)
			return m, nil
		}

		if msg.Type == tea.KeyEnter && !msg.Alt {
			cmd := m.submitInput()
			return m, cmd
		}

		if msg.Type == tea.KeyUp {
			if m.useHistory(-1) {
				m.adjustInputHeight()
				return m, nil
			}
		}

		if msg.Type == tea.KeyDown {
			if m.useHistory(1) {
				m.adjustInputHeight()
				return m, nil
			}
		}

	case spinner.TickMsg:
		if m.isBusy() {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}

	case statusTickMsg:
		if m.isBusy() {
			cmds = append(cmds, tickStatus())
		}

	case streamEventMsg:
		if msg.RunID != m.activeRunID {
			return m, nil
		}
		if !msg.OK {
			m.streamEvents = nil
			return m, nil
		}

		switch msg.Event.Type {
		case claudecode.StreamEventDelta:
			m.setStatus(statusStreaming)
			m.hasStreamDelta = true
			m.applyDelta(msg.Event.Text)
			m.refreshViewport(true)
		case claudecode.StreamEventFinal:
			m.applyFinal(msg.Event.Text)
			m.finishRun(statusIdle)
			m.refreshViewport(true)
		}

		if m.streamEvents != nil {
			cmds = append(cmds, waitStreamEvent(m.activeRunID, m.streamEvents))
		}

	case streamErrMsg:
		if msg.RunID != m.activeRunID {
			return m, nil
		}
		if !msg.OK {
			m.streamErrs = nil
			return m, nil
		}
		if msg.Err != nil {
			m.addSystem(fmt.Sprintf("prompt error: %v", msg.Err))
			m.finishRun(statusError)
			m.refreshViewport(true)
			return m, nil
		}
	}

	var inputCmd tea.Cmd
	m.input, inputCmd = m.input.Update(msg)
	if inputCmd != nil {
		cmds = append(cmds, inputCmd)
	}
	m.adjustInputHeight()
	m.layout()

	var viewportCmd tea.Cmd
	m.viewport, viewportCmd = m.viewport.Update(msg)
	if viewportCmd != nil {
		cmds = append(cmds, viewportCmd)
	}

	if len(cmds) == 0 {
		return m, nil
	}
	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "loading..."
	}

	header := m.headerView()
	status := m.statusView()
	input := m.inputView()

	content := lipgloss.JoinVertical(lipgloss.Left, header, m.viewport.View(), status, input)
	return lipgloss.NewStyle().
		Background(colorBackground).
		Foreground(colorText).
		Render(content)
}

func (m *model) headerView() string {
	profile := m.cfg.LLM.ClaudeCode.Profile
	if strings.TrimSpace(profile) == "" {
		profile = "default"
	}
	innerWidth := panelInnerWidth(m.width)
	left := "# localclaw"
	right := fmt.Sprintf(
		"model:%s/%s  workspace:%s",
		m.cfg.LLM.Provider,
		profile,
		formatWorkspacePath(m.workspacePath),
	)
	if innerWidth < 70 {
		right = fmt.Sprintf("model:%s  workspace:%s", m.cfg.LLM.Provider, formatWorkspacePath(m.workspacePath))
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
		base = "thinking"
	}
	settings := fmt.Sprintf(
		"thinking:%s  verbose:%s  tools:%s  /status",
		onOff(m.showThinking),
		onOff(m.verbose),
		mapBool(m.toolsExpanded, "expanded", "collapsed"),
	)
	innerWidth := panelInnerWidth(m.width)
	if innerWidth < 70 {
		settings = fmt.Sprintf("t:%s v:%s /status", onOff(m.showThinking), onOff(m.verbose))
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
	hintText := "Enter send • Alt+Enter newline • Ctrl+T thinking • /help"
	if panelInnerWidth(m.width) < 70 {
		hintText = "Enter send • Alt+Enter newline • /help"
	}
	if panelInnerWidth(m.width) < 42 {
		hintText = "Enter send • /help"
	}
	hint := inputHintStyle.Render(truncateText(hintText, panelInnerWidth(m.width)))
	body := m.input.View() + "\n" + hint
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

func (m *model) submitInput() tea.Cmd {
	value := strings.TrimSpace(m.input.Value())
	if value == "" {
		return nil
	}
	if m.running {
		m.addSystem("run already in progress; press Esc to abort")
		m.refreshViewport(true)
		return nil
	}

	m.rememberHistory(value)
	m.input.Reset()
	m.adjustInputHeight()

	if strings.HasPrefix(value, "/") {
		return m.handleSlash(value)
	}
	m.startRun(value)
	m.refreshViewport(true)

	cmds := []tea.Cmd{m.spinner.Tick, tickStatus()}
	if m.streamEvents != nil {
		cmds = append(cmds, waitStreamEvent(m.activeRunID, m.streamEvents))
	}
	if m.streamErrs != nil {
		cmds = append(cmds, waitStreamErr(m.activeRunID, m.streamErrs))
	}
	return tea.Batch(cmds...)
}

func (m *model) handleSlash(raw string) tea.Cmd {
	name, arg := parseSlash(raw)
	switch name {
	case "help":
		m.addSystem("commands: /help /status /clear /thinking <on|off> /verbose <on|off> /model <name> /exit")
	case "status":
		m.addSystem(fmt.Sprintf("status=%s model=%s agent=%s session=%s workspace=%s thinking=%s verbose=%s", m.status, m.cfg.LLM.Provider, m.agentID, m.sessionID, m.workspacePath, onOff(m.showThinking), onOff(m.verbose)))
	case "clear":
		m.messages = nil
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
	case "model":
		if strings.TrimSpace(arg) == "" {
			m.addSystem("usage: /model <name>")
		} else {
			m.addSystem(fmt.Sprintf("model override is not implemented yet (%s)", arg))
		}
	default:
		m.addSystem(fmt.Sprintf("unknown command: /%s", name))
	}
	m.refreshViewport(true)
	return nil
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

func (m *model) startRun(input string) {
	m.runSeq++
	m.activeRunID = m.runSeq
	m.running = true
	m.hasStreamDelta = false
	m.activeAssistantIdx = -1
	m.setStatus(statusSending)

	m.addUser(input)
	if m.app != nil {
		_ = m.app.AddSessionTokens(m.ctx, m.agentID, m.sessionID, memory.EstimateTokensFromText(input))
		m.app.RunMemoryFlushIfNeededAsync(m.ctx, m.agentID, m.sessionID)
	}

	runCtx, cancel := context.WithCancel(m.ctx)
	m.runCancel = cancel
	m.streamEvents, m.streamErrs = m.app.PromptStream(runCtx, input)
	m.setStatus(statusWaiting)
}

func (m *model) finishRun(finalStatus string) {
	m.running = false
	m.setStatus(finalStatus)
	m.activeRunID = 0
	m.activeAssistantIdx = -1
	m.streamEvents = nil
	m.streamErrs = nil
	if m.runCancel != nil {
		m.runCancel()
		m.runCancel = nil
	}
}

func (m *model) abortRun(message string) {
	if m.runCancel != nil {
		m.runCancel()
		m.runCancel = nil
	}
	if m.running {
		m.running = false
		m.activeRunID = 0
		m.activeAssistantIdx = -1
		m.streamEvents = nil
		m.streamErrs = nil
		m.setStatus(statusAborted)
	}
	if strings.TrimSpace(message) != "" {
		m.setStatus(message)
	}
}

func (m *model) applyDelta(chunk string) {
	if m.activeAssistantIdx < 0 || m.activeAssistantIdx >= len(m.messages) {
		m.addAssistant("", false)
	}
	msg := &m.messages[m.activeAssistantIdx]
	if msg.ThinkingPlaceholder {
		msg.Raw = ""
		msg.ThinkingPlaceholder = false
	}
	msg.Raw += chunk
	msg.Streaming = true
}

func (m *model) applyFinal(final string) {
	if m.activeAssistantIdx < 0 || m.activeAssistantIdx >= len(m.messages) {
		m.addAssistant("", false)
	}
	msg := &m.messages[m.activeAssistantIdx]
	trimmed := strings.TrimSpace(final)
	if trimmed != "" {
		msg.Raw = trimmed
	} else if strings.TrimSpace(msg.Raw) == "" {
		msg.Raw = "(no output)"
	}
	if m.app != nil {
		_ = m.app.AddSessionTokens(m.ctx, m.agentID, m.sessionID, memory.EstimateTokensFromText(msg.Raw))
	}
	msg.Streaming = false
	msg.ThinkingPlaceholder = false
}

func (m *model) addSystem(text string) {
	m.messages = append(m.messages, chatMessage{Role: roleSystem, Raw: text})
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
		if text == "" {
			continue
		}

		switch msg.Role {
		case roleSystem:
			blocks = append(blocks, systemStyle.Render(text))
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
			glamour.WithStyles(opencodeMarkdownStyles()),
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

func (m *model) rememberHistory(value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	if len(m.history) > 0 && m.history[len(m.history)-1] == value {
		m.historyIdx = -1
		m.historyDraft = ""
		return
	}
	m.history = append(m.history, value)
	if len(m.history) > 200 {
		m.history = m.history[len(m.history)-200:]
	}
	m.historyIdx = -1
	m.historyDraft = ""
}

func (m *model) useHistory(direction int) bool {
	if len(m.history) == 0 {
		return false
	}

	value := m.input.Value()
	if strings.Contains(value, "\n") {
		return false
	}

	if direction < 0 {
		if m.historyIdx == -1 {
			m.historyDraft = value
			m.historyIdx = len(m.history) - 1
		} else if m.historyIdx > 0 {
			m.historyIdx--
		}
		m.input.SetValue(m.history[m.historyIdx])
		m.input.CursorEnd()
		return true
	}

	if m.historyIdx == -1 {
		return false
	}
	if m.historyIdx < len(m.history)-1 {
		m.historyIdx++
		m.input.SetValue(m.history[m.historyIdx])
		m.input.CursorEnd()
		return true
	}

	m.historyIdx = -1
	m.input.SetValue(m.historyDraft)
	m.input.CursorEnd()
	return true
}

func (m *model) isBusy() bool {
	return m.status == statusSending || m.status == statusWaiting || m.status == statusStreaming
}

func (m *model) setStatus(next string) {
	if next == "" {
		return
	}
	if next != m.status {
		m.status = next
	}
	if m.isBusy() {
		if m.statusStartedAt.IsZero() {
			m.statusStartedAt = time.Now()
		}
		return
	}
	m.statusStartedAt = time.Time{}
}

func waitStreamEvent(runID int, ch <-chan claudecode.StreamEvent) tea.Cmd {
	return func() tea.Msg {
		evt, ok := <-ch
		return streamEventMsg{RunID: runID, Event: evt, OK: ok}
	}
}

func waitStreamErr(runID int, ch <-chan error) tea.Cmd {
	return func() tea.Msg {
		err, ok := <-ch
		return streamErrMsg{RunID: runID, Err: err, OK: ok}
	}
}

func tickStatus() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return statusTickMsg(t)
	})
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

func opencodeMarkdownStyles() ansi.StyleConfig {
	return ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: strPtr("#eeeeee")},
		},
		Paragraph: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: strPtr("#eeeeee")},
		},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: strPtr("#9d7cd8"), Bold: boolPtr(true)},
		},
		H1: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: strPtr("#9d7cd8"), Bold: boolPtr(true)},
		},
		H2: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: strPtr("#9d7cd8"), Bold: boolPtr(true)},
		},
		H3: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: strPtr("#9d7cd8"), Bold: boolPtr(true)},
		},
		Text: ansi.StylePrimitive{Color: strPtr("#eeeeee")},
		Strong: ansi.StylePrimitive{
			Color: strPtr("#f5a742"),
			Bold:  boolPtr(true),
		},
		Emph: ansi.StylePrimitive{
			Color:  strPtr("#e5c07b"),
			Italic: boolPtr(true),
		},
		HorizontalRule: ansi.StylePrimitive{
			Color: strPtr("#808080"),
		},
		List: ansi.StyleList{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{Color: strPtr("#eeeeee")},
			},
			LevelIndent: 2,
		},
		Item: ansi.StylePrimitive{
			Color: strPtr("#fab283"),
		},
		Enumeration: ansi.StylePrimitive{
			Color: strPtr("#56b6c2"),
		},
		Link: ansi.StylePrimitive{
			Color:     strPtr("#fab283"),
			Underline: boolPtr(true),
		},
		LinkText: ansi.StylePrimitive{
			Color: strPtr("#56b6c2"),
		},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: strPtr("#7fd88f")},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{
					Color:           strPtr("#eeeeee"),
					BackgroundColor: strPtr("#1e1e1e"),
				},
			},
			Theme: "dracula",
		},
		BlockQuote: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: strPtr("#e5c07b")},
			Indent:         uintPtr(1),
			IndentToken:    strPtr("┃ "),
		},
		Table: ansi.StyleTable{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{Color: strPtr("#eeeeee")},
			},
			CenterSeparator: strPtr("│"),
			ColumnSeparator: strPtr("│"),
			RowSeparator:    strPtr("─"),
		},
	}
}

func strPtr(v string) *string {
	return &v
}

func boolPtr(v bool) *bool {
	return &v
}

func uintPtr(v uint) *uint {
	return &v
}
