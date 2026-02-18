package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/llm"
	"github.com/dgriffin831/localclaw/internal/runtime"
)

const (
	statusIdle        = "idle"
	statusSending     = "sending"
	statusWaiting     = "waiting"
	statusStreaming   = "streaming"
	statusAborted     = "aborted"
	statusError       = "error"
	slashMenuLimit    = 6
	composerMinLines  = 4
	composerMaxLines  = 12
	composerPrompt    = "> "
	composerIndent    = "  "
	welcomeFileName   = "WELCOME.md"
	bootstrapFileName = "BOOTSTRAP.md"
	bootstrapSeedText = "Wake up, my friend!"
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
	input.Placeholder = "Ask localclaw..."
	input.Focus()
	input.ShowLineNumbers = false
	// bubbles/textarea v0.13.0 treats CharLimit <= 0 as effectively no input.
	// Use a high practical ceiling instead of 0.
	input.CharLimit = 100000
	// Render a single top-row prompt in view_layout to avoid per-line prompts.
	input.Prompt = ""
	input.SetHeight(composerMinLines)
	input.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("ctrl+j"))

	focused, blurred := textarea.DefaultStyles()
	focused.Base = focused.Base.Background(colorBackgroundPane).Foreground(colorText)
	focused.Text = lipgloss.NewStyle().Foreground(colorText).Background(colorBackgroundPane)
	focused.Prompt = lipgloss.NewStyle().Foreground(colorPrimary)
	focused.Placeholder = lipgloss.NewStyle().Foreground(colorTextMuted).Background(colorBackgroundPane)
	focused.CursorLine = lipgloss.NewStyle().Foreground(colorText).Background(colorBackgroundPane)
	focused.CursorLineNumber = lipgloss.NewStyle().Foreground(colorTextMuted)
	focused.LineNumber = lipgloss.NewStyle().Foreground(colorTextMuted)
	focused.EndOfBuffer = lipgloss.NewStyle().Foreground(colorBorderSubtle)
	blurred = focused
	blurred.Prompt = lipgloss.NewStyle().Foreground(colorTextMuted)
	input.FocusedStyle = focused
	input.BlurredStyle = blurred

	sp := spinner.New()
	sp.Spinner = statusIconSpinner(statusIdle)
	sp.Style = lipgloss.NewStyle().Foreground(colorText)

	vp := viewport.New(0, 0)
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
