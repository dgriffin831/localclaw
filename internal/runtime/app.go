package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/workspace"
)

// App composes the local workspace and memory capabilities exposed by MCP.
type App struct {
	cfg       config.Config
	workspace workspace.Manager
}

const DefaultAgentID = "default"

func ResolveAgentID(agentID string) string {
	trimmed := strings.TrimSpace(agentID)
	if trimmed == "" {
		return DefaultAgentID
	}
	return trimmed
}

func New(cfg config.Config) (*App, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	agentWorkspaces := make(map[string]string, len(cfg.Agents.List))
	for _, agent := range cfg.Agents.List {
		agentWorkspaces[agent.ID] = agent.Workspace
	}

	return &App{
		cfg: cfg,
		workspace: workspace.NewLocalManager(workspace.Settings{
			StateRoot:        cfg.App.Root,
			DefaultWorkspace: cfg.Agents.Defaults.Workspace,
			AgentWorkspaces:  agentWorkspaces,
		}),
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	if err := a.workspace.Init(ctx); err != nil {
		return fmt.Errorf("workspace init: %w", err)
	}
	if err := a.bootstrapDefaultConfigFile(); err != nil {
		return fmt.Errorf("bootstrap config: %w", err)
	}
	return nil
}

func (a *App) Config() config.Config {
	return a.cfg
}

func (a *App) ResolveWorkspacePath(agentID string) (string, error) {
	return a.workspace.ResolveWorkspace(ResolveAgentID(agentID))
}

func (a *App) EnsureWorkspace(ctx context.Context, agentID string) (workspace.WorkspaceInfo, error) {
	return a.workspace.EnsureWorkspace(ctx, ResolveAgentID(agentID), true)
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

func ResolveStatePath(path string) (string, error) {
	return resolvePath(path)
}

func resolvePath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", errors.New("path is required")
	}
	if trimmed == "~" || strings.HasPrefix(trimmed, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		if trimmed == "~" {
			return filepath.Clean(home), nil
		}
		return filepath.Clean(filepath.Join(home, strings.TrimPrefix(trimmed, "~/"))), nil
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
