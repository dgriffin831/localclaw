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

func TestDefaultConfigIncludesCodexReasoningDefault(t *testing.T) {
	cfg := Default()
	if strings.TrimSpace(cfg.LLM.Codex.ReasoningDefault) == "" {
		t.Fatalf("expected llm.codex.reasoning_default default")
	}
}

func TestDefaultConfigIncludesSecurityMode(t *testing.T) {
	cfg := Default()
	if cfg.Security.Mode != "sandbox-write" {
		t.Fatalf("expected default security.mode=sandbox-write, got %q", cfg.Security.Mode)
	}
}

func TestDefaultConfigIncludesBackupDefaults(t *testing.T) {
	cfg := Default()
	if !cfg.Backup.AutoSave {
		t.Fatalf("expected backup.auto_save default true")
	}
	if !cfg.Backup.AutoClean {
		t.Fatalf("expected backup.auto_clean default true")
	}
	if cfg.Backup.Interval != "1d" {
		t.Fatalf("expected backup.interval default 1d, got %q", cfg.Backup.Interval)
	}
	if cfg.Backup.RetainCount != 3 {
		t.Fatalf("expected backup.retain_count default 3, got %d", cfg.Backup.RetainCount)
	}
}

func TestValidateRejectsUnsupportedCodexReasoningDefault(t *testing.T) {
	cfg := Default()
	cfg.LLM.Provider = "codex"
	cfg.LLM.Codex.ReasoningDefault = "ultra"
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected invalid llm.codex.reasoning_default to fail validation")
	}
}

func TestDefaultConfigIncludesAppRootAndAgentScaffolding(t *testing.T) {
	cfg := Default()
	if strings.TrimSpace(cfg.App.Root) == "" {
		t.Fatalf("expected app.root default")
	}
	if cfg.App.Default.Verbose {
		t.Fatalf("expected app.default.verbose default false")
	}
	if cfg.App.Default.Mouse {
		t.Fatalf("expected app.default.mouse default false")
	}
	if cfg.App.Default.Tools {
		t.Fatalf("expected app.default.tools default false")
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

func TestLoadSupportsAppDefaultFlags(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")
	payload := `{
		"app": {
			"default": {
				"verbose": true,
				"mouse": false,
				"tools": true
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
	if !cfg.App.Default.Verbose {
		t.Fatalf("expected app.default.verbose=true from config")
	}
	if cfg.App.Default.Mouse {
		t.Fatalf("expected app.default.mouse=false from config")
	}
	if !cfg.App.Default.Tools {
		t.Fatalf("expected app.default.tools=true from config")
	}
}

func TestLoadRejectsRemovedAppDefaultThinkingToggle(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")
	payload := `{
		"app": {
			"default": {
				"thinking": false
			}
		}
	}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatalf("expected app.default.thinking to be rejected")
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
						"maxResults": 6
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

func TestLoadSupportsSecurityModes(t *testing.T) {
	tests := []struct {
		name string
		mode string
	}{
		{name: "full-access", mode: "full-access"},
		{name: "sandbox-write", mode: "sandbox-write"},
		{name: "read-only", mode: "read-only"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			path := filepath.Join(tmpDir, "config.json")
			payload := `{
				"security": {
					"mode": "` + tt.mode + `"
				}
			}`
			if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}

			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("load config: %v", err)
			}
			if cfg.Security.Mode != tt.mode {
				t.Fatalf("expected security.mode=%q, got %q", tt.mode, cfg.Security.Mode)
			}
		})
	}
}

func TestValidateRejectsUnsupportedSecurityMode(t *testing.T) {
	cfg := Default()
	cfg.Security.Mode = "wild-west"
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected invalid security.mode to fail validation")
	}
}

func TestValidateRejectsBlankBackupInterval(t *testing.T) {
	cfg := Default()
	cfg.Backup.Interval = "   "
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected blank backup.interval to fail validation")
	}
}

func TestValidateRejectsInvalidBackupInterval(t *testing.T) {
	cfg := Default()
	cfg.Backup.Interval = "1fortnight"
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected invalid backup.interval to fail validation")
	}
}

func TestValidateRejectsNonPositiveBackupRetainCount(t *testing.T) {
	cfg := Default()
	cfg.Backup.RetainCount = 0
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected backup.retain_count <= 0 to fail validation")
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

func TestLoadRejectsRemovedCodexMCPIsolatedHomeFields(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")
	payload := `{
		"llm": {
			"provider": "codex",
			"codex": {
				"mcp": {
					"use_isolated_home": true,
					"home_path": "~/.localclaw/runtime/codex/home"
				}
			}
		}
	}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatalf("expected removed codex mcp isolated-home fields to be rejected")
	}
}

func TestLoadRejectsRemovedCodexSessionArg(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")
	payload := `{
		"llm": {
			"codex": {
				"session_arg": "--session-id"
			}
		}
	}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatalf("expected removed llm.codex.session_arg to be rejected")
	}
}

func TestLoadRejectsRemovedMemoryQueryMinScore(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")
	payload := `{
		"agents": {
			"defaults": {
				"memory": {
					"query": {
						"minScore": 0.5
					}
				}
			}
		}
	}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatalf("expected removed agents.defaults.memory.query.minScore to be rejected")
	}
}

func TestValidateAllowsNoEnabledChannels(t *testing.T) {
	cfg := Default()
	cfg.Channels.Enabled = []string{}
	cfg.Channels.Slack.BotTokenEnv = ""
	cfg.Channels.Slack.APIBaseURL = ""
	cfg.Channels.Slack.TimeoutSeconds = 0
	cfg.Channels.Signal.CLIPath = ""
	cfg.Channels.Signal.Account = ""
	cfg.Channels.Signal.TimeoutSeconds = 0
	cfg.Channels.Signal.Inbound.Enabled = true
	cfg.Channels.Signal.Inbound.AllowFrom = nil

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected no enabled channels to be valid, got %v", err)
	}
}

func TestValidateRejectsUnsupportedChannel(t *testing.T) {
	cfg := Default()
	cfg.Channels.Enabled = []string{"slack", "teams"}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected unsupported channel error")
	}
}

func TestValidateRejectsMissingEnabledSlackConfig(t *testing.T) {
	cfg := Default()
	cfg.Channels.Enabled = []string{"slack"}
	cfg.Channels.Slack.BotTokenEnv = "   "
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected missing slack bot token env validation error")
	}
}

func TestValidateRejectsMissingEnabledSignalConfig(t *testing.T) {
	cfg := Default()
	cfg.Channels.Enabled = []string{"signal"}
	cfg.Channels.Signal.Account = ""
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected missing signal account validation error")
	}
}

func TestValidateAllowsBlankDisabledChannelConfig(t *testing.T) {
	cfg := Default()
	cfg.Channels.Enabled = []string{"slack"}
	cfg.Channels.Signal.Account = ""
	cfg.Channels.Signal.CLIPath = ""
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected disabled signal config to be ignored, got %v", err)
	}
}

func TestValidateRejectsSignalInboundWithoutAllowlist(t *testing.T) {
	cfg := Default()
	cfg.Channels.Enabled = []string{"signal"}
	cfg.Channels.Signal.Inbound.Enabled = true
	cfg.Channels.Signal.Inbound.AllowFrom = nil
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected signal inbound allowlist validation error")
	}
}

func TestValidateRejectsSignalInboundAgentMappingToUnknownAgent(t *testing.T) {
	cfg := Default()
	cfg.Channels.Enabled = []string{"signal"}
	cfg.Channels.Signal.Inbound.Enabled = true
	cfg.Channels.Signal.Inbound.AllowFrom = []string{"+15557654321"}
	cfg.Channels.Signal.Inbound.AgentBySender = map[string]string{
		"+15557654321": "agent-missing",
	}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected unknown agent mapping validation error")
	}
}

func TestValidateAllowsSignalInboundWithAllowlistAndAgentMapping(t *testing.T) {
	cfg := Default()
	cfg.Channels.Enabled = []string{"signal"}
	cfg.Agents.List = []AgentConfig{
		{ID: "agent-ops"},
	}
	cfg.Channels.Signal.Inbound.Enabled = true
	cfg.Channels.Signal.Inbound.AllowFrom = []string{"+15557654321"}
	cfg.Channels.Signal.Inbound.AgentBySender = map[string]string{
		"+15557654321": "agent-ops",
	}
	cfg.Channels.Signal.Inbound.DefaultAgent = "default"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid inbound signal config, got %v", err)
	}
}

func TestValidateRejectsSignalInboundNonPositiveTypingInterval(t *testing.T) {
	cfg := Default()
	cfg.Channels.Enabled = []string{"signal"}
	cfg.Channels.Signal.Inbound.Enabled = true
	cfg.Channels.Signal.Inbound.AllowFrom = []string{"+15557654321"}
	cfg.Channels.Signal.Inbound.TypingIntervalSeconds = 0
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected typing interval validation error")
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
				"extra_args": ["--skip-git-repo-check", "--no-color"]
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
	if cfg.LLM.Codex.ExtraArgs[0] != "--skip-git-repo-check" || cfg.LLM.Codex.ExtraArgs[1] != "--no-color" {
		t.Fatalf("unexpected codex extra args: %v", cfg.LLM.Codex.ExtraArgs)
	}
}

func TestLoadSupportsClaudeCodeExtraArgs(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")
	payload := `{
		"llm": {
			"provider": "claudecode",
			"claude_code": {
				"extra_args": ["--allowed-tools", "mcp__localclaw__localclaw_memory_search"]
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
	if len(cfg.LLM.ClaudeCode.ExtraArgs) != 2 {
		t.Fatalf("expected 2 claude_code extra args, got %d", len(cfg.LLM.ClaudeCode.ExtraArgs))
	}
	if cfg.LLM.ClaudeCode.ExtraArgs[0] != "--allowed-tools" {
		t.Fatalf("unexpected first claude_code extra arg: %q", cfg.LLM.ClaudeCode.ExtraArgs[0])
	}
}

func TestValidateRejectsCodexSecurityFlagsInExtraArgs(t *testing.T) {
	cfg := Default()
	cfg.LLM.Codex.ExtraArgs = []string{"--skip-git-repo-check", "--sandbox", "workspace-write"}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected codex security flag conflict to fail validation")
	}

	cfg = Default()
	cfg.LLM.Codex.ExtraArgs = []string{"--yolo"}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected codex --yolo conflict to fail validation")
	}

	cfg = Default()
	cfg.LLM.Codex.ExtraArgs = []string{"--add-dir", "/tmp/workspace"}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected codex --add-dir conflict to fail validation")
	}
}

func TestValidateRejectsClaudeSecurityFlagsInExtraArgs(t *testing.T) {
	cfg := Default()
	cfg.LLM.ClaudeCode.ExtraArgs = []string{"--dangerously-skip-permissions"}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected claude security flag conflict to fail validation")
	}

	cfg = Default()
	cfg.LLM.ClaudeCode.ExtraArgs = []string{"--permission-mode", "plan"}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected claude --permission-mode conflict to fail validation")
	}

	cfg = Default()
	cfg.LLM.ClaudeCode.ExtraArgs = []string{"--add-dir", "/tmp/workspace"}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected claude --add-dir conflict to fail validation")
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

func TestDefaultConfigIncludesCodexSkipGitRepoCheckArg(t *testing.T) {
	cfg := Default()
	if len(cfg.LLM.Codex.ExtraArgs) == 0 {
		t.Fatalf("expected default codex extra args to include --skip-git-repo-check")
	}
	if cfg.LLM.Codex.ExtraArgs[0] != "--skip-git-repo-check" {
		t.Fatalf("expected default codex extra args to start with --skip-git-repo-check, got %q", cfg.LLM.Codex.ExtraArgs[0])
	}
}

func TestDefaultConfigUsesJSONResumeOutputForCodex(t *testing.T) {
	cfg := Default()
	if cfg.LLM.Codex.ResumeOutput != "json" {
		t.Fatalf("expected default codex.resume_output=json, got %q", cfg.LLM.Codex.ResumeOutput)
	}
}

func TestDefaultConfigIncludesClaudeAllowedMCPTools(t *testing.T) {
	cfg := Default()
	if len(cfg.LLM.ClaudeCode.ExtraArgs) != 2 {
		t.Fatalf("expected 2 default claude_code extra args, got %d (%v)", len(cfg.LLM.ClaudeCode.ExtraArgs), cfg.LLM.ClaudeCode.ExtraArgs)
	}
	if cfg.LLM.ClaudeCode.ExtraArgs[0] != "--allowed-tools" {
		t.Fatalf("expected default claude_code extra args to start with --allowed-tools, got %q", cfg.LLM.ClaudeCode.ExtraArgs[0])
	}
	allowed := cfg.LLM.ClaudeCode.ExtraArgs[1]
	required := []string{
		"mcp__localclaw__localclaw_memory_search",
		"mcp__localclaw__localclaw_memory_get",
		"mcp__localclaw__localclaw_memory_grep",
		"mcp__localclaw__localclaw_workspace_status",
		"mcp__localclaw__localclaw_cron_list",
		"mcp__localclaw__localclaw_cron_add",
		"mcp__localclaw__localclaw_cron_remove",
		"mcp__localclaw__localclaw_cron_run",
		"mcp__localclaw__localclaw_sessions_list",
		"mcp__localclaw__localclaw_sessions_history",
		"mcp__localclaw__localclaw_sessions_delete",
		"mcp__localclaw__localclaw_session_status",
		"mcp__localclaw__localclaw_slack_send",
		"mcp__localclaw__localclaw_signal_send",
	}
	for _, tool := range required {
		if !strings.Contains(allowed, tool) {
			t.Fatalf("expected default allowed-tools to include %q, got %q", tool, allowed)
		}
	}
	notExpected := []string{
		"mcp__localclaw__localclaw_workspace_bootstrap_context",
		"mcp__localclaw__localclaw_sessions_send",
	}
	for _, tool := range notExpected {
		if strings.Contains(allowed, tool) {
			t.Fatalf("expected default allowed-tools to exclude %q, got %q", tool, allowed)
		}
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
