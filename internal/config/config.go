package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var allowedChannels = []string{"slack", "signal"}

var defaultClaudeAllowedMCPTools = []string{
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

var allowedCodexReasoningLevels = []string{
	"xlow",
	"low",
	"medium",
	"high",
	"xhigh",
}

// Config contains all runtime configuration for localclaw.
type Config struct {
	App       AppConfig       `json:"app"`
	LLM       LLMConfig       `json:"llm"`
	Channels  ChannelsConfig  `json:"channels"`
	Agents    AgentsConfig    `json:"agents"`
	Session   SessionConfig   `json:"session"`
	Cron      CronConfig      `json:"cron"`
	Heartbeat HeartbeatConfig `json:"heartbeat"`
}

type AppConfig struct {
	Name             string           `json:"name"`
	Root             string           `json:"root"`
	Default          AppDefaultConfig `json:"default"`
	ThinkingMessages []string         `json:"thinking_messages,omitempty"`
}

type AppDefaultConfig struct {
	Verbose bool `json:"verbose"`
	Mouse   bool `json:"mouse"`
	Tools   bool `json:"tools"`
}

type LLMConfig struct {
	Provider   string           `json:"provider"`
	ClaudeCode ClaudeCodeConfig `json:"claude_code"`
	Codex      CodexConfig      `json:"codex"`
}

type ClaudeCodeConfig struct {
	BinaryPath      string   `json:"binary_path"`
	Profile         string   `json:"profile"`
	ExtraArgs       []string `json:"extra_args"`
	SessionMode     string   `json:"session_mode"`
	SessionArg      string   `json:"session_arg"`
	ResumeArgs      []string `json:"resume_args"`
	SessionIDFields []string `json:"session_id_fields"`
}

type CodexConfig struct {
	BinaryPath       string         `json:"binary_path"`
	Profile          string         `json:"profile"`
	Model            string         `json:"model"`
	ReasoningDefault string         `json:"reasoning_default"`
	ExtraArgs        []string       `json:"extra_args"`
	SessionMode      string         `json:"session_mode"`
	ResumeArgs       []string       `json:"resume_args"`
	SessionIDFields  []string       `json:"session_id_fields"`
	ResumeOutput     string         `json:"resume_output"`
	MCP              CodexMCPConfig `json:"mcp"`
}

type CodexMCPConfig struct {
	ConfigPath string `json:"config_path"`
	ServerName string `json:"server_name"`
}

type ChannelsConfig struct {
	Enabled []string             `json:"enabled"`
	Slack   SlackChannelsConfig  `json:"slack"`
	Signal  SignalChannelsConfig `json:"signal"`
}

type SlackChannelsConfig struct {
	BotTokenEnv    string `json:"bot_token_env"`
	DefaultChannel string `json:"default_channel"`
	APIBaseURL     string `json:"api_base_url"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

type SignalChannelsConfig struct {
	CLIPath          string `json:"cli_path"`
	Account          string `json:"account"`
	DefaultRecipient string `json:"default_recipient"`
	TimeoutSeconds   int    `json:"timeout_seconds"`
}

type AgentsConfig struct {
	Defaults AgentDefaultsConfig `json:"defaults"`
	List     []AgentConfig       `json:"list"`
}

type AgentDefaultsConfig struct {
	Workspace  string           `json:"workspace"`
	Memory     MemoryConfig     `json:"memory"`
	Compaction CompactionConfig `json:"compaction"`
}

type AgentConfig struct {
	ID         string               `json:"id"`
	Workspace  string               `json:"workspace,omitempty"`
	Memory     MemoryOverrideConfig `json:"memory,omitempty"`
	Compaction CompactionConfig     `json:"compaction,omitempty"`
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
	Enabled    bool              `json:"enabled"`
	Tools      MemoryToolsConfig `json:"tools"`
	Sources    []string          `json:"sources"`
	ExtraPaths []string          `json:"extraPaths"`
	Store      MemoryStoreConfig `json:"store"`
	Chunking   ChunkingConfig    `json:"chunking"`
	Query      QueryConfig       `json:"query"`
	Sync       SyncConfig        `json:"sync"`
}

type MemoryToolsConfig struct {
	Get    bool `json:"get"`
	Search bool `json:"search"`
	Grep   bool `json:"grep"`
}

type MemoryOverrideConfig struct {
	Enabled    *bool                     `json:"enabled,omitempty"`
	Tools      MemoryToolsOverrideConfig `json:"tools,omitempty"`
	Sources    []string                  `json:"sources,omitempty"`
	ExtraPaths []string                  `json:"extraPaths,omitempty"`
	Store      MemoryStoreConfig         `json:"store,omitempty"`
	Chunking   ChunkingConfig            `json:"chunking,omitempty"`
	Query      QueryConfig               `json:"query,omitempty"`
	Sync       SyncConfig                `json:"sync,omitempty"`
}

type MemoryToolsOverrideConfig struct {
	Get    *bool `json:"get,omitempty"`
	Search *bool `json:"search,omitempty"`
	Grep   *bool `json:"grep,omitempty"`
}

type MemoryStoreConfig struct {
	Path string `json:"path"`
}

type ChunkingConfig struct {
	Tokens  int `json:"tokens"`
	Overlap int `json:"overlap"`
}

type QueryConfig struct {
	MaxResults int `json:"maxResults"`
}

type SyncConfig struct {
	OnSearch bool               `json:"onSearch"`
	Sessions SyncSessionsConfig `json:"sessions"`
}

type SyncSessionsConfig struct {
	DeltaBytes    int `json:"deltaBytes"`
	DeltaMessages int `json:"deltaMessages"`
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
		App: AppConfig{
			Name: "localclaw",
			Root: "~/.localclaw",
			Default: AppDefaultConfig{
				Verbose: false,
				Mouse:   false,
				Tools:   false,
			},
		},
		LLM: LLMConfig{
			Provider: "claudecode",
			ClaudeCode: ClaudeCodeConfig{
				BinaryPath:  "claude",
				Profile:     "default",
				ExtraArgs:   []string{"--allowed-tools", strings.Join(defaultClaudeAllowedMCPTools, ",")},
				SessionMode: "always",
				SessionArg:  "--session-id",
				ResumeArgs:  []string{"--resume", "{sessionId}"},
				SessionIDFields: []string{
					"session_id",
					"sessionId",
					"conversation_id",
					"conversationId",
				},
			},
			Codex: CodexConfig{
				BinaryPath:       "codex",
				ReasoningDefault: "medium",
				ExtraArgs:        []string{"--skip-git-repo-check"},
				SessionMode:      "existing",
				ResumeArgs:       []string{"resume", "{sessionId}"},
				SessionIDFields: []string{
					"thread_id",
					"threadId",
					"session_id",
					"sessionId",
				},
				ResumeOutput: "json",
				MCP: CodexMCPConfig{
					ConfigPath: "",
					ServerName: "localclaw",
				},
			},
		},
		Channels: ChannelsConfig{
			Enabled: []string{"slack", "signal"},
			Slack: SlackChannelsConfig{
				BotTokenEnv:    "SLACK_BOT_TOKEN",
				DefaultChannel: "",
				APIBaseURL:     "https://slack.com/api",
				TimeoutSeconds: 10,
			},
			Signal: SignalChannelsConfig{
				CLIPath:          "signal-cli",
				Account:          "+10000000000",
				DefaultRecipient: "",
				TimeoutSeconds:   10,
			},
		},
		Agents: AgentsConfig{
			Defaults: AgentDefaultsConfig{
				Workspace: ".",
				Memory: MemoryConfig{
					Enabled: true,
					Tools: MemoryToolsConfig{
						Get:    true,
						Search: true,
						Grep:   true,
					},
					Sources:    []string{"memory"},
					ExtraPaths: []string{},
					Store: MemoryStoreConfig{
						Path: "~/.localclaw/memory/{agentId}.sqlite",
					},
					Chunking: ChunkingConfig{
						Tokens:  400,
						Overlap: 40,
					},
					Query: QueryConfig{
						MaxResults: 8,
					},
					Sync: SyncConfig{
						OnSearch: false,
						Sessions: SyncSessionsConfig{
							DeltaBytes:    32768,
							DeltaMessages: 20,
						},
					},
				},
				Compaction: CompactionConfig{
					MemoryFlush: MemoryFlushConfig{
						Enabled:             true,
						ThresholdTokens:     28000,
						TriggerWindowTokens: 4000,
						Prompt:              "",
						TimeoutSeconds:      20,
					},
				},
			},
			List: []AgentConfig{},
		},
		Session: SessionConfig{
			Store: "~/.localclaw/agents/{agentId}/sessions/sessions.json",
		},
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
	decoder := json.NewDecoder(bytes.NewReader(buf))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	return cfg, cfg.Validate()
}

func (c Config) Validate() error {
	if c.App.Name == "" {
		return errors.New("app.name is required")
	}
	if strings.TrimSpace(c.App.Root) == "" {
		return errors.New("app.root is required")
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
	if err := validateProviderSessionMode(c.LLM.ClaudeCode.SessionMode, "llm.claude_code.session_mode"); err != nil {
		return err
	}
	if err := validateResumeArgs(c.LLM.ClaudeCode.SessionMode, c.LLM.ClaudeCode.ResumeArgs, "llm.claude_code.resume_args"); err != nil {
		return err
	}
	if err := validateSessionIDFields(c.LLM.ClaudeCode.SessionIDFields, "llm.claude_code.session_id_fields"); err != nil {
		return err
	}
	if err := validateProviderSessionMode(c.LLM.Codex.SessionMode, "llm.codex.session_mode"); err != nil {
		return err
	}
	if err := validateResumeArgs(c.LLM.Codex.SessionMode, c.LLM.Codex.ResumeArgs, "llm.codex.resume_args"); err != nil {
		return err
	}
	if err := validateSessionIDFields(c.LLM.Codex.SessionIDFields, "llm.codex.session_id_fields"); err != nil {
		return err
	}
	if err := validateCodexResumeOutput(c.LLM.Codex.ResumeOutput); err != nil {
		return err
	}
	if err := validateCodexReasoningDefault(c.LLM.Codex.ReasoningDefault); err != nil {
		return err
	}
	if len(c.Channels.Enabled) == 0 {
		return errors.New("channels.enabled must include at least one channel")
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
	if _, enabled := seen["slack"]; enabled {
		if strings.TrimSpace(c.Channels.Slack.BotTokenEnv) == "" {
			return errors.New("channels.slack.bot_token_env is required when slack is enabled")
		}
		if strings.TrimSpace(c.Channels.Slack.APIBaseURL) == "" {
			return errors.New("channels.slack.api_base_url is required when slack is enabled")
		}
		if c.Channels.Slack.TimeoutSeconds <= 0 {
			return errors.New("channels.slack.timeout_seconds must be > 0 when slack is enabled")
		}
	}
	if _, enabled := seen["signal"]; enabled {
		if strings.TrimSpace(c.Channels.Signal.CLIPath) == "" {
			return errors.New("channels.signal.cli_path is required when signal is enabled")
		}
		if strings.TrimSpace(c.Channels.Signal.Account) == "" {
			return errors.New("channels.signal.account is required when signal is enabled")
		}
		if c.Channels.Signal.TimeoutSeconds <= 0 {
			return errors.New("channels.signal.timeout_seconds must be > 0 when signal is enabled")
		}
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
	return nil
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

func validateProviderSessionMode(value, fieldName string) error {
	mode := strings.ToLower(strings.TrimSpace(value))
	switch mode {
	case "always", "existing", "none":
		return nil
	default:
		return fmt.Errorf("%s must be one of: always, existing, none", fieldName)
	}
}

func validateResumeArgs(mode string, resumeArgs []string, fieldName string) error {
	if strings.ToLower(strings.TrimSpace(mode)) != "existing" {
		return nil
	}
	if len(resumeArgs) == 0 {
		return nil
	}
	for _, arg := range resumeArgs {
		if strings.Contains(arg, "{sessionId}") {
			return nil
		}
	}
	return fmt.Errorf("%s must include {sessionId} placeholder when session_mode=existing", fieldName)
}

func validateSessionIDFields(fields []string, fieldName string) error {
	for i, field := range fields {
		if strings.TrimSpace(field) == "" {
			return fmt.Errorf("%s[%d] cannot be blank", fieldName, i)
		}
	}
	return nil
}

func validateCodexResumeOutput(value string) error {
	mode := strings.ToLower(strings.TrimSpace(value))
	switch mode {
	case "", "json", "jsonl", "text":
		return nil
	default:
		return errors.New("llm.codex.resume_output must be one of: json, jsonl, text")
	}
}

func validateCodexReasoningDefault(value string) error {
	level := strings.ToLower(strings.TrimSpace(value))
	if level == "" {
		return errors.New("llm.codex.reasoning_default is required")
	}
	if !containsString(allowedCodexReasoningLevels, level) {
		return fmt.Errorf("llm.codex.reasoning_default must be one of: %s", strings.Join(allowedCodexReasoningLevels, ", "))
	}
	return nil
}
