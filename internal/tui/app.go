package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	"github.com/dgriffin831/localclaw/internal/llm"
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
	slashMenuLimit  = 6
	welcomeFileName = "WELCOME.md"
)

type messageRole string

const (
	roleUser      messageRole = "user"
	roleAssistant messageRole = "assistant"
	roleSystem    messageRole = "system"
)

type slashCommandDef struct {
	Name        string
	Args        string
	Description string
}

var slashCommandDefs = []slashCommandDef{
	{Name: "help", Description: "show this help"},
	{Name: "status", Description: "show current status and session info"},
	{Name: "tools", Description: "show provider and available localclaw tools"},
	{Name: "clear", Description: "clear the visible transcript"},
	{Name: "reset", Description: "reset the current session"},
	{Name: "new", Description: "start a new session"},
	{Name: "thinking", Args: "<on|off>", Description: "toggle thinking visibility"},
	{Name: "verbose", Args: "<on|off>", Description: "toggle verbose mode"},
	{Name: "model", Args: "<name>", Description: "set model override (not implemented)"},
	{Name: "exit", Description: "exit the TUI"},
	{Name: "quit", Description: "alias for /exit"},
}

type chatMessage struct {
	Role                messageRole
	Raw                 string
	RenderMarkdown      bool
	Streaming           bool
	ThinkingPlaceholder bool
}

type streamEventMsg struct {
	RunID int
	Event llm.StreamEvent
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

	showThinking          bool
	verbose               bool
	toolsExpanded         bool
	thinkingMessages      []string
	thinkingMessageIdx    int
	activeThinkingMessage string
	providerName          string
	providerModel         string
	providerTools         []string
	toolCallOwnershipByID map[string]llm.ToolClass
	streamDeltaEvents     int
	streamDeltaChars      int

	runSeq             int
	activeRunID        int
	activeAssistantIdx int
	runCancel          context.CancelFunc
	streamEvents       <-chan llm.StreamEvent
	streamErrs         <-chan error

	renderer      *glamour.TermRenderer
	rendererWidth int

	history      []string
	historyIdx   int
	historyDraft string

	slashQuery    string
	slashMatches  []slashCommandDef
	slashSelected int

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

	slashMenuItemStyle = lipgloss.NewStyle().
				Foreground(colorTextMuted)

	slashMenuSelectedStyle = lipgloss.NewStyle().
				Foreground(colorText).
				Bold(true)

	slashMenuMoreStyle = lipgloss.NewStyle().
				Foreground(colorTextMuted).
				Italic(true)
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
	input.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("ctrl+j"))

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
		ctx:                   ctx,
		app:                   app,
		cfg:                   cfg,
		agentID:               resolution.AgentID,
		sessionID:             resolution.SessionID,
		sessionKey:            resolution.SessionKey,
		workspacePath:         workspacePath,
		viewport:              vp,
		input:                 input,
		spinner:               sp,
		status:                statusIdle,
		showThinking:          true,
		historyIdx:            -1,
		activeAssistantIdx:    -1,
		thinkingMessages:      resolveThinkingMessages(cfg.App.ThinkingMessages),
		providerName:          strings.TrimSpace(cfg.LLM.Provider),
		toolCallOwnershipByID: map[string]llm.ToolClass{},
	}
	m.addSystem("localclaw ready. Type /help for commands.")
	if welcome := m.loadWelcomeMessage(); welcome != "" {
		m.addSystemMarkdown(welcome)
	}
	return m
}

func Run(ctx context.Context, app *runtime.App, cfg config.Config) error {
	m := newModel(ctx, app, cfg)
	p := newProgram(m)

	go func() {
		<-ctx.Done()
		p.Send(ctxDoneMsg{})
	}()

	return p.Start()
}

func newProgram(m tea.Model) *tea.Program {
	return tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
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
				m.updateSlashAutocomplete()
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

		if msg.Type == tea.KeyShiftTab {
			if m.moveSlashSelection(-1) {
				return m, nil
			}
		}

		if msg.Type == tea.KeyTab {
			if m.applySlashCompletion() {
				m.adjustInputHeight()
				m.layout()
				return m, nil
			}
		}

		if msg.Type == tea.KeyEnter && !msg.Alt && !key.Matches(msg, m.input.KeyMap.InsertNewline) {
			cmd := m.submitInput()
			return m, cmd
		}

		if key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+p", "alt+up"))) {
			if m.useHistory(-1) {
				m.updateSlashAutocomplete()
				m.adjustInputHeight()
				return m, nil
			}
		}

		if key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+n", "alt+down"))) {
			if m.useHistory(1) {
				m.updateSlashAutocomplete()
				m.adjustInputHeight()
				return m, nil
			}
		}

		if msg.Type == tea.KeyUp {
			if m.moveSlashSelection(-1) {
				return m, nil
			}
		}

		if msg.Type == tea.KeyDown {
			if m.moveSlashSelection(1) {
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
			m.addVerbose("stream: event channel closed")
			m.streamEvents = nil
			return m, nil
		}

		switch msg.Event.Type {
		case llm.StreamEventDelta:
			m.streamDeltaEvents++
			m.streamDeltaChars += len(msg.Event.Text)
			if m.streamDeltaEvents == 1 {
				m.addVerbose("stream: first delta received")
			}
			m.setStatus(statusStreaming)
			m.hasStreamDelta = true
			m.applyDelta(msg.Event.Text)
			m.refreshViewport(true)
		case llm.StreamEventFinal:
			m.applyFinal(msg.Event.Text)
			m.addVerbose("stream: final received delta_events=%d delta_chars=%d final_chars=%d", m.streamDeltaEvents, m.streamDeltaChars, len(strings.TrimSpace(msg.Event.Text)))
			m.finishRun(statusIdle)
			m.refreshViewport(true)
		case llm.StreamEventToolCall:
			toolName := "tool"
			toolClass := llm.ToolClassUnspecified
			if msg.Event.ToolCall != nil && strings.TrimSpace(msg.Event.ToolCall.Name) != "" {
				toolName = msg.Event.ToolCall.Name
			}
			if msg.Event.ToolCall != nil {
				toolClass = msg.Event.ToolCall.Class
				class := string(msg.Event.ToolCall.Class)
				if class == "" {
					class = "unspecified"
				}
				callID := strings.TrimSpace(msg.Event.ToolCall.ID)
				if callID != "" {
					m.toolCallOwnershipByID[callID] = toolClass
				}
				if callID == "" {
					callID = "n/a"
				}
				m.addVerbose("tool call details: id=%s class=%s args=%s", callID, class, summarizeVerboseMap(msg.Event.ToolCall.Args))
			}
			ownership := toolOwnershipLabel(toolClass)
			m.setStatus(fmt.Sprintf("tool [%s] %s", ownership, toolName))
			m.addSystem(fmt.Sprintf("tool call [%s]: %s", ownership, toolName))
			m.refreshViewport(true)
		case llm.StreamEventToolResult:
			if msg.Event.ToolResult != nil {
				toolName := msg.Event.ToolResult.Tool
				toolClass := msg.Event.ToolResult.Class
				callID := strings.TrimSpace(msg.Event.ToolResult.CallID)
				if toolClass == llm.ToolClassUnspecified && callID != "" {
					toolClass = m.toolCallOwnershipByID[callID]
				}
				if callID != "" {
					delete(m.toolCallOwnershipByID, callID)
				}
				if toolName == "" && msg.Event.ToolCall != nil {
					toolName = msg.Event.ToolCall.Name
				}
				ownership := toolOwnershipLabel(toolClass)
				if msg.Event.ToolResult.OK {
					m.addSystem(fmt.Sprintf("tool completed [%s]: %s", ownership, toolName))
				} else {
					if strings.TrimSpace(msg.Event.ToolResult.Error) != "" {
						m.addSystem(fmt.Sprintf("tool failed [%s]: %s (%s)", ownership, toolName, msg.Event.ToolResult.Error))
					} else {
						m.addSystem(fmt.Sprintf("tool failed [%s]: %s", ownership, toolName))
					}
				}
				if callID == "" {
					callID = "n/a"
				}
				status := strings.TrimSpace(msg.Event.ToolResult.Status)
				if status == "" {
					status = "n/a"
				}
				errText := strings.TrimSpace(msg.Event.ToolResult.Error)
				if errText == "" {
					errText = "none"
				}
				m.addVerbose("tool result details: call_id=%s tool=%s ok=%t status=%s error=%s data_keys=%s", callID, toolName, msg.Event.ToolResult.OK, status, truncateVerboseText(errText), summarizeVerboseKeys(msg.Event.ToolResult.Data))
			}
			if m.running {
				m.setStatus(statusWaiting)
			}
			m.refreshViewport(true)
		case llm.StreamEventProviderMetadata:
			if msg.Event.ProviderMetadata != nil {
				metadata := msg.Event.ProviderMetadata
				if provider := strings.TrimSpace(metadata.Provider); provider != "" {
					m.providerName = provider
				}
				if model := strings.TrimSpace(metadata.Model); model != "" {
					m.providerModel = model
				}
				m.providerTools = normalizeProviderToolList(metadata.Tools)
				m.addVerbose("provider metadata: provider=%s model=%s tools=%s", valueOrDefault(strings.TrimSpace(m.providerName), "n/a"), valueOrDefault(strings.TrimSpace(m.providerModel), "n/a"), summarizeVerboseList(m.providerTools))
			}
		}

		if m.streamEvents != nil {
			cmds = append(cmds, waitStreamEvent(m.activeRunID, m.streamEvents))
		}

	case streamErrMsg:
		if msg.RunID != m.activeRunID {
			return m, nil
		}
		if !msg.OK {
			m.addVerbose("stream: error channel closed")
			m.streamErrs = nil
			return m, nil
		}
		if msg.Err != nil {
			m.addVerbose("stream: error=%s", truncateVerboseText(msg.Err.Error()))
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
	m.updateSlashAutocomplete()
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
		base = m.currentThinkingMessage()
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
	hintText := "Enter send • Tab autocomplete • Ctrl+J newline • Ctrl+T thinking • /help"
	if panelInnerWidth(m.width) < 70 {
		hintText = "Enter send • Tab autocomplete • Ctrl+J newline • /help"
	}
	if panelInnerWidth(m.width) < 42 {
		hintText = "Enter send • Tab complete • /help"
	}
	hint := inputHintStyle.Render(truncateText(hintText, panelInnerWidth(m.width)))
	body := m.input.View()
	if menu := m.slashMenuView(); menu != "" {
		body += "\n" + menu
	}
	body += "\n" + hint
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
	m.updateSlashAutocomplete()
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
		m.addSystem(slashHelpText())
	case "status":
		m.addSystem(fmt.Sprintf("status=%s model=%s agent=%s session=%s workspace=%s thinking=%s verbose=%s", m.status, m.cfg.LLM.Provider, m.agentID, m.sessionID, m.workspacePath, onOff(m.showThinking), onOff(m.verbose)))
	case "tools":
		m.addSystem(m.toolsSummary())
	case "clear":
		m.messages = nil
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

func (m *model) toolsSummary() string {
	provider := strings.TrimSpace(m.providerName)
	if provider == "" {
		provider = strings.TrimSpace(m.cfg.LLM.Provider)
	}
	if provider == "" {
		provider = "unknown"
	}

	lines := []string{fmt.Sprintf("provider=%s", provider)}
	if strings.TrimSpace(m.providerModel) != "" {
		lines = append(lines, "provider model: "+m.providerModel)
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
		line := fmt.Sprintf("%-22s %s", formatSlashUsage(cmd), cmd.Description)
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
		lines = append(lines, fmt.Sprintf("%-22s %s", formatSlashUsage(cmd), cmd.Description))
	}
	return strings.Join(lines, "\n")
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

func (m *model) startRun(input string) {
	resetToolCallOwnershipByID(m.toolCallOwnershipByID)
	m.runSeq++
	m.activeRunID = m.runSeq
	m.running = true
	m.hasStreamDelta = false
	m.streamDeltaEvents = 0
	m.streamDeltaChars = 0
	m.activeAssistantIdx = -1
	m.activeThinkingMessage = m.nextThinkingMessage()
	m.setStatus(statusSending)
	m.emitVerboseRunStartDiagnostics(input)

	m.addUser(input)
	if m.app == nil {
		m.addSystem("runtime unavailable")
		m.finishRun(statusError)
		return
	}

	userTokenDelta := memory.EstimateTokensFromText(input)
	if m.app != nil {
		if err := m.app.AddSessionTokens(m.ctx, m.agentID, m.sessionID, userTokenDelta); err != nil {
			m.addVerbose("transcript write: role=user token_update_error=%s", truncateVerboseText(err.Error()))
		}
		if err := m.app.AppendSessionTranscriptMessage(m.ctx, m.agentID, m.sessionID, "user", input); err != nil {
			m.addVerbose("transcript write: role=user append_error=%s", truncateVerboseText(err.Error()))
		} else {
			m.addVerbose("transcript write: role=user chars=%d tokens=%d", len(strings.TrimSpace(input)), userTokenDelta)
		}
		m.app.RunMemoryFlushIfNeededAsync(m.ctx, m.agentID, m.sessionID)
		m.addVerbose("runtime: memory flush check queued")
	}

	runCtx, cancel := context.WithCancel(m.ctx)
	m.runCancel = cancel
	m.streamEvents, m.streamErrs = m.app.PromptStreamForSession(runCtx, m.agentID, m.sessionID, input)
	m.setStatus(statusWaiting)
}

func (m *model) finishRun(finalStatus string) {
	m.running = false
	m.setStatus(finalStatus)
	m.activeThinkingMessage = ""
	resetToolCallOwnershipByID(m.toolCallOwnershipByID)
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
	resetToolCallOwnershipByID(m.toolCallOwnershipByID)
	if m.running {
		m.running = false
		m.activeRunID = 0
		m.activeAssistantIdx = -1
		m.activeThinkingMessage = ""
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
		assistantTokenDelta := memory.EstimateTokensFromText(msg.Raw)
		if err := m.app.AddSessionTokens(m.ctx, m.agentID, m.sessionID, assistantTokenDelta); err != nil {
			m.addVerbose("transcript write: role=assistant token_update_error=%s", truncateVerboseText(err.Error()))
		}
		if err := m.app.AppendSessionTranscriptMessage(m.ctx, m.agentID, m.sessionID, "assistant", msg.Raw); err != nil {
			m.addVerbose("transcript write: role=assistant append_error=%s", truncateVerboseText(err.Error()))
		} else {
			m.addVerbose("transcript write: role=assistant chars=%d tokens=%d", len(strings.TrimSpace(msg.Raw)), assistantTokenDelta)
		}
	}
	msg.Streaming = false
	msg.ThinkingPlaceholder = false
}

func (m *model) runSessionReset(startNew bool, source string) {
	m.abortRun("")
	if m.app != nil {
		next := m.app.ResetSession(m.ctx, runtime.ResetSessionRequest{
			AgentID:   m.agentID,
			SessionID: m.sessionID,
			Source:    source,
			StartNew:  startNew,
		})
		m.agentID = next.AgentID
		m.sessionID = next.SessionID
		m.sessionKey = next.SessionKey
	}
	m.messages = nil
	if startNew {
		m.addSystem(fmt.Sprintf("started new session %s", m.sessionID))
		if welcome := m.loadWelcomeMessage(); welcome != "" {
			m.addSystemMarkdown(welcome)
		}
	} else {
		m.addSystem("session reset")
	}
}

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
			if msg.RenderMarkdown {
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

func waitStreamEvent(runID int, ch <-chan llm.StreamEvent) tea.Cmd {
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

func resolveThinkingMessages(messages []string) []string {
	resolved := make([]string, 0, len(messages))
	for _, message := range messages {
		trimmed := strings.TrimSpace(message)
		if trimmed == "" {
			continue
		}
		resolved = append(resolved, trimmed)
	}
	if len(resolved) == 0 {
		return []string{"thinking"}
	}
	return resolved
}

func (m *model) nextThinkingMessage() string {
	if len(m.thinkingMessages) == 0 {
		return "thinking"
	}
	idx := m.thinkingMessageIdx % len(m.thinkingMessages)
	message := m.thinkingMessages[idx]
	m.thinkingMessageIdx = (m.thinkingMessageIdx + 1) % len(m.thinkingMessages)
	return message
}

func (m *model) currentThinkingMessage() string {
	if strings.TrimSpace(m.activeThinkingMessage) != "" {
		return m.activeThinkingMessage
	}
	if len(m.thinkingMessages) > 0 {
		return m.thinkingMessages[0]
	}
	return "thinking"
}

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

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
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
			BlockPrefix: "• ",
			Color:       strPtr("#fab283"),
		},
		Enumeration: ansi.StylePrimitive{
			BlockPrefix: ". ",
			Color:       strPtr("#56b6c2"),
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
