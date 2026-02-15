package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/dgriffin831/localclaw/internal/channels/signal"
	"github.com/dgriffin831/localclaw/internal/channels/slack"
	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/cron"
	"github.com/dgriffin831/localclaw/internal/heartbeat"
	"github.com/dgriffin831/localclaw/internal/llm/claudecode"
	"github.com/dgriffin831/localclaw/internal/memory"
	"github.com/dgriffin831/localclaw/internal/session"
	"github.com/dgriffin831/localclaw/internal/skills"
	"github.com/dgriffin831/localclaw/internal/workspace"
)

// App composes all localclaw capabilities in a single process.
type App struct {
	cfg       config.Config
	memory    memory.Store
	sessions  *session.Store
	workspace workspace.Manager
	skills    skills.Registry
	cron      cron.Scheduler
	heartbeat heartbeat.Monitor
	slack     slack.Client
	signal    signal.Client
	llm       claudecode.Client
}

const (
	DefaultAgentID   = "default"
	DefaultSessionID = "main"
)

type SessionResolution struct {
	AgentID    string
	SessionID  string
	SessionKey string
}

func ResolveAgentID(agentID string) string {
	trimmed := strings.TrimSpace(agentID)
	if trimmed == "" {
		return DefaultAgentID
	}
	return trimmed
}

func ResolveSessionID(sessionID string) string {
	trimmed := strings.TrimSpace(sessionID)
	if trimmed == "" {
		return DefaultSessionID
	}
	return trimmed
}

func ResolveSession(agentID, sessionID string) SessionResolution {
	resolvedAgentID := ResolveAgentID(agentID)
	resolvedSessionID := ResolveSessionID(sessionID)
	return SessionResolution{
		AgentID:    resolvedAgentID,
		SessionID:  resolvedSessionID,
		SessionKey: fmt.Sprintf("%s/%s", resolvedAgentID, resolvedSessionID),
	}
}

func New(cfg config.Config) (*App, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	agentWorkspaces := make(map[string]string, len(cfg.Agents.List))
	agentIDs := make([]string, 0, len(cfg.Agents.List))
	for _, agent := range cfg.Agents.List {
		agentWorkspaces[agent.ID] = agent.Workspace
		agentIDs = append(agentIDs, agent.ID)
	}

	return &App{
		cfg:    cfg,
		memory: memory.NewLocalStore(cfg.Memory.Path),
		sessions: session.NewStore(session.Settings{
			StateRoot:     cfg.State.Root,
			StorePath:     cfg.Session.Store,
			KnownAgentIDs: agentIDs,
		}),
		workspace: workspace.NewLocalManager(workspace.Settings{
			StateRoot:        cfg.State.Root,
			DefaultWorkspace: cfg.Agents.Defaults.Workspace,
			AgentWorkspaces:  agentWorkspaces,
		}),
		skills:    skills.NewLocalRegistry(),
		cron:      cron.NewInProcessScheduler(cfg.Cron.Enabled),
		heartbeat: heartbeat.NewLocalMonitor(cfg.Heartbeat.Enabled, cfg.Heartbeat.IntervalSeconds),
		slack:     slack.NewLocalAdapter(),
		signal:    signal.NewLocalAdapter(),
		llm: claudecode.NewClient(claudecode.Settings{
			BinaryPath:    cfg.LLM.ClaudeCode.BinaryPath,
			Profile:       cfg.LLM.ClaudeCode.Profile,
			UseGovCloud:   cfg.LLM.ClaudeCode.UseGovCloud,
			BedrockRegion: cfg.LLM.ClaudeCode.BedrockRegion,
			AuthMode:      cfg.LLM.ClaudeCode.AuthMode,
		}),
	}, nil
}

func (a *App) ResolveWorkspacePath(agentID string) (string, error) {
	return a.workspace.ResolveWorkspace(ResolveAgentID(agentID))
}

func (a *App) ResolveSessionsPath(agentID string) (string, error) {
	return a.sessions.ResolveSessionsPath(ResolveAgentID(agentID))
}

func (a *App) ResolveTranscriptPath(agentID, sessionID string) (string, error) {
	resolution := ResolveSession(agentID, sessionID)
	return a.sessions.ResolveTranscriptPath(resolution.AgentID, resolution.SessionID)
}

func (a *App) Run(ctx context.Context) error {
	if err := a.workspace.Init(ctx); err != nil {
		return fmt.Errorf("workspace init: %w", err)
	}
	if err := a.memory.Init(ctx); err != nil {
		return fmt.Errorf("memory init: %w", err)
	}
	if err := a.sessions.Init(ctx); err != nil {
		return fmt.Errorf("session init: %w", err)
	}
	if err := a.skills.Load(ctx); err != nil {
		return fmt.Errorf("skills load: %w", err)
	}
	if err := a.cron.Start(ctx); err != nil {
		return fmt.Errorf("cron start: %w", err)
	}
	if err := a.heartbeat.Ping(ctx, "localclaw startup heartbeat"); err != nil {
		return fmt.Errorf("heartbeat ping: %w", err)
	}
	return nil
}

// Prompt sends a single input to the configured local LLM client.
func (a *App) Prompt(ctx context.Context, input string) (string, error) {
	return a.llm.Prompt(ctx, input)
}

// PromptStream sends input to the local LLM client and yields incremental output events.
func (a *App) PromptStream(ctx context.Context, input string) (<-chan claudecode.StreamEvent, <-chan error) {
	return a.llm.PromptStream(ctx, input)
}
