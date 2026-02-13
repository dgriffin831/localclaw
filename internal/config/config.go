package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
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

type MemoryConfig struct {
	Path string `json:"path"`
}

type WorkspaceConfig struct {
	Root string `json:"root"`
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
		Memory:   MemoryConfig{Path: ".localclaw/memory.json"},
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
		return cfg, cfg.Validate()
	}

	buf, err := os.ReadFile(path)
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
	if c.LLM.Provider != "claudecode" {
		return fmt.Errorf("unsupported llm.provider %q", c.LLM.Provider)
	}
	if c.LLM.ClaudeCode.BinaryPath == "" {
		return errors.New("llm.claude_code.binary_path is required")
	}
	if !slices.Contains(allowedClaudeAuthModes, c.LLM.ClaudeCode.AuthMode) {
		return fmt.Errorf("unsupported llm.claude_code.auth_mode %q", c.LLM.ClaudeCode.AuthMode)
	}
	if c.LLM.ClaudeCode.UseGovCloud && strings.TrimSpace(c.LLM.ClaudeCode.BedrockRegion) == "" {
		return errors.New("llm.claude_code.bedrock_region is required when use_govcloud is true")
	}
	if len(c.Channels.Enabled) == 0 {
		return errors.New("channels.enabled must include at least one channel")
	}
	seen := map[string]struct{}{}
	for _, channel := range c.Channels.Enabled {
		if !slices.Contains(allowedChannels, channel) {
			return fmt.Errorf("unsupported channel %q", channel)
		}
		if _, ok := seen[channel]; ok {
			return fmt.Errorf("duplicate channel %q", channel)
		}
		seen[channel] = struct{}{}
	}
	if c.Heartbeat.Enabled && c.Heartbeat.IntervalSeconds <= 0 {
		return errors.New("heartbeat.interval_seconds must be > 0")
	}
	return c.ValidateLocalOnlyPolicy()
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
