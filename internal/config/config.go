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

// Config contains all runtime configuration for localclaw.
type Config struct {
	App    AppConfig    `json:"app"`
	Agents AgentsConfig `json:"agents"`
}

type AppConfig struct {
	Name string `json:"name"`
	Root string `json:"root"`
}

type AgentsConfig struct {
	Defaults AgentDefaultsConfig `json:"defaults"`
	List     []AgentConfig       `json:"list"`
}

type AgentDefaultsConfig struct {
	Workspace string       `json:"workspace"`
	Memory    MemoryConfig `json:"memory"`
}

type AgentConfig struct {
	ID        string               `json:"id"`
	Workspace string               `json:"workspace,omitempty"`
	Memory    MemoryOverrideConfig `json:"memory,omitempty"`
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
	Vector     VectorConfig      `json:"vector"`
}

type MemoryToolsConfig struct {
	Create bool `json:"create"`
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
	Sync       SyncOverrideConfig        `json:"sync,omitempty"`
	Vector     VectorOverrideConfig      `json:"vector,omitempty"`
}

type MemoryToolsOverrideConfig struct {
	Create *bool `json:"create,omitempty"`
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
	OnSearch bool `json:"onSearch"`
}

type SyncOverrideConfig struct {
	OnSearch *bool `json:"onSearch,omitempty"`
}

type VectorConfig struct {
	Enabled    bool               `json:"enabled"`
	Provider   string             `json:"provider"`
	SearchMode string             `json:"searchMode"`
	Model      VectorModelConfig  `json:"model"`
	Server     VectorServerConfig `json:"server"`
}

type VectorOverrideConfig struct {
	Enabled    *bool                      `json:"enabled,omitempty"`
	Provider   string                     `json:"provider,omitempty"`
	SearchMode string                     `json:"searchMode,omitempty"`
	Model      VectorModelConfig          `json:"model,omitempty"`
	Server     VectorServerOverrideConfig `json:"server,omitempty"`
}

type VectorModelConfig struct {
	ID         string `json:"id,omitempty"`
	Path       string `json:"path,omitempty"`
	PrimaryURL string `json:"primaryUrl,omitempty"`
	MirrorURL  string `json:"mirrorUrl,omitempty"`
	SHA256     string `json:"sha256,omitempty"`
}

type VectorServerConfig struct {
	Managed               bool   `json:"managed"`
	Binary                string `json:"binary"`
	Host                  string `json:"host"`
	Port                  int    `json:"port"`
	StartupTimeoutSeconds int    `json:"startupTimeoutSeconds"`
}

type VectorServerOverrideConfig struct {
	Managed               *bool  `json:"managed,omitempty"`
	Binary                string `json:"binary,omitempty"`
	Host                  string `json:"host,omitempty"`
	Port                  int    `json:"port,omitempty"`
	StartupTimeoutSeconds int    `json:"startupTimeoutSeconds,omitempty"`
}

func Default() Config {
	return Config{
		App: AppConfig{
			Name: "localclaw",
			Root: "~/.localclaw",
		},
		Agents: AgentsConfig{
			Defaults: AgentDefaultsConfig{
				Workspace: ".",
				Memory: MemoryConfig{
					Enabled: true,
					Tools: MemoryToolsConfig{
						Create: true,
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
					},
					Vector: VectorConfig{
						Enabled:    true,
						Provider:   "llama.cpp",
						SearchMode: "hybrid",
						Model: VectorModelConfig{
							ID:         "embeddinggemma-300M-Q8_0",
							Path:       "~/.localclaw/models/embeddinggemma-300M-Q8_0.gguf",
							PrimaryURL: "https://huggingface.co/ggml-org/embeddinggemma-300M-GGUF/resolve/0f741b5a6585bd53aeb15cd1372c56f2a0f65e12/embeddinggemma-300M-Q8_0.gguf",
							MirrorURL:  "https://github.com/dgriffin831/embeddinggemma-gguf-mirror/raw/main/embeddinggemma-300M-Q8_0.gguf",
							SHA256:     "b5ce9d77a3fc4b3b39ccb5643c36777911cc4eb46a66962eadfa3f5f60490d63",
						},
						Server: VectorServerConfig{
							Managed:               true,
							Binary:                "llama-server",
							Host:                  "127.0.0.1",
							Port:                  0,
							StartupTimeoutSeconds: 20,
						},
					},
				},
			},
			List: []AgentConfig{},
		},
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
	if strings.TrimSpace(c.App.Name) == "" {
		return errors.New("app.name is required")
	}
	if strings.TrimSpace(c.App.Root) == "" {
		return errors.New("app.root is required")
	}
	if strings.TrimSpace(c.Agents.Defaults.Workspace) == "" {
		return errors.New("agents.defaults.workspace is required")
	}
	if err := validateMemoryConfig(c.Agents.Defaults.Memory, "agents.defaults.memory"); err != nil {
		return err
	}

	seenAgentIDs := map[string]struct{}{}
	for _, agent := range c.Agents.List {
		agentID := strings.TrimSpace(agent.ID)
		if agentID == "" {
			return errors.New("agents.list[].id is required")
		}
		if _, ok := seenAgentIDs[agentID]; ok {
			return fmt.Errorf("duplicate agent id %q", agentID)
		}
		seenAgentIDs[agentID] = struct{}{}
		if agent.Workspace != "" && strings.TrimSpace(agent.Workspace) == "" {
			return errors.New("agents.list[].workspace cannot be blank")
		}
		if err := validateMemoryOverrideConfig(agent.Memory, "agents.list[].memory"); err != nil {
			return err
		}
	}
	return nil
}

func validateMemoryConfig(cfg MemoryConfig, prefix string) error {
	if strings.TrimSpace(cfg.Store.Path) == "" {
		return fmt.Errorf("%s.store.path is required", prefix)
	}
	if cfg.Chunking.Tokens <= 0 {
		return fmt.Errorf("%s.chunking.tokens must be > 0", prefix)
	}
	if cfg.Chunking.Overlap < 0 {
		return fmt.Errorf("%s.chunking.overlap must be >= 0", prefix)
	}
	if cfg.Chunking.Overlap >= cfg.Chunking.Tokens {
		return fmt.Errorf("%s.chunking.overlap must be less than chunking.tokens", prefix)
	}
	if cfg.Query.MaxResults <= 0 {
		return fmt.Errorf("%s.query.maxResults must be > 0", prefix)
	}
	if err := validateMemorySources(cfg.Sources, prefix+".sources"); err != nil {
		return err
	}
	return validateVectorConfig(cfg.Vector, prefix+".vector")
}

func validateMemoryOverrideConfig(cfg MemoryOverrideConfig, prefix string) error {
	if len(cfg.Sources) > 0 {
		if err := validateMemorySources(cfg.Sources, prefix+".sources"); err != nil {
			return err
		}
	}
	if cfg.Chunking.Tokens < 0 {
		return fmt.Errorf("%s.chunking.tokens must be >= 0", prefix)
	}
	if cfg.Chunking.Overlap < 0 {
		return fmt.Errorf("%s.chunking.overlap must be >= 0", prefix)
	}
	if cfg.Chunking.Tokens > 0 && cfg.Chunking.Overlap >= cfg.Chunking.Tokens {
		return fmt.Errorf("%s.chunking.overlap must be less than chunking.tokens", prefix)
	}
	if cfg.Query.MaxResults < 0 {
		return fmt.Errorf("%s.query.maxResults must be >= 0", prefix)
	}
	if err := validateVectorOverrideConfig(cfg.Vector, prefix+".vector"); err != nil {
		return err
	}
	return nil
}

func validateMemorySources(sources []string, field string) error {
	if len(sources) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	for i, raw := range sources {
		source := strings.ToLower(strings.TrimSpace(raw))
		if source == "" {
			return fmt.Errorf("%s[%d] cannot be blank", field, i)
		}
		if source != "memory" {
			return fmt.Errorf("%s[%d] unsupported source %q (supported: memory)", field, i, raw)
		}
		if _, ok := seen[source]; ok {
			return fmt.Errorf("%s contains duplicate source %q", field, source)
		}
		seen[source] = struct{}{}
	}
	return nil
}

func validateVectorConfig(cfg VectorConfig, prefix string) error {
	if strings.TrimSpace(cfg.Provider) != "llama.cpp" {
		return fmt.Errorf("%s.provider must be llama.cpp", prefix)
	}
	mode := strings.ToLower(strings.TrimSpace(cfg.SearchMode))
	if mode != "hybrid" && mode != "keyword" && mode != "vector" {
		return fmt.Errorf("%s.searchMode must be hybrid, keyword, or vector", prefix)
	}
	if strings.TrimSpace(cfg.Model.ID) == "" {
		return fmt.Errorf("%s.model.id is required", prefix)
	}
	if strings.TrimSpace(cfg.Model.Path) == "" {
		return fmt.Errorf("%s.model.path is required", prefix)
	}
	if strings.TrimSpace(cfg.Model.PrimaryURL) == "" {
		return fmt.Errorf("%s.model.primaryUrl is required", prefix)
	}
	if strings.TrimSpace(cfg.Model.MirrorURL) == "" {
		return fmt.Errorf("%s.model.mirrorUrl is required", prefix)
	}
	if strings.TrimSpace(cfg.Model.SHA256) == "" {
		return fmt.Errorf("%s.model.sha256 is required", prefix)
	}
	if strings.TrimSpace(cfg.Server.Binary) == "" {
		return fmt.Errorf("%s.server.binary is required", prefix)
	}
	if strings.TrimSpace(cfg.Server.Host) == "" {
		return fmt.Errorf("%s.server.host is required", prefix)
	}
	if cfg.Server.Port < 0 || cfg.Server.Port > 65535 {
		return fmt.Errorf("%s.server.port must be between 0 and 65535", prefix)
	}
	if cfg.Server.StartupTimeoutSeconds <= 0 {
		return fmt.Errorf("%s.server.startupTimeoutSeconds must be > 0", prefix)
	}
	return nil
}

func validateVectorOverrideConfig(cfg VectorOverrideConfig, prefix string) error {
	if strings.TrimSpace(cfg.Provider) != "" && strings.TrimSpace(cfg.Provider) != "llama.cpp" {
		return fmt.Errorf("%s.provider must be llama.cpp", prefix)
	}
	mode := strings.ToLower(strings.TrimSpace(cfg.SearchMode))
	if mode != "" && mode != "hybrid" && mode != "keyword" && mode != "vector" {
		return fmt.Errorf("%s.searchMode must be hybrid, keyword, or vector", prefix)
	}
	if cfg.Server.Port < 0 || cfg.Server.Port > 65535 {
		return fmt.Errorf("%s.server.port must be between 0 and 65535", prefix)
	}
	if cfg.Server.StartupTimeoutSeconds < 0 {
		return fmt.Errorf("%s.server.startupTimeoutSeconds must be >= 0", prefix)
	}
	return nil
}
