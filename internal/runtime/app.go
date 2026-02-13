package runtime

import (
	"context"
	"fmt"

	"github.com/dgriffin831/localclaw/internal/channels/signal"
	"github.com/dgriffin831/localclaw/internal/channels/slack"
	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/cron"
	"github.com/dgriffin831/localclaw/internal/heartbeat"
	"github.com/dgriffin831/localclaw/internal/llm/claudecode"
	"github.com/dgriffin831/localclaw/internal/memory"
	"github.com/dgriffin831/localclaw/internal/skills"
	"github.com/dgriffin831/localclaw/internal/workspace"
)

// App composes all localclaw capabilities in a single process.
type App struct {
	cfg       config.Config
	memory    memory.Store
	workspace workspace.Manager
	skills    skills.Registry
	cron      cron.Scheduler
	heartbeat heartbeat.Monitor
	slack     slack.Client
	signal    signal.Client
	llm       claudecode.Client
}

func New(cfg config.Config) (*App, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &App{
		cfg:       cfg,
		memory:    memory.NewLocalStore(cfg.Memory.Path),
		workspace: workspace.NewLocalManager(cfg.Workspace.Root),
		skills:    skills.NewLocalRegistry(),
		cron:      cron.NewInProcessScheduler(cfg.Cron.Enabled),
		heartbeat: heartbeat.NewLocalMonitor(cfg.Heartbeat.Enabled, cfg.Heartbeat.IntervalSeconds),
		slack:     slack.NewLocalAdapter(),
		signal:    signal.NewLocalAdapter(),
		llm:       claudecode.NewClient(cfg.LLM.ClaudeCode.BinaryPath),
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	if err := a.workspace.Init(ctx); err != nil {
		return fmt.Errorf("workspace init: %w", err)
	}
	if err := a.memory.Init(ctx); err != nil {
		return fmt.Errorf("memory init: %w", err)
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
