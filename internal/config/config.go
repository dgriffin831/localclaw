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
var allowedClaudeAuthModes = []string{"default", "aws_profile", "bedrock"}

// Config contains all runtime configuration for localclaw.
type Config struct {
	App       AppConfig       `json:"app"`
	Security  SecurityConfig  `json:"security"`
	LLM       LLMConfig       `json:"llm"`
	Channels  ChannelsConfig  `json:"channels"`
	State     StateConfig     `json:"state"`
	Agents    AgentsConfig    `json:"agents"`
	Session   SessionConfig   `json:"session"`
	Memory    MemoryConfig    `json:"memory"`
	Workspace WorkspaceConfig `json:"workspace"`
	Cron      CronConfig      `json:"cron"`
	Heartbeat HeartbeatConfig `json:"heartbeat"`
}

type AppConfig struct {
	Name string `json:"name"`
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
}

type ClaudeCodeConfig struct {
	BinaryPath    string `json:"binary_path"`
	Profile       string `json:"profile"`
	UseGovCloud   bool   `json:"use_govcloud"`
	BedrockRegion string `json:"bedrock_region"`
	AuthMode      string `json:"auth_mode"`
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
}

type AgentConfig struct {
	ID           string             `json:"id"`
	Workspace    string             `json:"workspace,omitempty"`
	MemorySearch MemorySearchConfig `json:"memorySearch,omitempty"`
	Compaction   CompactionConfig   `json:"compaction,omitempty"`
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

type MemoryConfig struct {
	Path string `json:"path"`
}

type WorkspaceConfig struct {
	Root string `json:"root"`
}

type MemorySearchConfig struct {
	Enabled          bool                    `json:"enabled"`
	Sources          []string                `json:"sources"`
	ExtraPaths       []string                `json:"extraPaths"`
	Provider         string                  `json:"provider"`
	Fallback         string                  `json:"fallback"`
	Model            string                  `json:"model"`
	Store            MemorySearchStoreConfig `json:"store"`
	Chunking         ChunkingConfig          `json:"chunking"`
	Query            QueryConfig             `json:"query"`
	Sync             SyncConfig              `json:"sync"`
	Cache            CacheConfig             `json:"cache"`
	Local            LocalConfig             `json:"local"`
	Remote           RemoteConfig            `json:"remote"`
	LegacyImportPath string                  `json:"legacyImportPath,omitempty"`
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
	OnSessionStart  bool               `json:"onSessionStart"`
	OnSearch        bool               `json:"onSearch"`
	Watch           bool               `json:"watch"`
	WatchDebounceMs int                `json:"watchDebounceMs"`
	IntervalMinutes int                `json:"intervalMinutes"`
	Sessions        SyncSessionsConfig `json:"sessions"`
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
	ModelPath     string `json:"modelPath"`
	ModelCacheDir string `json:"modelCacheDir"`
}

type RemoteConfig struct {
	BaseURL string            `json:"baseURL"`
	APIKey  string            `json:"apiKey"`
	Headers map[string]string `json:"headers"`
	Batch   RemoteBatchConfig `json:"batch"`
}

type RemoteBatchConfig struct {
	Enabled bool `json:"enabled"`
	Size    int  `json:"size"`
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
				BinaryPath:    "claude",
				Profile:       "default",
				UseGovCloud:   false,
				BedrockRegion: "",
				AuthMode:      "default",
			},
		},
		Channels: ChannelsConfig{Enabled: []string{"slack", "signal"}},
		State:    StateConfig{Root: "~/.localclaw"},
		Agents: AgentsConfig{
			Defaults: AgentDefaultsConfig{
				Workspace: ".",
				Compaction: CompactionConfig{
					MemoryFlush: MemoryFlushConfig{
						Enabled:             false,
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
						OnSessionStart:  false,
						OnSearch:        false,
						Watch:           false,
						WatchDebounceMs: 500,
						IntervalMinutes: 0,
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
						ModelPath:     "",
						ModelCacheDir: "",
					},
					Remote: RemoteConfig{
						Headers: map[string]string{},
						Batch: RemoteBatchConfig{
							Enabled: false,
							Size:    16,
						},
					},
				},
			},
			List: []AgentConfig{},
		},
		Session: SessionConfig{
			Store: "~/.localclaw/agents/{agentId}/sessions/sessions.json",
		},
		Memory: MemoryConfig{Path: ".localclaw/memory.json"},
		Workspace: WorkspaceConfig{
			Root: ".",
		},
		Cron:      CronConfig{Enabled: true},
		Heartbeat: HeartbeatConfig{Enabled: true, IntervalSeconds: 30},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		cfg.applyCompatibilityMappings()
		return cfg, cfg.Validate()
	}

	buf, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(buf, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	cfg.applyCompatibilityMappings()
	return cfg, cfg.Validate()
}

func (c Config) Validate() error {
	if c.App.Name == "" {
		return errors.New("app.name is required")
	}
	if c.LLM.Provider != "claudecode" {
		return fmt.Errorf("unsupported llm.provider %q", c.LLM.Provider)
	}
	if c.LLM.ClaudeCode.BinaryPath == "" {
		return errors.New("llm.claude_code.binary_path is required")
	}
	if !containsString(allowedClaudeAuthModes, c.LLM.ClaudeCode.AuthMode) {
		return fmt.Errorf("unsupported llm.claude_code.auth_mode %q", c.LLM.ClaudeCode.AuthMode)
	}
	if c.LLM.ClaudeCode.UseGovCloud && strings.TrimSpace(c.LLM.ClaudeCode.BedrockRegion) == "" {
		return errors.New("llm.claude_code.bedrock_region is required when use_govcloud is true")
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
		if err := validateMemoryFlushConfig(agent.Compaction.MemoryFlush, "agents.list[].compaction.memoryFlush"); err != nil {
			return err
		}
	}
	if err := validateMemoryFlushConfig(c.Agents.Defaults.Compaction.MemoryFlush, "agents.defaults.compaction.memoryFlush"); err != nil {
		return err
	}
	if c.Heartbeat.Enabled && c.Heartbeat.IntervalSeconds <= 0 {
		return errors.New("heartbeat.interval_seconds must be > 0")
	}
	return c.ValidateLocalOnlyPolicy()
}

func (c *Config) applyCompatibilityMappings() {
	defaults := Default()

	legacyWorkspaceRoot := strings.TrimSpace(c.Workspace.Root)
	defaultWorkspace := strings.TrimSpace(c.Agents.Defaults.Workspace)
	defaultWorkspaceDefault := strings.TrimSpace(defaults.Agents.Defaults.Workspace)
	switch {
	case legacyWorkspaceRoot != "" && (defaultWorkspace == "" || defaultWorkspace == defaultWorkspaceDefault):
		c.Agents.Defaults.Workspace = legacyWorkspaceRoot
	case legacyWorkspaceRoot == "" && defaultWorkspace != "":
		c.Workspace.Root = defaultWorkspace
	}

	legacyMemoryPath := strings.TrimSpace(c.Memory.Path)
	if strings.TrimSpace(c.Agents.Defaults.MemorySearch.LegacyImportPath) == "" && legacyMemoryPath != "" {
		c.Agents.Defaults.MemorySearch.LegacyImportPath = legacyMemoryPath
	}

	stateRoot := strings.TrimSpace(c.State.Root)
	if stateRoot == "" {
		return
	}
	defaultSessionStore := strings.TrimSpace(defaults.Session.Store)
	if strings.TrimSpace(c.Session.Store) == defaultSessionStore {
		c.Session.Store = filepath.Join(stateRoot, "agents", "{agentId}", "sessions", "sessions.json")
	}
	defaultMemoryStorePath := strings.TrimSpace(defaults.Agents.Defaults.MemorySearch.Store.Path)
	if strings.TrimSpace(c.Agents.Defaults.MemorySearch.Store.Path) == defaultMemoryStorePath {
		c.Agents.Defaults.MemorySearch.Store.Path = filepath.Join(stateRoot, "memory", "{agentId}.sqlite")
	}
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
