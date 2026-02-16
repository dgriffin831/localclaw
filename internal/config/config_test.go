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
		t.Fatalf("expected default config to validate, got error: %v", err)
	}
}

func TestDefaultConfigIncludesAppRootAndAgentScaffolding(t *testing.T) {
	cfg := Default()
	if strings.TrimSpace(cfg.App.Root) == "" {
		t.Fatalf("expected app.root default")
	}
	if strings.TrimSpace(cfg.Agents.Defaults.Workspace) == "" {
		t.Fatalf("expected agents.defaults.workspace default")
	}
	if strings.TrimSpace(cfg.Session.Store) == "" {
		t.Fatalf("expected session.store default")
	}
	if !strings.Contains(cfg.Session.Store, "{agentId}") {
		t.Fatalf("expected session.store to support {agentId} placeholder, got %q", cfg.Session.Store)
	}
	if strings.TrimSpace(cfg.Agents.Defaults.Memory.Store.Path) == "" {
		t.Fatalf("expected agents.defaults.memory.store.path default")
	}
	if !cfg.Agents.Defaults.Memory.Enabled {
		t.Fatalf("expected agents.defaults.memory.enabled default true")
	}
	if !cfg.Agents.Defaults.Memory.Tools.Get {
		t.Fatalf("expected agents.defaults.memory.tools.get default true")
	}
	if !cfg.Agents.Defaults.Memory.Tools.Search {
		t.Fatalf("expected agents.defaults.memory.tools.search default true")
	}
	if !cfg.Agents.Defaults.Memory.Tools.Grep {
		t.Fatalf("expected agents.defaults.memory.tools.grep default true")
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
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if len(cfg.App.ThinkingMessages) != 3 {
		t.Fatalf("expected 3 app.thinking_messages, got %d", len(cfg.App.ThinkingMessages))
	}
	if cfg.App.ThinkingMessages[0] != "thinking" {
		t.Fatalf("unexpected first thinking message %q", cfg.App.ThinkingMessages[0])
	}
}

func TestLoadUsesExplicitAgentDefaultsWorkspace(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")
	payload := `{
		"agents": {
			"defaults": {
				"workspace": "/tmp/new-workspace"
			}
		}
	}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Agents.Defaults.Workspace != "/tmp/new-workspace" {
		t.Fatalf("expected explicit agents.defaults.workspace to be preserved, got %q", cfg.Agents.Defaults.Workspace)
	}
}

func TestLoadDoesNotRebaseDerivedDefaultsWhenAppRootChanges(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")
	payload := `{
		"app": {"root": "/var/lib/localclaw"}
	}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	defaults := Default()
	if cfg.Session.Store != defaults.Session.Store {
		t.Fatalf("expected session.store default without rebasing, got %q", cfg.Session.Store)
	}
	if cfg.Agents.Defaults.Memory.Store.Path != defaults.Agents.Defaults.Memory.Store.Path {
		t.Fatalf("expected memory.store.path default without rebasing, got %q", cfg.Agents.Defaults.Memory.Store.Path)
	}
}

func TestLoadPreservesExplicitPathsWhenAppRootChanges(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")
	payload := `{
		"app": {"root": "/var/lib/localclaw"},
		"session": {"store": "/custom/sessions/{agentId}.json"},
		"agents": {
			"defaults": {
				"memory": {
					"store": {
						"path": "/custom/memory/{agentId}.sqlite"
					}
				}
			}
		}
	}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Session.Store != "/custom/sessions/{agentId}.json" {
		t.Fatalf("expected explicit session.store to be preserved, got %q", cfg.Session.Store)
	}
	if cfg.Agents.Defaults.Memory.Store.Path != "/custom/memory/{agentId}.sqlite" {
		t.Fatalf("expected explicit memory.store.path to be preserved, got %q", cfg.Agents.Defaults.Memory.Store.Path)
	}
}

func TestLoadSupportsMemoryFeatureFlags(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")
	payload := `{
		"agents": {
			"defaults": {
				"memory": {
					"enabled": false,
					"tools": {
						"get": false,
						"search": false,
						"grep": true
					}
				}
			}
		}
	}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Agents.Defaults.Memory.Enabled {
		t.Fatalf("expected agents.defaults.memory.enabled=false from config")
	}
	if cfg.Agents.Defaults.Memory.Tools.Get {
		t.Fatalf("expected agents.defaults.memory.tools.get=false from config")
	}
	if cfg.Agents.Defaults.Memory.Tools.Search {
		t.Fatalf("expected agents.defaults.memory.tools.search=false from config")
	}
	if !cfg.Agents.Defaults.Memory.Tools.Grep {
		t.Fatalf("expected agents.defaults.memory.tools.grep=true from config")
	}
}

func TestLoadSupportsMemorySearchSettingsUnderMemorySection(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")
	payload := `{
		"agents": {
			"defaults": {
				"memory": {
					"sources": ["memory"],
					"extraPaths": ["memory/incidents.md"],
					"store": {
						"path": "/custom/memory/{agentId}.sqlite"
					},
					"chunking": {
						"tokens": 420,
						"overlap": 42
					},
					"query": {
						"maxResults": 6,
						"minScore": 0.5
					},
					"sync": {
						"onSearch": true,
						"sessions": {
							"deltaBytes": 2048,
							"deltaMessages": 5
						}
					}
				}
			}
		}
	}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("expected memory search settings under agents.defaults.memory to load, got: %v", err)
	}
	if cfg.Agents.Defaults.Memory.Store.Path != "/custom/memory/{agentId}.sqlite" {
		t.Fatalf("expected memory.store.path override, got %q", cfg.Agents.Defaults.Memory.Store.Path)
	}
	if cfg.Agents.Defaults.Memory.Query.MaxResults != 6 {
		t.Fatalf("expected memory.query.maxResults=6, got %d", cfg.Agents.Defaults.Memory.Query.MaxResults)
	}
	if !cfg.Agents.Defaults.Memory.Sync.OnSearch {
		t.Fatalf("expected memory.sync.onSearch=true")
	}
}

func TestLoadSupportsMemorySearchExtraPaths(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")
	payload := `{
		"agents": {
			"defaults": {
				"memory": {
					"extraPaths": [
						"memory",
						"memory/incidents.md"
					]
				}
			}
		}
	}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(cfg.Agents.Defaults.Memory.ExtraPaths) != 2 {
		t.Fatalf("expected memory.extraPaths length 2, got %d", len(cfg.Agents.Defaults.Memory.ExtraPaths))
	}
	if cfg.Agents.Defaults.Memory.ExtraPaths[0] != "memory" {
		t.Fatalf("expected first memory.extraPaths entry to be memory, got %q", cfg.Agents.Defaults.Memory.ExtraPaths[0])
	}
}

func TestLoadRejectsLegacyMemorySearchSection(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")
	payload := `{
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
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatalf("expected legacy agents.defaults.memorySearch to be rejected")
	}
}

func TestLoadRejectsRemovedToolsAndSkillsConfig(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")
	payload := `{
		"tools": {"delegated": {"enabled": true}},
		"skills": {"enabled": ["writer"]}
	}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatalf("expected removed tools/skills config keys to be rejected")
	}
}

func TestLoadRejectsLegacyWorkspaceAndMemoryFields(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")
	payload := `{
		"workspace": {"root": "/tmp/legacy-workspace"},
		"memory": {"path": "/tmp/legacy-memory.json"}
	}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatalf("expected legacy workspace/memory fields to be rejected")
	}
}

func TestLoadRejectsLegacyStateRoot(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")
	payload := `{
		"state": {"root": "/var/lib/legacy-localclaw"}
	}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatalf("expected legacy state.root to be rejected")
	}
}

func TestLoadRejectsLegacySecurityFields(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")
	payload := `{
		"security": {
			"enforce_local_only": false,
			"enable_gateway": true,
			"enable_http_server": true,
			"listen_address": "127.0.0.1:8080"
		}
	}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatalf("expected legacy security fields to be rejected")
	}
}

func TestLoadRejectsLegacyGovCloudFields(t *testing.T) {
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
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatalf("expected legacy govcloud fields to be rejected")
	}
}

func TestValidateRejectsUnsupportedChannel(t *testing.T) {
	cfg := Default()
	cfg.Channels.Enabled = []string{"slack", "teams"}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected unsupported channel error")
	}
}

func TestValidateSupportsCodexProviderAndRequiresBinaryPath(t *testing.T) {
	cfg := Default()
	cfg.LLM.Provider = "codex"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected codex provider config to validate, got %v", err)
	}

	cfg = Default()
	cfg.LLM.Provider = "codex"
	cfg.LLM.Codex.BinaryPath = "   "
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected codex binary path validation error")
	}
}

func TestLoadSupportsCodexExtraArgs(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")
	payload := `{
		"llm": {
			"provider": "codex",
			"codex": {
				"extra_args": ["--sandbox", "workspace-write"]
			}
		}
	}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(cfg.LLM.Codex.ExtraArgs) != 2 {
		t.Fatalf("expected 2 codex extra args, got %d", len(cfg.LLM.Codex.ExtraArgs))
	}
	if cfg.LLM.Codex.ExtraArgs[0] != "--sandbox" || cfg.LLM.Codex.ExtraArgs[1] != "workspace-write" {
		t.Fatalf("unexpected codex extra args: %v", cfg.LLM.Codex.ExtraArgs)
	}
}

func TestValidateRejectsWhitespaceAgentWorkspaceOverride(t *testing.T) {
	cfg := Default()
	cfg.Agents.List = []AgentConfig{{ID: "agent-a", Workspace: "   "}}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected invalid agents.list[].workspace error")
	}
}

func TestValidateRejectsNegativeMemoryFlushValues(t *testing.T) {
	cfg := Default()
	cfg.Agents.Defaults.Compaction.MemoryFlush.ThresholdTokens = -1
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected negative thresholdTokens error")
	}

	cfg = Default()
	cfg.Agents.Defaults.Compaction.MemoryFlush.TriggerWindowTokens = -1
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected negative triggerWindowTokens error")
	}

	cfg = Default()
	cfg.Agents.Defaults.Compaction.MemoryFlush.TimeoutSeconds = -1
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected negative timeoutSeconds error")
	}
}

func TestValidateRejectsBlankThinkingMessages(t *testing.T) {
	cfg := Default()
	cfg.App.ThinkingMessages = []string{"thinking", "   "}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected blank thinking message error")
	}
}

func TestDefaultConfigIncludesProviderSessionContinuationDefaults(t *testing.T) {
	cfg := Default()
	if cfg.LLM.ClaudeCode.SessionMode != "always" {
		t.Fatalf("expected claude_code.session_mode=always, got %q", cfg.LLM.ClaudeCode.SessionMode)
	}
	if cfg.LLM.Codex.SessionMode != "existing" {
		t.Fatalf("expected codex.session_mode=existing, got %q", cfg.LLM.Codex.SessionMode)
	}
}

func TestValidateRejectsInvalidProviderSessionModes(t *testing.T) {
	cfg := Default()
	cfg.LLM.ClaudeCode.SessionMode = "invalid"
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected invalid claude_code.session_mode error")
	}

	cfg = Default()
	cfg.LLM.Codex.SessionMode = "bad"
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected invalid codex.session_mode error")
	}
}

func TestValidateRejectsResumeArgsWithoutSessionIDPlaceholderForExistingMode(t *testing.T) {
	cfg := Default()
	cfg.LLM.ClaudeCode.SessionMode = "existing"
	cfg.LLM.ClaudeCode.ResumeArgs = []string{"--resume", "fixed-id"}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected claude_code.resume_args placeholder validation error")
	}

	cfg = Default()
	cfg.LLM.Codex.SessionMode = "existing"
	cfg.LLM.Codex.ResumeArgs = []string{"resume", "fixed-id"}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected codex.resume_args placeholder validation error")
	}
}

func TestValidateRejectsInvalidCodexResumeOutputMode(t *testing.T) {
	cfg := Default()
	cfg.LLM.Codex.ResumeOutput = "yaml"
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected codex.resume_output validation error")
	}
}
