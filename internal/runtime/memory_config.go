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
		cfg.Tools.Create != nil ||
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
		cfg.Vector.Enabled != nil ||
		strings.TrimSpace(cfg.Vector.Provider) != "" ||
		strings.TrimSpace(cfg.Vector.SearchMode) != "" ||
		strings.TrimSpace(cfg.Vector.Model.ID) != "" ||
		strings.TrimSpace(cfg.Vector.Model.Path) != "" ||
		strings.TrimSpace(cfg.Vector.Model.PrimaryURL) != "" ||
		strings.TrimSpace(cfg.Vector.Model.MirrorURL) != "" ||
		strings.TrimSpace(cfg.Vector.Model.SHA256) != "" ||
		cfg.Vector.Server.Managed != nil ||
		strings.TrimSpace(cfg.Vector.Server.Binary) != "" ||
		strings.TrimSpace(cfg.Vector.Server.Host) != "" ||
		cfg.Vector.Server.Port > 0 ||
		cfg.Vector.Server.StartupTimeoutSeconds > 0
}

func mergeMemoryConfig(base config.MemoryConfig, override config.MemoryOverrideConfig) config.MemoryConfig {
	merged := base
	if override.Enabled != nil {
		merged.Enabled = *override.Enabled
	}
	if override.Tools.Create != nil {
		merged.Tools.Create = *override.Tools.Create
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
	if override.Vector.Enabled != nil {
		merged.Vector.Enabled = *override.Vector.Enabled
	}
	if strings.TrimSpace(override.Vector.Provider) != "" {
		merged.Vector.Provider = override.Vector.Provider
	}
	if strings.TrimSpace(override.Vector.SearchMode) != "" {
		merged.Vector.SearchMode = override.Vector.SearchMode
	}
	if strings.TrimSpace(override.Vector.Model.ID) != "" {
		merged.Vector.Model.ID = override.Vector.Model.ID
	}
	if strings.TrimSpace(override.Vector.Model.Path) != "" {
		merged.Vector.Model.Path = override.Vector.Model.Path
	}
	if strings.TrimSpace(override.Vector.Model.PrimaryURL) != "" {
		merged.Vector.Model.PrimaryURL = override.Vector.Model.PrimaryURL
	}
	if strings.TrimSpace(override.Vector.Model.MirrorURL) != "" {
		merged.Vector.Model.MirrorURL = override.Vector.Model.MirrorURL
	}
	if strings.TrimSpace(override.Vector.Model.SHA256) != "" {
		merged.Vector.Model.SHA256 = override.Vector.Model.SHA256
	}
	if override.Vector.Server.Managed != nil {
		merged.Vector.Server.Managed = *override.Vector.Server.Managed
	}
	if strings.TrimSpace(override.Vector.Server.Binary) != "" {
		merged.Vector.Server.Binary = override.Vector.Server.Binary
	}
	if strings.TrimSpace(override.Vector.Server.Host) != "" {
		merged.Vector.Server.Host = override.Vector.Server.Host
	}
	if override.Vector.Server.Port > 0 {
		merged.Vector.Server.Port = override.Vector.Server.Port
	}
	if override.Vector.Server.StartupTimeoutSeconds > 0 {
		merged.Vector.Server.StartupTimeoutSeconds = override.Vector.Server.StartupTimeoutSeconds
	}
	return merged
}
