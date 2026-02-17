package tui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/runtime"
)

func Run(ctx context.Context, app *runtime.App, cfg config.Config) error {
	m := newModel(ctx, app, cfg)
	p := newProgram(m)

	go func() {
		<-ctx.Done()
		p.Send(ctxDoneMsg{})
	}()

	return p.Start()
}

func newProgram(m model) *tea.Program {
	options := []tea.ProgramOption{tea.WithAltScreen()}
	if m.mouseEnabled {
		options = append(options, tea.WithMouseCellMotion())
	}
	return tea.NewProgram(m, options...)
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
		handled, cmd := m.handleKeyMsg(msg)
		if handled {
			return m, cmd
		}

	case spinner.TickMsg:
		if cmd := m.handleSpinnerTick(msg); cmd != nil {
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
		m.handleStreamEvent(msg.Event)
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
			m.addSystem("prompt error: " + msg.Err.Error())
			m.finishRun(statusError)
			m.refreshViewport(true)
			return m, nil
		}

	case providerToolsDiscoveredMsg:
		m.providerToolsDiscoveryInFlight = false
		if provider := strings.TrimSpace(msg.Provider); provider != "" {
			m.providerName = provider
		}
		if model := strings.TrimSpace(msg.Model); model != "" {
			m.providerModel = model
		}
		if msg.Err != nil {
			m.addVerbose("provider tools discovery failed: %s", truncateVerboseText(msg.Err.Error()))
			m.addSystem("provider tools discovery failed: " + msg.Err.Error())
			m.addSystem(m.toolsSummary())
			m.refreshViewport(true)
			return m, nil
		}
		m.providerTools = normalizeProviderToolList(msg.Tools)
		m.addSystem(m.toolsSummary())
		m.refreshViewport(true)
		return m, nil
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
	gap := m.composerGapView()
	input := m.inputView()
	footer := m.composerFooterView()

	parts := make([]string, 0, 6)
	if header != "" {
		parts = append(parts, header)
	}
	parts = append(parts, m.viewport.View(), gap)
	if status != "" {
		parts = append(parts, status)
	}
	parts = append(parts, input, footer)
	content := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return lipgloss.NewStyle().
		Background(colorBackground).
		Foreground(colorText).
		Render(content)
}
