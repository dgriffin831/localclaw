package runtime

import (
	"testing"

	"github.com/dgriffin831/localclaw/internal/config"
)

func TestResolveMemoryConfigAgentOverrideCanDisableOnSearch(t *testing.T) {
	cfg := config.Default()
	cfg.Agents.Defaults.Memory.Sync.OnSearch = true
	overrideOnSearch := false
	cfg.Agents.List = []config.AgentConfig{
		{
			ID: "ops",
			Memory: config.MemoryOverrideConfig{
				Sync: config.SyncOverrideConfig{
					OnSearch: &overrideOnSearch,
				},
			},
		},
	}

	resolved := ResolveMemoryConfig(cfg, "ops")
	if resolved.Sync.OnSearch {
		t.Fatalf("expected resolved memory.sync.onSearch=false when agent override is false")
	}
}

func TestResolveMemoryConfigAgentOverrideCanEnableOnSearch(t *testing.T) {
	cfg := config.Default()
	cfg.Agents.Defaults.Memory.Sync.OnSearch = false
	overrideOnSearch := true
	cfg.Agents.List = []config.AgentConfig{
		{
			ID: "ops",
			Memory: config.MemoryOverrideConfig{
				Sync: config.SyncOverrideConfig{
					OnSearch: &overrideOnSearch,
				},
			},
		},
	}

	resolved := ResolveMemoryConfig(cfg, "ops")
	if !resolved.Sync.OnSearch {
		t.Fatalf("expected resolved memory.sync.onSearch=true when agent override is true")
	}
}
