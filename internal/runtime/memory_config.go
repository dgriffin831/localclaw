package runtime

import (
	"strings"

	"github.com/dgriffin831/localclaw/internal/config"
)

// ResolveMemoryConfig returns the effective memory config for an agent by
// applying that agent's memory override over defaults.
func ResolveMemoryConfig(cfg config.Config, agentID string) config.MemoryConfig {
	resolved := cfg.Agents.Defaults.Memory
	normalizedAgentID := ResolveAgentID(agentID)
	for _, agent := range cfg.Agents.List {
		if ResolveAgentID(agent.ID) != normalizedAgentID {
			continue
		}
		if hasMemoryOverride(agent.Memory) {
			resolved = mergeMemoryConfig(resolved, agent.Memory)
		}
		break
	}
	return resolved
}

func hasMemoryOverride(cfg config.MemoryOverrideConfig) bool {
	return cfg.Enabled != nil ||
		cfg.Tools.Get != nil ||
		cfg.Tools.Search != nil ||
		cfg.Tools.Grep != nil ||
		len(cfg.Sources) > 0 ||
		len(cfg.ExtraPaths) > 0 ||
		strings.TrimSpace(cfg.Store.Path) != "" ||
		cfg.Chunking.Tokens > 0 ||
		cfg.Chunking.Overlap > 0 ||
		cfg.Query.MaxResults > 0 ||
		cfg.Sync.OnSearch != nil ||
		cfg.Sync.Sessions.DeltaBytes > 0 ||
		cfg.Sync.Sessions.DeltaMessages > 0
}

func mergeMemoryConfig(base config.MemoryConfig, override config.MemoryOverrideConfig) config.MemoryConfig {
	merged := base
	if override.Enabled != nil {
		merged.Enabled = *override.Enabled
	}
	if override.Tools.Get != nil {
		merged.Tools.Get = *override.Tools.Get
	}
	if override.Tools.Search != nil {
		merged.Tools.Search = *override.Tools.Search
	}
	if override.Tools.Grep != nil {
		merged.Tools.Grep = *override.Tools.Grep
	}
	if len(override.Sources) > 0 {
		merged.Sources = append([]string{}, override.Sources...)
	}
	if len(override.ExtraPaths) > 0 {
		merged.ExtraPaths = append([]string{}, override.ExtraPaths...)
	}
	if strings.TrimSpace(override.Store.Path) != "" {
		merged.Store.Path = override.Store.Path
	}
	if override.Chunking.Tokens > 0 {
		merged.Chunking.Tokens = override.Chunking.Tokens
	}
	if override.Chunking.Overlap > 0 {
		merged.Chunking.Overlap = override.Chunking.Overlap
	}
	if override.Query.MaxResults > 0 {
		merged.Query.MaxResults = override.Query.MaxResults
	}
	if override.Sync.OnSearch != nil {
		merged.Sync.OnSearch = *override.Sync.OnSearch
	}
	if override.Sync.Sessions.DeltaBytes > 0 {
		merged.Sync.Sessions.DeltaBytes = override.Sync.Sessions.DeltaBytes
	}
	if override.Sync.Sessions.DeltaMessages > 0 {
		merged.Sync.Sessions.DeltaMessages = override.Sync.Sessions.DeltaMessages
	}
	return merged
}
