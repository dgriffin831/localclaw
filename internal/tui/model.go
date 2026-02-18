package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/glamour"

	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/llm"
	"github.com/dgriffin831/localclaw/internal/runtime"
)

const (
	statusIdle              = "idle"
	statusSending           = "sending"
	statusWaiting           = "waiting"
	statusStreaming         = "streaming"
	statusAborted           = "aborted"
	statusError             = "error"
	slashMenuLimit          = 6
	composerMinLines        = 4
	composerMaxLines        = 12
	composerPlaceholderText = "Ask localclaw..."
	composerPlaceholderFill = '\u200b'
	composerPrompt          = "> "
	composerIndent          = "  "
	welcomeFileName         = "WELCOME.md"
	bootstrapFileName       = "BOOTSTRAP.md"
	bootstrapSeedText       = "Wake up, my friend!"
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
	RenderMarkdown      bool
	Streaming           bool
	ThinkingPlaceholder bool
	ToolCard            *toolCardMessage
}

type toolCardMessage struct {
	CallID    string
	ToolName  string
	Args      map[string]interface{}
	HasResult bool
	OK        bool
	Status    string
	Error     string
	Data      map[string]interface{}
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
type bootstrapSeedTriggerMsg struct{}
type providerToolsDiscoveredMsg struct {
	Provider string
	Model    string
	Tools    []string
	Err      error
}

type providerModelsDiscoveredMsg struct {
	Catalogs map[string]llm.ProviderModelCatalog
	Errors   map[string]string
}

type model struct {
	ctx context.Context
	app *runtime.App
	cfg config.Config
	// Runtime-resolved identity and paths shared across runtime/TUI/CLI.
	agentID       string
	sessionID     string
	sessionKey    string
	sessionTokens int
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

	verbose                         bool
	toolsExpanded                   bool
	mouseEnabled                    bool
	thinkingMessages                []string
	thinkingMessageIdx              int
	activeThinkingMessage           string
	providerName                    string
	providerModel                   string
	providerOverride                string
	modelOverride                   string
	reasoningOverride               string
	providerTools                   []string
	providerToolsDiscoveryInFlight  bool
	providerModelCatalogs           map[string]llm.ProviderModelCatalog
	providerModelCatalogErrors      map[string]string
	providerModelsDiscoveryInFlight bool
	toolCardIndexByCallID           map[string]int

	streamDeltaEvents int
	streamDeltaChars  int

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
	queuedInputs []string

	slashQuery    string
	slashMatches  []slashCommandDef
	slashSelected int

	lastCtrlC time.Time
}

var (
	colorPrimary        = lipgloss.Color("#fab283")
	colorError          = lipgloss.Color("#e06c75")
	colorWarning        = lipgloss.Color("#f5a742")
	colorText           = lipgloss.Color("#eeeeee")
	colorTextMuted      = lipgloss.Color("#808080")
	colorBackground     = lipgloss.Color("#0a0a0a")
	colorBackgroundPane = lipgloss.Color("#141414")
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
			Foreground(colorText).
			Background(colorBackground)

	statusBusyStyle = panelRowStyle.Copy().
			Foreground(colorText).
			Background(colorBackground)

	statusErrStyle = panelRowStyle.Copy().
			Foreground(colorError).
			Bold(true)

	inputStyle = panelRowStyle.Copy().
			BorderForeground(colorBorderSubtle).
			Background(colorBackgroundPane)

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
	workspacePath := cfg.Agents.Defaults.Workspace
	if app != nil {
		if resolvedPath, err := app.ResolveWorkspacePath(resolution.AgentID); err == nil {
			workspacePath = resolvedPath
		}
	}

	input := textarea.New()
	input.Placeholder = composerPlaceholder()
	input.Focus()
	input.ShowLineNumbers = false
	// Keep a practical ceiling to avoid unbounded composer growth.
	input.CharLimit = 100000
	// Render a single top-row prompt in view_layout to avoid per-line prompts.
	input.Prompt = ""
	input.SetHeight(composerMinLines)
	input.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter"))

	styles := textarea.DefaultStyles(true)
	styles.Focused.Base = styles.Focused.Base.Background(colorBackgroundPane).Foreground(colorText)
	styles.Focused.Text = lipgloss.NewStyle().Foreground(colorText).Background(colorBackgroundPane)
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(colorPrimary)
	styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(colorTextMuted).Background(colorBackgroundPane)
	styles.Focused.CursorLine = lipgloss.NewStyle().Foreground(colorText).Background(colorBackgroundPane)
	styles.Focused.CursorLineNumber = lipgloss.NewStyle().Foreground(colorTextMuted)
	styles.Focused.LineNumber = lipgloss.NewStyle().Foreground(colorTextMuted)
	styles.Focused.EndOfBuffer = lipgloss.NewStyle().Foreground(colorBorderSubtle)
	styles.Blurred = styles.Focused
	styles.Blurred.Prompt = lipgloss.NewStyle().Foreground(colorTextMuted)
	// Use a visible accent cursor while keeping terminal-default reverse-video
	// escapes out of placeholder mode.
	styles.Cursor.Color = colorPrimary
	input.SetStyles(styles)

	sp := spinner.New()
	sp.Spinner = statusIconSpinner(statusIdle)
	sp.Style = lipgloss.NewStyle().Foreground(colorText)

	vp := viewport.New()
	vp.KeyMap = transcriptViewportKeyMap()
	vp.MouseWheelEnabled = true

	m := model{
		ctx:                        ctx,
		app:                        app,
		cfg:                        cfg,
		agentID:                    resolution.AgentID,
		sessionID:                  resolution.SessionID,
		sessionKey:                 resolution.SessionKey,
		workspacePath:              workspacePath,
		viewport:                   vp,
		input:                      input,
		spinner:                    sp,
		status:                     statusIdle,
		verbose:                    cfg.App.Default.Verbose,
		toolsExpanded:              cfg.App.Default.Tools,
		mouseEnabled:               cfg.App.Default.Mouse,
		historyIdx:                 -1,
		activeAssistantIdx:         -1,
		thinkingMessages:           resolveThinkingMessages(cfg.App.ThinkingMessages),
		providerName:               strings.TrimSpace(cfg.LLM.Provider),
		providerModelCatalogs:      map[string]llm.ProviderModelCatalog{},
		providerModelCatalogErrors: map[string]string{},
		toolCardIndexByCallID:      map[string]int{},
	}
	m.syncSessionMetadata()
	m.addSystem("localclaw ready. Type /help for commands.")
	if welcome := m.loadWelcomeMessage(); welcome != "" {
		m.addSystemMarkdown(welcome)
	}
	return m
}

func transcriptViewportKeyMap() viewport.KeyMap {
	return viewport.KeyMap{
		PageDown: key.NewBinding(key.WithKeys("pgdown")),
		PageUp:   key.NewBinding(key.WithKeys("pgup")),
		Up:       key.NewBinding(key.WithKeys("ctrl+up")),
		Down:     key.NewBinding(key.WithKeys("ctrl+down")),
	}
}

func composerPlaceholder() string {
	if composerMaxLines <= 1 {
		return composerPlaceholderText
	}
	// The textarea placeholder path leaves trailing cells unstyled on rows
	// beyond placeholder text. Invisible filler lines keep full-row pane
	// background rendering consistent until the first typed character.
	fillerLine := string(composerPlaceholderFill)
	filler := strings.TrimSuffix(strings.Repeat(fillerLine+"\n", composerMaxLines-1), "\n")
	return composerPlaceholderText + "\n" + filler
}

func (m *model) syncSessionMetadata() {
	if m.app == nil {
		return
	}
	entry, err := m.app.MCPSessionStatus(m.ctx, m.agentID, m.sessionID)
	if err != nil {
		if errors.Is(err, runtime.ErrMCPNotFound) {
			m.sessionTokens = 0
		}
		return
	}
	if entry.TotalTokens < 0 {
		m.sessionTokens = 0
		return
	}
	m.sessionTokens = entry.TotalTokens
}
