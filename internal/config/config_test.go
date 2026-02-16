package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigIsValid(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		fatalf(t, "expected default config to validate, got error: %v", err)
	}
}

func TestDefaultConfigIncludesStateAndAgentScaffolding(t *testing.T) {
	cfg := Default()
	if strings.TrimSpace(cfg.State.Root) == "" {
		fatalf(t, "expected state.root default")
	}
	if strings.TrimSpace(cfg.Agents.Defaults.Workspace) == "" {
		fatalf(t, "expected agents.defaults.workspace default")
	}
	if strings.TrimSpace(cfg.Session.Store) == "" {
		fatalf(t, "expected session.store default")
	}
	if !strings.Contains(cfg.Session.Store, "{agentId}") {
		fatalf(t, "expected session.store to support {agentId} placeholder, got %q", cfg.Session.Store)
	}
	if cfg.Agents.Defaults.Compaction.MemoryFlush.ThresholdTokens <= 0 {
		fatalf(t, "expected agents.defaults.compaction.memoryFlush.thresholdTokens default")
	}
	if !cfg.Agents.Defaults.Compaction.MemoryFlush.Enabled {
		fatalf(t, "expected agents.defaults.compaction.memoryFlush.enabled default")
	}
	if len(cfg.App.ThinkingMessages) != 0 {
		fatalf(t, "expected app.thinking_messages to default empty, got %v", cfg.App.ThinkingMessages)
	}
}

func TestLoadSupportsThinkingMessages(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")
	payload := `{
		"app": {
			"thinking_messages": ["thinking", "checking memory", "drafting response"]
		}
	}`
	if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
		fatalf(t, "write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		fatalf(t, "load config: %v", err)
	}

	if len(cfg.App.ThinkingMessages) != 3 {
		fatalf(t, "expected 3 app.thinking_messages, got %d", len(cfg.App.ThinkingMessages))
	}
	if cfg.App.ThinkingMessages[0] != "thinking" {
		fatalf(t, "unexpected first thinking message %q", cfg.App.ThinkingMessages[0])
	}
}

func TestLoadIgnoresLegacyWorkspaceAndMemoryFields(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")
	payload := `{
		"workspace": {"root": "/tmp/legacy-workspace"},
		"memory": {"path": "/tmp/legacy-memory.json"}
	}`
	if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
		fatalf(t, "write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		fatalf(t, "load config: %v", err)
	}
	if cfg.Agents.Defaults.Workspace != Default().Agents.Defaults.Workspace {
		fatalf(t, "expected legacy workspace to be ignored, got %q", cfg.Agents.Defaults.Workspace)
	}
}

func TestLoadUsesExplicitAgentDefaultsWorkspace(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")
	payload := `{
		"workspace": {"root": "/tmp/legacy-workspace"},
		"agents": {
			"defaults": {
				"workspace": "/tmp/new-workspace"
			}
		}
	}`
	if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
		fatalf(t, "write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		fatalf(t, "load config: %v", err)
	}
	if cfg.Agents.Defaults.Workspace != "/tmp/new-workspace" {
		fatalf(t, "expected explicit agents.defaults.workspace to be preserved, got %q", cfg.Agents.Defaults.Workspace)
	}
}

func TestLoadDoesNotRebaseDerivedDefaultsWhenStateRootChanges(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")
	payload := `{
		"state": {"root": "/var/lib/localclaw"}
	}`
	if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
		fatalf(t, "write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		fatalf(t, "load config: %v", err)
	}
	defaults := Default()
	if cfg.Session.Store != defaults.Session.Store {
		fatalf(t, "expected session.store default without rebasing, got %q", cfg.Session.Store)
	}
	if cfg.Agents.Defaults.MemorySearch.Store.Path != defaults.Agents.Defaults.MemorySearch.Store.Path {
		fatalf(t, "expected memorySearch.store.path default without rebasing, got %q", cfg.Agents.Defaults.MemorySearch.Store.Path)
	}
}

func TestLoadPreservesExplicitPathsWhenStateRootChanges(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")
	payload := `{
		"state": {"root": "/var/lib/localclaw"},
		"session": {"store": "/custom/sessions/{agentId}.json"},
		"agents": {
			"defaults": {
				"memorySearch": {
					"store": {
						"path": "/custom/memory/{agentId}.sqlite"
					}
				}
			}
		}
	}`
	if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
		fatalf(t, "write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		fatalf(t, "load config: %v", err)
	}
	if cfg.Session.Store != "/custom/sessions/{agentId}.json" {
		fatalf(t, "expected explicit session.store to be preserved, got %q", cfg.Session.Store)
	}
	if cfg.Agents.Defaults.MemorySearch.Store.Path != "/custom/memory/{agentId}.sqlite" {
		fatalf(t, "expected explicit memorySearch.store.path to be preserved, got %q", cfg.Agents.Defaults.MemorySearch.Store.Path)
	}
}

func TestValidateRejectsUnsupportedChannel(t *testing.T) {
	cfg := Default()
	cfg.Channels.Enabled = []string{"slack", "teams"}
	if err := cfg.Validate(); err == nil {
		fatalf(t, "expected unsupported channel error")
	}
}

func TestValidateRejectsNetworkServerFlags(t *testing.T) {
	cfg := Default()
	cfg.Security.EnableHTTPServer = true
	if err := cfg.Validate(); err == nil {
		fatalf(t, "expected local-only policy rejection")
	}
}

func TestLoadIgnoresLegacyGovCloudFields(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")
	payload := `{
		"llm": {
			"provider": "claudecode",
			"claude_code": {
				"binary_path": "claude",
				"profile": "default",
				"use_govcloud": true,
				"bedrock_region": ""
			}
		}
	}`
	if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
		fatalf(t, "write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		fatalf(t, "expected legacy govcloud fields to be ignored, got error: %v", err)
	}
	if cfg.LLM.Provider != "claudecode" {
		fatalf(t, "expected provider claudecode, got %q", cfg.LLM.Provider)
	}
}

func TestValidateSupportsCodexProviderAndRequiresBinaryPath(t *testing.T) {
	cfg := Default()
	cfg.LLM.Provider = "codex"
	if err := cfg.Validate(); err != nil {
		fatalf(t, "expected codex provider config to validate, got %v", err)
	}

	cfg = Default()
	cfg.LLM.Provider = "codex"
	cfg.LLM.Codex.BinaryPath = "   "
	if err := cfg.Validate(); err == nil {
		fatalf(t, "expected codex binary path validation error")
	}
}

func TestValidateRejectsWhitespaceAgentWorkspaceOverride(t *testing.T) {
	cfg := Default()
	cfg.Agents.List = []AgentConfig{
		{ID: "agent-a", Workspace: "   "},
	}
	if err := cfg.Validate(); err == nil {
		fatalf(t, "expected invalid agents.list[].workspace error")
	}
}

func TestValidateRejectsNegativeMemoryFlushValues(t *testing.T) {
	cfg := Default()
	cfg.Agents.Defaults.Compaction.MemoryFlush.ThresholdTokens = -1
	if err := cfg.Validate(); err == nil {
		fatalf(t, "expected negative thresholdTokens error")
	}

	cfg = Default()
	cfg.Agents.Defaults.Compaction.MemoryFlush.TriggerWindowTokens = -1
	if err := cfg.Validate(); err == nil {
		fatalf(t, "expected negative triggerWindowTokens error")
	}

	cfg = Default()
	cfg.Agents.Defaults.Compaction.MemoryFlush.TimeoutSeconds = -1
	if err := cfg.Validate(); err == nil {
		fatalf(t, "expected negative timeoutSeconds error")
	}
}

func TestLoadSupportsMemoryLocalEmbeddingSettings(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")
	payload := `{
		"agents": {
			"defaults": {
				"memorySearch": {
					"local": {
						"runtimePath": "/usr/local/bin/local-embed",
						"modelPath": "/models/embeddinggemma-300m",
						"modelCacheDir": "/var/cache/localclaw/models",
						"queryTimeoutSeconds": 7,
						"batchTimeoutSeconds": 29
					}
				}
			}
		}
	}`
	if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
		fatalf(t, "write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		fatalf(t, "load config: %v", err)
	}

	if cfg.Agents.Defaults.MemorySearch.Local.RuntimePath != "/usr/local/bin/local-embed" {
		fatalf(t, "expected memorySearch.local.runtimePath to load, got %q", cfg.Agents.Defaults.MemorySearch.Local.RuntimePath)
	}
	if cfg.Agents.Defaults.MemorySearch.Local.ModelPath != "/models/embeddinggemma-300m" {
		fatalf(t, "expected memorySearch.local.modelPath to load, got %q", cfg.Agents.Defaults.MemorySearch.Local.ModelPath)
	}
	if cfg.Agents.Defaults.MemorySearch.Local.ModelCacheDir != "/var/cache/localclaw/models" {
		fatalf(t, "expected memorySearch.local.modelCacheDir to load, got %q", cfg.Agents.Defaults.MemorySearch.Local.ModelCacheDir)
	}
	if cfg.Agents.Defaults.MemorySearch.Local.QueryTimeoutSeconds != 7 {
		fatalf(t, "expected memorySearch.local.queryTimeoutSeconds=7, got %d", cfg.Agents.Defaults.MemorySearch.Local.QueryTimeoutSeconds)
	}
	if cfg.Agents.Defaults.MemorySearch.Local.BatchTimeoutSeconds != 29 {
		fatalf(t, "expected memorySearch.local.batchTimeoutSeconds=29, got %d", cfg.Agents.Defaults.MemorySearch.Local.BatchTimeoutSeconds)
	}
}

func TestValidateRejectsBlankThinkingMessages(t *testing.T) {
	cfg := Default()
	cfg.App.ThinkingMessages = []string{"thinking", "   "}
	if err := cfg.Validate(); err == nil {
		fatalf(t, "expected blank thinking message error")
	}
}

func TestValidateRejectsInvalidLocalEmbeddingConfig(t *testing.T) {
	cfg := Default()
	cfg.Agents.Defaults.MemorySearch.Local.RuntimePath = "   "
	if err := cfg.Validate(); err == nil {
		fatalf(t, "expected blank local runtimePath validation error")
	}

	cfg = Default()
	cfg.Agents.Defaults.MemorySearch.Local.QueryTimeoutSeconds = -1
	if err := cfg.Validate(); err == nil {
		fatalf(t, "expected negative queryTimeoutSeconds validation error")
	}

	cfg = Default()
	cfg.Agents.Defaults.MemorySearch.Local.BatchTimeoutSeconds = -1
	if err := cfg.Validate(); err == nil {
		fatalf(t, "expected negative batchTimeoutSeconds validation error")
	}
}

func TestDefaultConfigDisablesDelegatedToolsByDefault(t *testing.T) {
	cfg := Default()
	if cfg.Tools.Delegated.Enabled {
		fatalf(t, "expected tools.delegated.enabled default false")
	}
	if len(cfg.Tools.Allow) != 0 {
		fatalf(t, "expected tools.allow default empty, got %v", cfg.Tools.Allow)
	}
	if len(cfg.Tools.Deny) != 0 {
		fatalf(t, "expected tools.deny default empty, got %v", cfg.Tools.Deny)
	}
}

func TestValidateRejectsBlankToolPolicyEntries(t *testing.T) {
	cfg := Default()
	cfg.Tools.Allow = []string{"memory_search", "   "}
	if err := cfg.Validate(); err == nil {
		fatalf(t, "expected tools.allow validation error")
	}

	cfg = Default()
	cfg.Agents.Defaults.Tools.Deny = []string{"memory_get", ""}
	if err := cfg.Validate(); err == nil {
		fatalf(t, "expected agents.defaults.tools.deny validation error")
	}

	cfg = Default()
	cfg.Agents.List = []AgentConfig{
		{
			ID: "writer",
			Tools: ToolsConfig{
				Delegated: DelegatedToolsConfig{
					Allow: []string{"remote_search", "  "},
				},
			},
		},
	}
	if err := cfg.Validate(); err == nil {
		fatalf(t, "expected agents.list[].tools.delegated.allow validation error")
	}
}

func fatalf(t *testing.T, format string, args ...interface{}) {
	t.Helper()
	t.Fatalf(format, args...)
}
