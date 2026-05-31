package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/memory"
)

const (
	MemoryToolCreate = "localclaw_memory_create"
	MemoryToolSearch = "localclaw_memory_search"
	MemoryToolGet    = "localclaw_memory_get"
	MemoryToolGrep   = "localclaw_memory_grep"
)

func (a *App) newMemoryToolManager(ctx context.Context, agentID string, memoryCfg config.MemoryConfig) (*memory.SQLiteIndexManager, func(), error) {
	resolvedAgentID := ResolveAgentID(agentID)
	workspacePath, err := a.ResolveWorkspacePath(resolvedAgentID)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve workspace: %w", err)
	}

	storePath, err := resolveStorePath(a.cfg.App.Root, memoryCfg.Store.Path, resolvedAgentID)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve memory store path: %w", err)
	}

	sourceSet := normalizeSources(memoryCfg.Sources)
	if len(sourceSet) == 0 {
		sourceSet["memory"] = true
	}
	extraPaths := append([]string{}, memoryCfg.ExtraPaths...)
	if !sourceSet["memory"] {
		extraPaths = nil
	}

	manager := memory.NewSQLiteIndexManager(memory.IndexManagerConfig{
		DBPath:        storePath,
		WorkspaceRoot: workspacePath,
		Sources:       memoryCfg.Sources,
		ExtraPaths:    extraPaths,
		ChunkTokens:   memoryCfg.Chunking.Tokens,
		ChunkOverlap:  memoryCfg.Chunking.Overlap,
		EnableFTS:     true,
		Vector:        toMemoryVectorConfig(memoryCfg.Vector),
	})
	if err := manager.Open(ctx); err != nil {
		return nil, nil, fmt.Errorf("open memory index: %w", err)
	}

	return manager, func() { _ = manager.Close() }, nil
}

func toMemoryVectorConfig(cfg config.VectorConfig) memory.VectorConfig {
	return memory.VectorConfig{
		Enabled:    cfg.Enabled,
		Provider:   cfg.Provider,
		SearchMode: cfg.SearchMode,
		Model: memory.VectorModelConfig{
			ID:         cfg.Model.ID,
			Path:       cfg.Model.Path,
			PrimaryURL: cfg.Model.PrimaryURL,
			MirrorURL:  cfg.Model.MirrorURL,
			SHA256:     cfg.Model.SHA256,
		},
		Server: memory.VectorServerConfig{
			Managed:               cfg.Server.Managed,
			Binary:                cfg.Server.Binary,
			Host:                  cfg.Server.Host,
			Port:                  cfg.Server.Port,
			StartupTimeoutSeconds: cfg.Server.StartupTimeoutSeconds,
		},
	}
}

func (a *App) memoryToolEnabled(agentID, toolName string) (bool, string) {
	name := strings.ToLower(strings.TrimSpace(toolName))
	if name == "" {
		return false, "tool name is required"
	}

	resolvedAgentID := ResolveAgentID(agentID)
	memoryCfg := ResolveMemoryConfig(a.cfg, resolvedAgentID)
	if !memoryCfg.Enabled {
		return false, fmt.Sprintf("memory tools are disabled for agent %q", resolvedAgentID)
	}

	switch name {
	case MemoryToolCreate:
		if !memoryCfg.Tools.Create {
			return false, fmt.Sprintf("memory_create is disabled for agent %q", resolvedAgentID)
		}
	case MemoryToolSearch:
		if !memoryCfg.Tools.Search {
			return false, fmt.Sprintf("memory_search is disabled for agent %q", resolvedAgentID)
		}
	case MemoryToolGet:
		if !memoryCfg.Tools.Get {
			return false, fmt.Sprintf("memory_get is disabled for agent %q", resolvedAgentID)
		}
	case MemoryToolGrep:
		if !memoryCfg.Tools.Grep {
			return false, fmt.Sprintf("memory_grep is disabled for agent %q", resolvedAgentID)
		}
	}

	return true, ""
}

func resolveStorePath(stateRoot string, storePattern string, agentID string) (string, error) {
	pattern := strings.TrimSpace(storePattern)
	if pattern == "" {
		return "", errors.New("memory.store.path is required")
	}
	pattern = strings.ReplaceAll(pattern, "{agentId}", agentID)
	if pattern == "~" || strings.HasPrefix(pattern, "~/") || filepath.IsAbs(pattern) {
		resolved, err := resolvePath(pattern)
		if err != nil {
			return "", err
		}
		return filepath.Clean(resolved), nil
	}
	root, err := resolvePath(stateRoot)
	if err != nil {
		return "", err
	}
	return filepath.Clean(filepath.Join(root, pattern)), nil
}

func normalizeSources(values []string) map[string]bool {
	out := map[string]bool{}
	for _, raw := range values {
		source := strings.ToLower(strings.TrimSpace(raw))
		if source == "" {
			continue
		}
		out[source] = true
	}
	return out
}

func ensureParentDir(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o755)
}
