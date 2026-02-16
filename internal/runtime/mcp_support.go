package runtime

import (
	"context"
	"fmt"

	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/memory"
)

func (a *App) MCPToolsConfig(agentID string) config.ToolsConfig {
	return a.resolveToolsConfig(agentID)
}

func (a *App) MCPMemorySearch(ctx context.Context, agentID, sessionID, query string, opts memory.SearchOptions) ([]memory.SearchResult, error) {
	resolvedAgentID := ResolveAgentID(agentID)
	resolvedSession := ResolveSession(resolvedAgentID, sessionID)
	searchCfg := a.resolveMemorySearchConfig(resolvedAgentID)
	if !searchCfg.Enabled {
		return nil, fmt.Errorf("memory tools are disabled for agent %q", resolvedAgentID)
	}
	if opts.SessionKey == "" {
		opts.SessionKey = resolvedSession.SessionKey
	}
	if opts.MaxResults <= 0 {
		opts.MaxResults = searchCfg.Query.MaxResults
	}

	manager, cleanup, err := a.newMemoryToolManager(ctx, resolvedAgentID, searchCfg)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	if searchCfg.Sync.OnSearch {
		if _, err := manager.Sync(ctx, false); err != nil {
			return nil, fmt.Errorf("memory_search sync failed: %w", err)
		}
	}
	return manager.Search(ctx, query, opts)
}

func (a *App) MCPMemoryGet(ctx context.Context, agentID, _ string, path string, opts memory.GetOptions) (memory.GetResult, error) {
	resolvedAgentID := ResolveAgentID(agentID)
	searchCfg := a.resolveMemorySearchConfig(resolvedAgentID)
	if !searchCfg.Enabled {
		return memory.GetResult{}, fmt.Errorf("memory tools are disabled for agent %q", resolvedAgentID)
	}

	manager, cleanup, err := a.newMemoryToolManager(ctx, resolvedAgentID, searchCfg)
	if err != nil {
		return memory.GetResult{}, err
	}
	defer cleanup()

	return manager.Get(ctx, path, opts)
}
