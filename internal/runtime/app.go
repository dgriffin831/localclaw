package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dgriffin831/localclaw/internal/channels/signal"
	"github.com/dgriffin831/localclaw/internal/channels/slack"
	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/cron"
	"github.com/dgriffin831/localclaw/internal/heartbeat"
	"github.com/dgriffin831/localclaw/internal/hooks"
	"github.com/dgriffin831/localclaw/internal/llm"
	"github.com/dgriffin831/localclaw/internal/llm/claudecode"
	"github.com/dgriffin831/localclaw/internal/llm/codex"
	"github.com/dgriffin831/localclaw/internal/memory"
	"github.com/dgriffin831/localclaw/internal/session"
	"github.com/dgriffin831/localclaw/internal/skills"
	"github.com/dgriffin831/localclaw/internal/workspace"
)

// App composes all localclaw capabilities in a single process.
type App struct {
	cfg             config.Config
	enabledChannels map[string]struct{}
	tools           *skills.ToolRegistry
	sessions        *session.Store
	workspace       workspace.Manager
	skills          skills.Registry
	cron            cron.Scheduler
	heartbeat       heartbeat.Monitor
	slack           slack.Client
	signal          signal.Client
	signalReceive   func(ctx context.Context, settings signal.ReceiveSettings) ([]signal.ReceiveMessage, error)
	llm             llm.Client
	llmClients      map[string]llm.Client
	transcript      *session.TranscriptWriter
	now             func() time.Time
	heartbeatLogf   func(format string, args ...interface{})

	snapshotMu          sync.Mutex
	skillPromptSnapshot map[string]skillsSessionSnapshot

	providerModelsMu    sync.Mutex
	providerModelsCache map[string]llm.ProviderModelCatalog
}

type skillsSessionSnapshot struct {
	CompactionCount int
	Prompt          string
}

const (
	DefaultAgentID   = "default"
	DefaultSessionID = "main"
	// Keep this prompt aligned with OpenClaw heartbeat defaults so behavior is consistent.
	defaultHeartbeatPrompt = "Read HEARTBEAT.md if it exists (workspace context). Follow it strictly. Do not infer or repeat old tasks from prior chats. If nothing needs attention, reply HEARTBEAT_OK."
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
	resolvedStateRoot, err := resolveAbsolutePath(cfg.App.Root)
	if err != nil {
		return nil, fmt.Errorf("resolve state root: %w", err)
	}

	agentWorkspaces := make(map[string]string, len(cfg.Agents.List))
	agentIDs := make([]string, 0, len(cfg.Agents.List))
	for _, agent := range cfg.Agents.List {
		agentWorkspaces[agent.ID] = agent.Workspace
		agentIDs = append(agentIDs, agent.ID)
	}

	workspaceManager := workspace.NewLocalManager(workspace.Settings{
		StateRoot:        cfg.App.Root,
		DefaultWorkspace: cfg.Agents.Defaults.Workspace,
		AgentWorkspaces:  agentWorkspaces,
	})
	heartbeatLogf := newStateFileLogger(resolvedStateRoot, "heartbeats.log")
	cronLogf := newStateFileLogger(resolvedStateRoot, "crons.log")
	claudeClient := claudecode.NewClient(claudecode.Settings{
		BinaryPath:          cfg.LLM.ClaudeCode.BinaryPath,
		Profile:             cfg.LLM.ClaudeCode.Profile,
		SecurityMode:        cfg.Security.Mode,
		ExtraArgs:           cfg.LLM.ClaudeCode.ExtraArgs,
		SessionMode:         cfg.LLM.ClaudeCode.SessionMode,
		SessionArg:          cfg.LLM.ClaudeCode.SessionArg,
		ResumeArgs:          cfg.LLM.ClaudeCode.ResumeArgs,
		SessionIDFields:     cfg.LLM.ClaudeCode.SessionIDFields,
		StrictMCPConfig:     true,
		MCPConfigDir:        filepath.Join(resolvedStateRoot, "runtime", "mcp"),
		MCPServerBinaryPath: "localclaw",
		MCPServerArgs:       []string{"mcp", "serve"},
	})
	codexClient := codex.NewClient(codex.Settings{
		BinaryPath:       cfg.LLM.Codex.BinaryPath,
		Profile:          cfg.LLM.Codex.Profile,
		Model:            cfg.LLM.Codex.Model,
		ReasoningDefault: cfg.LLM.Codex.ReasoningDefault,
		SecurityMode:     cfg.Security.Mode,
		ExtraArgs:        cfg.LLM.Codex.ExtraArgs,
		SessionMode:      cfg.LLM.Codex.SessionMode,
		ResumeArgs:       cfg.LLM.Codex.ResumeArgs,
		SessionIDFields:  cfg.LLM.Codex.SessionIDFields,
		ResumeOutput:     cfg.LLM.Codex.ResumeOutput,
		WorkingDirectory: cfg.Agents.Defaults.Workspace,
		MCP: codex.MCPSettings{
			ConfigPath:       cfg.LLM.Codex.MCP.ConfigPath,
			ServerName:       cfg.LLM.Codex.MCP.ServerName,
			ServerBinaryPath: "localclaw",
			ServerArgs:       []string{"mcp", "serve"},
		},
	})

	llmClients := map[string]llm.Client{
		"claudecode": claudeClient,
		"codex":      codexClient,
	}

	provider := strings.ToLower(strings.TrimSpace(cfg.LLM.Provider))
	llmClient, ok := llmClients[provider]
	if !ok {
		return nil, fmt.Errorf("unsupported llm provider %q", cfg.LLM.Provider)
	}
	switch provider {
	case "claudecode":
		if err := claudeClient.ValidateMCPWiring(); err != nil {
			return nil, fmt.Errorf("invalid claude mcp wiring: %w", err)
		}
	case "codex":
		if err := codexClient.ValidateMCPWiring(); err != nil {
			return nil, fmt.Errorf("invalid codex mcp wiring: %w", err)
		}
	}

	enabledChannels := make(map[string]struct{}, len(cfg.Channels.Enabled))
	for _, channel := range cfg.Channels.Enabled {
		name := strings.ToLower(strings.TrimSpace(channel))
		if name == "" {
			continue
		}
		enabledChannels[name] = struct{}{}
	}

	var slackAdapter slack.Client
	if _, ok := enabledChannels["slack"]; ok {
		slackAdapter = slack.NewLocalAdapter(slack.Settings{
			TokenEnv:       cfg.Channels.Slack.BotTokenEnv,
			DefaultChannel: cfg.Channels.Slack.DefaultChannel,
			APIBaseURL:     cfg.Channels.Slack.APIBaseURL,
			Timeout:        time.Duration(cfg.Channels.Slack.TimeoutSeconds) * time.Second,
		})
	}

	var signalAdapter signal.Client
	if _, ok := enabledChannels["signal"]; ok {
		signalAdapter = signal.NewLocalAdapter(signal.Settings{
			CLIPath:          cfg.Channels.Signal.CLIPath,
			Account:          cfg.Channels.Signal.Account,
			DefaultRecipient: cfg.Channels.Signal.DefaultRecipient,
			Timeout:          time.Duration(cfg.Channels.Signal.TimeoutSeconds) * time.Second,
		})
	}

	app := &App{
		cfg:             cfg,
		enabledChannels: enabledChannels,
		tools:           skills.DefaultToolRegistry(),
		sessions: session.NewStore(session.Settings{
			StateRoot:     cfg.App.Root,
			StorePath:     cfg.Session.Store,
			KnownAgentIDs: agentIDs,
		}),
		workspace: workspaceManager,
		skills: skills.NewLocalRegistry(skills.LocalRegistrySettings{
			AgentIDs: agentIDs,
			ResolveWorkspace: func(agentID string) (string, error) {
				return workspaceManager.ResolveWorkspace(agentID)
			},
		}),
		heartbeat: heartbeat.NewLocalMonitorWithSettings(heartbeat.Settings{
			Enabled:         cfg.Heartbeat.Enabled,
			IntervalSeconds: cfg.Heartbeat.IntervalSeconds,
			Logf:            heartbeatLogf,
		}),
		slack:         slackAdapter,
		signal:        signalAdapter,
		signalReceive: signal.ReceiveBatch,
		llm:           llmClient,
		llmClients:    llmClients,
		// TODO: Wire transcript appends into memory autosync (StartAutoSync/HandleTranscriptUpdate) via a runtime event bus so session delta indexing is active at startup.
		transcript:          session.NewTranscriptWriter(session.TranscriptWriterSettings{}),
		now:                 time.Now,
		heartbeatLogf:       heartbeatLogf,
		skillPromptSnapshot: map[string]skillsSessionSnapshot{},
		providerModelsCache: map[string]llm.ProviderModelCatalog{},
	}
	app.cron = cron.NewInProcessSchedulerWithSettings(cron.Settings{
		Enabled:   cfg.Cron.Enabled,
		StateRoot: resolvedStateRoot,
		Executor:  app.runCronEntry,
		Logf:      cronLogf,
	})
	return app, nil
}

func resolveAbsolutePath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", errors.New("path is required")
	}
	if strings.HasPrefix(trimmed, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		trimmed = filepath.Join(home, strings.TrimPrefix(trimmed, "~"))
	}
	if filepath.IsAbs(trimmed) {
		return filepath.Clean(trimmed), nil
	}
	absPath, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}
	return filepath.Clean(absPath), nil
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
	if err := a.bootstrapDefaultConfigFile(); err != nil {
		return fmt.Errorf("bootstrap config: %w", err)
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
	a.heartbeat.Start(ctx, func(runCtx context.Context) error {
		return a.runHeartbeatTick(runCtx)
	})
	return nil
}

func (a *App) runHeartbeatTick(ctx context.Context) error {
	workspacePath, err := a.ResolveWorkspacePath(DefaultAgentID)
	if err != nil {
		return fmt.Errorf("resolve heartbeat workspace: %w", err)
	}
	heartbeatPath := filepath.Join(workspacePath, "HEARTBEAT.md")
	if _, err := os.ReadFile(heartbeatPath); err != nil {
		a.logHeartbeatf("heartbeat: skipped tick; unable to read %s: %v", heartbeatPath, err)
		return nil
	}
	if _, err := a.PromptForSession(ctx, DefaultAgentID, DefaultSessionID, buildHeartbeatPrompt(heartbeatPath)); err != nil {
		return fmt.Errorf("prompt heartbeat: %w", err)
	}
	return nil
}

func (a *App) logHeartbeatf(format string, args ...interface{}) {
	if a != nil && a.heartbeatLogf != nil {
		a.heartbeatLogf(format, args...)
		return
	}
	log.Printf(format, args...)
}

func buildHeartbeatPrompt(heartbeatPath string) string {
	trimmed := strings.TrimSpace(heartbeatPath)
	if trimmed == "" {
		return defaultHeartbeatPrompt
	}
	return fmt.Sprintf("%s\nUse workspace heartbeat file: %s", defaultHeartbeatPrompt, trimmed)
}

func (a *App) bootstrapDefaultConfigFile() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}

	configDir := filepath.Join(home, ".localclaw")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	path := filepath.Join(configDir, "localclaw.json")
	payload, err := json.MarshalIndent(a.cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	payload = append(payload, '\n')

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return fmt.Errorf("create default config file: %w", err)
	}
	defer file.Close()

	if _, err := file.Write(payload); err != nil {
		return fmt.Errorf("write default config file: %w", err)
	}
	return nil
}

// Prompt sends a single input to the configured local LLM client.
func (a *App) Prompt(ctx context.Context, input string) (string, error) {
	return a.PromptForSessionWithOptions(ctx, DefaultAgentID, DefaultSessionID, input, llm.PromptOptions{})
}

// PromptStream sends input to the local LLM client and yields incremental output events.
func (a *App) PromptStream(ctx context.Context, input string) (<-chan llm.StreamEvent, <-chan error) {
	return a.PromptStreamForSessionWithOptions(ctx, DefaultAgentID, DefaultSessionID, input, llm.PromptOptions{})
}

// PromptForSession sends a single input with the resolved agent/session context.
func (a *App) PromptForSession(ctx context.Context, agentID, sessionID, input string) (string, error) {
	return a.PromptForSessionWithOptions(ctx, agentID, sessionID, input, llm.PromptOptions{})
}

// PromptForSessionWithOptions sends a single input with session context and prompt options.
func (a *App) PromptForSessionWithOptions(ctx context.Context, agentID, sessionID, input string, opts llm.PromptOptions) (string, error) {
	events, errs := a.PromptStreamForSessionWithOptions(ctx, agentID, sessionID, input, opts)
	var streamed strings.Builder
	final := ""
	for events != nil || errs != nil {
		select {
		case evt, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			switch evt.Type {
			case llm.StreamEventDelta:
				streamed.WriteString(evt.Text)
			case llm.StreamEventFinal:
				final = strings.TrimSpace(evt.Text)
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err != nil {
				return "", err
			}
		}
	}
	if final != "" {
		return final, nil
	}
	return strings.TrimSpace(streamed.String()), nil
}

// PromptStreamForSession streams output with the resolved agent/session context.
func (a *App) PromptStreamForSession(ctx context.Context, agentID, sessionID, input string) (<-chan llm.StreamEvent, <-chan error) {
	return a.PromptStreamForSessionWithOptions(ctx, agentID, sessionID, input, llm.PromptOptions{})
}

// PromptStreamForSessionWithOptions streams output with the resolved agent/session context and prompt options.
func (a *App) PromptStreamForSessionWithOptions(ctx context.Context, agentID, sessionID, input string, opts llm.PromptOptions) (<-chan llm.StreamEvent, <-chan error) {
	resolution := ResolveSession(agentID, sessionID)
	req, err := a.buildPromptRequest(ctx, resolution, input, opts)
	if err != nil {
		events := make(chan llm.StreamEvent)
		errs := make(chan error, 1)
		close(events)
		errs <- err
		close(errs)
		return events, errs
	}
	return a.promptStreamWithSessionContinuation(ctx, resolution, req)
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
		a.clearProviderSessionContinuation(ctx, current)
		a.clearSkillPromptSnapshot(current.SessionKey)
		return current
	}
	a.clearSkillPromptSnapshot(current.SessionKey)
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
	// TODO: Replace truthy/positive heuristic detection with explicit optional override fields so agents can intentionally disable inherited defaults (for example enabled=false or threshold=0).
	return cfg.Enabled ||
		cfg.ThresholdTokens > 0 ||
		cfg.TriggerWindowTokens > 0 ||
		strings.TrimSpace(cfg.Prompt) != "" ||
		cfg.TimeoutSeconds > 0
}

func mergeMemoryFlushConfig(base, override config.MemoryFlushConfig) config.MemoryFlushConfig {
	merged := base
	// TODO: Support explicit false/zero overrides from agent config; current merge only applies enabling/positive values and cannot turn inherited settings off.
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

func (a *App) clearProviderSessionContinuation(ctx context.Context, resolution SessionResolution) {
	if a.sessions == nil {
		return
	}
	_, _ = a.sessions.Update(ctx, resolution.AgentID, resolution.SessionID, func(entry *session.SessionEntry) error {
		entry.ProviderSessionIDs = nil
		return nil
	})
}
