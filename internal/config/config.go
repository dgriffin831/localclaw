package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var allowedChannels = []string{"slack", "signal"}

// Config contains all runtime configuration for localclaw.
type Config struct {
	App       AppConfig       `json:"app"`
	Security  SecurityConfig  `json:"security"`
	LLM       LLMConfig       `json:"llm"`
	Channels  ChannelsConfig  `json:"channels"`
	State     StateConfig     `json:"state"`
	Agents    AgentsConfig    `json:"agents"`
	Session   SessionConfig   `json:"session"`
	Tools     ToolsConfig     `json:"tools"`
	Skills    SkillsConfig    `json:"skills"`
	Cron      CronConfig      `json:"cron"`
	Heartbeat HeartbeatConfig `json:"heartbeat"`
}

type AppConfig struct {
	Name             string   `json:"name"`
	ThinkingMessages []string `json:"thinking_messages,omitempty"`
}

type SecurityConfig struct {
	EnforceLocalOnly bool   `json:"enforce_local_only"`
	EnableGateway    bool   `json:"enable_gateway"`
	EnableHTTPServer bool   `json:"enable_http_server"`
	ListenAddress    string `json:"listen_address"`
}

type LLMConfig struct {
	Provider   string           `json:"provider"`
	ClaudeCode ClaudeCodeConfig `json:"claude_code"`
	Codex      CodexConfig      `json:"codex"`
}

type ClaudeCodeConfig struct {
	BinaryPath string `json:"binary_path"`
	Profile    string `json:"profile"`
}

type CodexConfig struct {
	BinaryPath string         `json:"binary_path"`
	Profile    string         `json:"profile"`
	Model      string         `json:"model"`
	MCP        CodexMCPConfig `json:"mcp"`
}

type CodexMCPConfig struct {
	ConfigPath      string `json:"config_path"`
	UseIsolatedHome bool   `json:"use_isolated_home"`
	HomePath        string `json:"home_path"`
	ServerName      string `json:"server_name"`
}

type ChannelsConfig struct {
	Enabled []string `json:"enabled"`
}

type StateConfig struct {
	Root string `json:"root"`
}

type AgentsConfig struct {
	Defaults AgentDefaultsConfig `json:"defaults"`
	List     []AgentConfig       `json:"list"`
}

type AgentDefaultsConfig struct {
	Workspace    string             `json:"workspace"`
	MemorySearch MemorySearchConfig `json:"memorySearch"`
	Compaction   CompactionConfig   `json:"compaction"`
	Tools        ToolsConfig        `json:"tools"`
	Skills       SkillsConfig       `json:"skills"`
}

type AgentConfig struct {
	ID           string             `json:"id"`
	Workspace    string             `json:"workspace,omitempty"`
	MemorySearch MemorySearchConfig `json:"memorySearch,omitempty"`
	Compaction   CompactionConfig   `json:"compaction,omitempty"`
	Tools        ToolsConfig        `json:"tools,omitempty"`
	Skills       SkillsConfig       `json:"skills,omitempty"`
}

type CompactionConfig struct {
	MemoryFlush MemoryFlushConfig `json:"memoryFlush"`
}

type MemoryFlushConfig struct {
	Enabled             bool   `json:"enabled"`
	ThresholdTokens     int    `json:"thresholdTokens"`
	TriggerWindowTokens int    `json:"triggerWindowTokens"`
	Prompt              string `json:"prompt"`
	TimeoutSeconds      int    `json:"timeoutSeconds"`
}

type SessionConfig struct {
	Store string `json:"store"`
}

type ToolsConfig struct {
	Allow     []string             `json:"allow,omitempty"`
	Deny      []string             `json:"deny,omitempty"`
	Delegated DelegatedToolsConfig `json:"delegated"`
}

type DelegatedToolsConfig struct {
	Enabled bool     `json:"enabled"`
	Allow   []string `json:"allow,omitempty"`
	Deny    []string `json:"deny,omitempty"`
}

type SkillsConfig struct {
	Enabled  []string `json:"enabled,omitempty"`
	Disabled []string `json:"disabled,omitempty"`
}

type MemorySearchConfig struct {
	Enabled    bool                    `json:"enabled"`
	Sources    []string                `json:"sources"`
	ExtraPaths []string                `json:"extraPaths"`
	Provider   string                  `json:"provider"`
	Fallback   string                  `json:"fallback"`
	Model      string                  `json:"model"`
	Store      MemorySearchStoreConfig `json:"store"`
	Chunking   ChunkingConfig          `json:"chunking"`
	Query      QueryConfig             `json:"query"`
	Sync       SyncConfig              `json:"sync"`
	Cache      CacheConfig             `json:"cache"`
	Local      LocalConfig             `json:"local"`
}

type MemorySearchStoreConfig struct {
	Path   string                 `json:"path"`
	Vector MemorySearchVectorMode `json:"vector"`
}

type MemorySearchVectorMode struct {
	Enabled bool `json:"enabled"`
}

type ChunkingConfig struct {
	Tokens  int `json:"tokens"`
	Overlap int `json:"overlap"`
}

type QueryConfig struct {
	MaxResults int               `json:"maxResults"`
	MinScore   float64           `json:"minScore"`
	Hybrid     QueryHybridConfig `json:"hybrid"`
}

type QueryHybridConfig struct {
	Enabled             bool    `json:"enabled"`
	VectorWeight        float64 `json:"vectorWeight"`
	KeywordWeight       float64 `json:"keywordWeight"`
	CandidateMultiplier int     `json:"candidateMultiplier"`
}

type SyncConfig struct {
	OnSearch bool               `json:"onSearch"`
	Sessions SyncSessionsConfig `json:"sessions"`
}

type SyncSessionsConfig struct {
	DeltaBytes    int `json:"deltaBytes"`
	DeltaMessages int `json:"deltaMessages"`
}

type CacheConfig struct {
	Enabled    bool `json:"enabled"`
	MaxEntries int  `json:"maxEntries"`
}

type LocalConfig struct {
	RuntimePath         string `json:"runtimePath"`
	ModelPath           string `json:"modelPath"`
	ModelCacheDir       string `json:"modelCacheDir"`
	QueryTimeoutSeconds int    `json:"queryTimeoutSeconds"`
	BatchTimeoutSeconds int    `json:"batchTimeoutSeconds"`
}

type CronConfig struct {
	Enabled bool `json:"enabled"`
}

type HeartbeatConfig struct {
	Enabled         bool `json:"enabled"`
	IntervalSeconds int  `json:"interval_seconds"`
}

func Default() Config {
	return Config{
		App: AppConfig{Name: "localclaw"},
		Security: SecurityConfig{
			EnforceLocalOnly: true,
			EnableGateway:    false,
			EnableHTTPServer: false,
			ListenAddress:    "",
		},
		LLM: LLMConfig{
			Provider: "claudecode",
			ClaudeCode: ClaudeCodeConfig{
				BinaryPath: "claude",
				Profile:    "default",
			},
			Codex: CodexConfig{
				BinaryPath: "codex",
				MCP: CodexMCPConfig{
					ConfigPath:      "",
					UseIsolatedHome: true,
					HomePath:        "",
					ServerName:      "localclaw",
				},
			},
		},
		Channels: ChannelsConfig{Enabled: []string{"slack", "signal"}},
		State:    StateConfig{Root: "~/.localclaw"},
		Agents: AgentsConfig{
			Defaults: AgentDefaultsConfig{
				Workspace: ".",
				Tools: ToolsConfig{
					Delegated: DelegatedToolsConfig{
						Enabled: false,
					},
				},
				Skills: SkillsConfig{},
				Compaction: CompactionConfig{
					MemoryFlush: MemoryFlushConfig{
						Enabled:             true,
						ThresholdTokens:     28000,
						TriggerWindowTokens: 4000,
						Prompt:              "",
						TimeoutSeconds:      20,
					},
				},
				MemorySearch: MemorySearchConfig{
					Enabled:    false,
					Sources:    []string{"memory"},
					ExtraPaths: []string{},
					Provider:   "auto",
					Fallback:   "none",
					Store: MemorySearchStoreConfig{
						Path: "~/.localclaw/memory/{agentId}.sqlite",
						Vector: MemorySearchVectorMode{
							Enabled: true,
						},
					},
					Chunking: ChunkingConfig{
						Tokens:  400,
						Overlap: 40,
					},
					Query: QueryConfig{
						MaxResults: 8,
						MinScore:   0,
						Hybrid: QueryHybridConfig{
							Enabled:             true,
							VectorWeight:        0.8,
							KeywordWeight:       0.2,
							CandidateMultiplier: 4,
						},
					},
					Sync: SyncConfig{
						OnSearch: false,
						Sessions: SyncSessionsConfig{
							DeltaBytes:    32768,
							DeltaMessages: 20,
						},
					},
					Cache: CacheConfig{
						Enabled:    true,
						MaxEntries: 1000,
					},
					Local: LocalConfig{
						RuntimePath:         "",
						ModelPath:           "",
						ModelCacheDir:       "",
						QueryTimeoutSeconds: 0,
						BatchTimeoutSeconds: 0,
					},
				},
			},
			List: []AgentConfig{},
		},
		Session: SessionConfig{
			Store: "~/.localclaw/agents/{agentId}/sessions/sessions.json",
		},
		Tools: ToolsConfig{
			Delegated: DelegatedToolsConfig{
				Enabled: false,
			},
		},
		Skills:    SkillsConfig{},
		Cron:      CronConfig{Enabled: true},
		Heartbeat: HeartbeatConfig{Enabled: true, IntervalSeconds: 30},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	loadPath := strings.TrimSpace(path)
	if loadPath == "" {
		if home, err := os.UserHomeDir(); err == nil {
			defaultPath := filepath.Join(home, ".localclaw", "localclaw.json")
			if _, statErr := os.Stat(defaultPath); statErr == nil {
				loadPath = defaultPath
			} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
				return Config{}, fmt.Errorf("stat default config: %w", statErr)
			}
		}
	}

	if loadPath == "" {
		return cfg, cfg.Validate()
	}

	buf, err := os.ReadFile(loadPath)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(buf, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	return cfg, cfg.Validate()
}

func (c Config) Validate() error {
	if c.App.Name == "" {
		return errors.New("app.name is required")
	}
	for idx, message := range c.App.ThinkingMessages {
		if strings.TrimSpace(message) == "" {
			return fmt.Errorf("app.thinking_messages[%d] cannot be blank", idx)
		}
	}
	if c.LLM.Provider != "claudecode" && c.LLM.Provider != "codex" {
		return fmt.Errorf("unsupported llm.provider %q", c.LLM.Provider)
	}
	if c.LLM.Provider == "claudecode" {
		if strings.TrimSpace(c.LLM.ClaudeCode.BinaryPath) == "" {
			return errors.New("llm.claude_code.binary_path is required")
		}
	}
	if c.LLM.Provider == "codex" {
		if strings.TrimSpace(c.LLM.Codex.BinaryPath) == "" {
			return errors.New("llm.codex.binary_path is required")
		}
	}
	if len(c.Channels.Enabled) == 0 {
		return errors.New("channels.enabled must include at least one channel")
	}
	if strings.TrimSpace(c.State.Root) == "" {
		return errors.New("state.root is required")
	}
	if strings.TrimSpace(c.Agents.Defaults.Workspace) == "" {
		return errors.New("agents.defaults.workspace is required")
	}
	if err := validateToolsConfig(c.Tools, "tools"); err != nil {
		return err
	}
	if err := validateSkillsConfig(c.Skills, "skills"); err != nil {
		return err
	}
	if err := validateToolsConfig(c.Agents.Defaults.Tools, "agents.defaults.tools"); err != nil {
		return err
	}
	if err := validateSkillsConfig(c.Agents.Defaults.Skills, "agents.defaults.skills"); err != nil {
		return err
	}
	if strings.TrimSpace(c.Session.Store) == "" {
		return errors.New("session.store is required")
	}
	seen := map[string]struct{}{}
	for _, channel := range c.Channels.Enabled {
		if !containsString(allowedChannels, channel) {
			return fmt.Errorf("unsupported channel %q", channel)
		}
		if _, ok := seen[channel]; ok {
			return fmt.Errorf("duplicate channel %q", channel)
		}
		seen[channel] = struct{}{}
	}
	seenAgentIDs := map[string]struct{}{}
	for _, agent := range c.Agents.List {
		agentID := strings.TrimSpace(agent.ID)
		if agentID == "" {
			return errors.New("agents.list[].id is required")
		}
		if agent.Workspace != "" && strings.TrimSpace(agent.Workspace) == "" {
			return errors.New("agents.list[].workspace cannot be blank")
		}
		if _, ok := seenAgentIDs[agentID]; ok {
			return fmt.Errorf("duplicate agent id %q", agentID)
		}
		seenAgentIDs[agentID] = struct{}{}
		if err := validateToolsConfig(agent.Tools, "agents.list[].tools"); err != nil {
			return err
		}
		if err := validateSkillsConfig(agent.Skills, "agents.list[].skills"); err != nil {
			return err
		}
		if err := validateMemoryFlushConfig(agent.Compaction.MemoryFlush, "agents.list[].compaction.memoryFlush"); err != nil {
			return err
		}
		if err := validateLocalEmbeddingConfig(agent.MemorySearch.Local, "agents.list[].memorySearch.local"); err != nil {
			return err
		}
	}
	if err := validateMemoryFlushConfig(c.Agents.Defaults.Compaction.MemoryFlush, "agents.defaults.compaction.memoryFlush"); err != nil {
		return err
	}
	if err := validateLocalEmbeddingConfig(c.Agents.Defaults.MemorySearch.Local, "agents.defaults.memorySearch.local"); err != nil {
		return err
	}
	if c.Heartbeat.Enabled && c.Heartbeat.IntervalSeconds <= 0 {
		return errors.New("heartbeat.interval_seconds must be > 0")
	}
	return c.ValidateLocalOnlyPolicy()
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func validateMemoryFlushConfig(cfg MemoryFlushConfig, fieldPrefix string) error {
	if cfg.ThresholdTokens < 0 {
		return fmt.Errorf("%s.thresholdTokens must be >= 0", fieldPrefix)
	}
	if cfg.TriggerWindowTokens < 0 {
		return fmt.Errorf("%s.triggerWindowTokens must be >= 0", fieldPrefix)
	}
	if cfg.TimeoutSeconds < 0 {
		return fmt.Errorf("%s.timeoutSeconds must be >= 0", fieldPrefix)
	}
	return nil
}

func validateLocalEmbeddingConfig(cfg LocalConfig, fieldPrefix string) error {
	if strings.TrimSpace(cfg.RuntimePath) == "" && cfg.RuntimePath != "" {
		return fmt.Errorf("%s.runtimePath cannot be blank", fieldPrefix)
	}
	if cfg.QueryTimeoutSeconds < 0 {
		return fmt.Errorf("%s.queryTimeoutSeconds must be >= 0", fieldPrefix)
	}
	if cfg.BatchTimeoutSeconds < 0 {
		return fmt.Errorf("%s.batchTimeoutSeconds must be >= 0", fieldPrefix)
	}
	return nil
}

func validateToolsConfig(cfg ToolsConfig, fieldPrefix string) error {
	if err := validatePolicyNameList(cfg.Allow, fieldPrefix+".allow"); err != nil {
		return err
	}
	if err := validatePolicyNameList(cfg.Deny, fieldPrefix+".deny"); err != nil {
		return err
	}
	if err := validatePolicyNameList(cfg.Delegated.Allow, fieldPrefix+".delegated.allow"); err != nil {
		return err
	}
	if err := validatePolicyNameList(cfg.Delegated.Deny, fieldPrefix+".delegated.deny"); err != nil {
		return err
	}
	return nil
}

func validateSkillsConfig(cfg SkillsConfig, fieldPrefix string) error {
	if err := validatePolicyNameList(cfg.Enabled, fieldPrefix+".enabled"); err != nil {
		return err
	}
	if err := validatePolicyNameList(cfg.Disabled, fieldPrefix+".disabled"); err != nil {
		return err
	}
	return nil
}

func validatePolicyNameList(values []string, field string) error {
	seen := map[string]struct{}{}
	for idx, raw := range values {
		normalized := strings.ToLower(strings.TrimSpace(raw))
		if normalized == "" {
			return fmt.Errorf("%s[%d] cannot be blank", field, idx)
		}
		if _, ok := seen[normalized]; ok {
			return fmt.Errorf("duplicate %s entry %q", field, normalized)
		}
		seen[normalized] = struct{}{}
	}
	return nil
}

// ValidateLocalOnlyPolicy enforces startup guardrails to keep localclaw non-network-exposed.
func (c Config) ValidateLocalOnlyPolicy() error {
	if !c.Security.EnforceLocalOnly {
		return errors.New("security.enforce_local_only must remain true")
	}
	if c.Security.EnableGateway {
		return errors.New("security.enable_gateway is forbidden in local-only mode")
	}
	if c.Security.EnableHTTPServer {
		return errors.New("security.enable_http_server is forbidden in local-only mode")
	}
	if c.Security.ListenAddress != "" {
		return errors.New("security.listen_address must be empty in local-only mode")
	}
	return nil
}
