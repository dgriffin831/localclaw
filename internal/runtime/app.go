package runtime

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/dgriffin831/localclaw/internal/channels/signal"
	"github.com/dgriffin831/localclaw/internal/channels/slack"
	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/cron"
	"github.com/dgriffin831/localclaw/internal/heartbeat"
	"github.com/dgriffin831/localclaw/internal/hooks"
	"github.com/dgriffin831/localclaw/internal/llm/claudecode"
	"github.com/dgriffin831/localclaw/internal/memory"
	"github.com/dgriffin831/localclaw/internal/session"
	"github.com/dgriffin831/localclaw/internal/skills"
	"github.com/dgriffin831/localclaw/internal/workspace"
)

// App composes all localclaw capabilities in a single process.
type App struct {
	cfg        config.Config
	memory     memory.Store
	sessions   *session.Store
	workspace  workspace.Manager
	skills     skills.Registry
	cron       cron.Scheduler
	heartbeat  heartbeat.Monitor
	slack      slack.Client
	signal     signal.Client
	llm        claudecode.Client
	transcript *session.TranscriptWriter
	now        func() time.Time
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

type ResetSessionRequest struct {
	AgentID   string
	SessionID string
	Source    string
	StartNew  bool
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
		transcript: session.NewTranscriptWriter(session.TranscriptWriterSettings{}),
		now:        time.Now,
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

func (a *App) AddSessionTokens(ctx context.Context, agentID, sessionID string, delta int) error {
	if delta <= 0 {
		return nil
	}
	resolution := ResolveSession(agentID, sessionID)
	_, err := a.sessions.Update(ctx, resolution.AgentID, resolution.SessionID, func(entry *session.SessionEntry) error {
		entry.Key = resolution.SessionKey
		entry.TotalTokens += delta
		return nil
	})
	return err
}

func (a *App) RunMemoryFlushIfNeeded(ctx context.Context, agentID, sessionID string) error {
	resolution := ResolveSession(agentID, sessionID)
	workspacePath, err := a.ResolveWorkspacePath(resolution.AgentID)
	if err != nil {
		return err
	}

	cfg := a.resolveMemoryFlushConfig(resolution.AgentID)
	if !cfg.Enabled {
		return nil
	}
	_, err = memory.MaybeRunMemoryFlush(ctx, memory.FlushRunRequest{
		AgentID:           resolution.AgentID,
		SessionID:         resolution.SessionID,
		SessionKey:        resolution.SessionKey,
		WorkspacePath:     workspacePath,
		WorkspaceWritable: memory.IsWorkspaceWritable(workspacePath),
		Settings: memory.FlushSettings{
			Enabled:                   cfg.Enabled,
			CompactionThresholdTokens: cfg.ThresholdTokens,
			TriggerWindowTokens:       cfg.TriggerWindowTokens,
			Prompt:                    cfg.Prompt,
			Timeout:                   time.Duration(cfg.TimeoutSeconds) * time.Second,
		},
	}, a.sessions, a.llm)
	return err
}

func (a *App) RunMemoryFlushIfNeededAsync(ctx context.Context, agentID, sessionID string) {
	go func() {
		_ = a.RunMemoryFlushIfNeeded(ctx, agentID, sessionID)
	}()
}

func (a *App) AppendSessionTranscriptMessage(ctx context.Context, agentID, sessionID, role, content string) error {
	if a.transcript == nil {
		return nil
	}
	resolution := ResolveSession(agentID, sessionID)
	transcriptPath, err := a.ResolveTranscriptPath(resolution.AgentID, resolution.SessionID)
	if err != nil {
		return err
	}
	return a.transcript.AppendMessage(ctx, transcriptPath, session.TranscriptMessage{
		Role:    role,
		Content: content,
	})
}

func (a *App) ResetSession(ctx context.Context, req ResetSessionRequest) SessionResolution {
	current := ResolveSession(req.AgentID, req.SessionID)
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "session-reset"
	}

	workspacePath, workspaceErr := a.ResolveWorkspacePath(current.AgentID)
	transcriptPath, transcriptErr := a.ResolveTranscriptPath(current.AgentID, current.SessionID)
	if workspaceErr != nil || transcriptErr != nil {
		log.Printf("session reset hook skipped for %s: workspaceErr=%v transcriptErr=%v", current.SessionKey, workspaceErr, transcriptErr)
	} else {
		_, err := hooks.RunSessionMemorySnapshot(ctx, hooks.SessionMemorySnapshotRequest{
			AgentID:        current.AgentID,
			SessionID:      current.SessionID,
			SessionKey:     current.SessionKey,
			Source:         source,
			WorkspacePath:  workspacePath,
			TranscriptPath: transcriptPath,
			PromptClient:   a.llm,
			Now:            a.now,
		})
		if err != nil {
			log.Printf("session reset hook failed for %s: %v", current.SessionKey, err)
		}
	}

	if !req.StartNew {
		return current
	}
	nextID := a.nextSessionID(ctx, current.AgentID, current.SessionID)
	return ResolveSession(current.AgentID, nextID)
}

func (a *App) nextSessionID(ctx context.Context, agentID, currentSessionID string) string {
	base := fmt.Sprintf("s-%s", a.now().UTC().Format("20060102-150405"))
	for i := 1; ; i++ {
		candidate := base
		if i > 1 {
			candidate = fmt.Sprintf("%s-%d", base, i)
		}
		if candidate == currentSessionID {
			continue
		}
		if a.sessions != nil {
			if _, exists, err := a.sessions.Get(ctx, agentID, candidate); err == nil && exists {
				continue
			}
		}
		if transcriptPath, err := a.ResolveTranscriptPath(agentID, candidate); err == nil {
			if _, err := os.Stat(transcriptPath); err == nil {
				continue
			}
		}
		return candidate
	}
}

func (a *App) resolveMemoryFlushConfig(agentID string) config.MemoryFlushConfig {
	resolved := a.cfg.Agents.Defaults.Compaction.MemoryFlush
	normalizedAgentID := ResolveAgentID(agentID)
	for _, agent := range a.cfg.Agents.List {
		if ResolveAgentID(agent.ID) != normalizedAgentID {
			continue
		}
		override := agent.Compaction.MemoryFlush
		if !hasMemoryFlushOverride(override) {
			break
		}
		resolved = mergeMemoryFlushConfig(resolved, override)
		break
	}
	return resolved
}

func hasMemoryFlushOverride(cfg config.MemoryFlushConfig) bool {
	return cfg.Enabled ||
		cfg.ThresholdTokens > 0 ||
		cfg.TriggerWindowTokens > 0 ||
		strings.TrimSpace(cfg.Prompt) != "" ||
		cfg.TimeoutSeconds > 0
}

func mergeMemoryFlushConfig(base, override config.MemoryFlushConfig) config.MemoryFlushConfig {
	merged := base
	if override.Enabled {
		merged.Enabled = true
	}
	if override.ThresholdTokens > 0 {
		merged.ThresholdTokens = override.ThresholdTokens
	}
	if override.TriggerWindowTokens > 0 {
		merged.TriggerWindowTokens = override.TriggerWindowTokens
	}
	if strings.TrimSpace(override.Prompt) != "" {
		merged.Prompt = override.Prompt
	}
	if override.TimeoutSeconds > 0 {
		merged.TimeoutSeconds = override.TimeoutSeconds
	}
	return merged
}
