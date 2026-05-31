package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigIsMemoryWorkspaceOnly(t *testing.T) {
	cfg := Default()
	if cfg.App.Name != "localclaw" {
		t.Fatalf("unexpected app name %q", cfg.App.Name)
	}
	if cfg.Agents.Defaults.Workspace != "." {
		t.Fatalf("unexpected default workspace %q", cfg.Agents.Defaults.Workspace)
	}
	if !cfg.Agents.Defaults.Memory.Tools.Create {
		t.Fatalf("expected memory create tool enabled by default")
	}
	if !cfg.Agents.Defaults.Memory.Vector.Enabled {
		t.Fatalf("expected vector memory enabled by default")
	}
	if cfg.Agents.Defaults.Memory.Vector.Model.SHA256 != "b5ce9d77a3fc4b3b39ccb5643c36777911cc4eb46a66962eadfa3f5f60490d63" {
		t.Fatalf("unexpected vector model sha")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should validate: %v", err)
	}
}

func TestLoadRejectsRemovedLLMSection(t *testing.T) {
	path := writeConfig(t, `{
  "app": {"name":"localclaw","root":"~/.localclaw"},
  "llm": {"provider":"codex"},
  "agents": {
    "defaults": {
      "workspace": ".",
      "memory": {
        "enabled": true,
        "tools": {"create": true, "get": true, "search": true, "grep": true},
        "sources": ["memory"],
        "extraPaths": [],
        "store": {"path": "~/.localclaw/memory/{agentId}.sqlite"},
        "chunking": {"tokens": 400, "overlap": 40},
        "query": {"maxResults": 8},
        "sync": {"onSearch": false}
      }
    },
    "list": []
  }
}`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestValidateRejectsSessionMemorySource(t *testing.T) {
	cfg := Default()
	cfg.Agents.Defaults.Memory.Sources = []string{"sessions"}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "unsupported source") {
		t.Fatalf("expected unsupported source error, got %v", err)
	}
}

func TestValidateAgentMemoryOverride(t *testing.T) {
	cfg := Default()
	cfg.Agents.List = []AgentConfig{{
		ID: "writer",
		Memory: MemoryOverrideConfig{
			Query: QueryConfig{MaxResults: 3},
			Vector: VectorOverrideConfig{
				Enabled: boolPtr(false),
				Model:   VectorModelConfig{Path: "~/custom/model.gguf"},
			},
		},
	}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected override to validate: %v", err)
	}
}

func TestLoadRejectsUnknownVectorField(t *testing.T) {
	path := writeConfig(t, `{
  "app": {"name":"localclaw","root":"~/.localclaw"},
  "agents": {
    "defaults": {
      "workspace": ".",
      "memory": {
        "enabled": true,
        "tools": {"create": true, "get": true, "search": true, "grep": true},
        "sources": ["memory"],
        "extraPaths": [],
        "store": {"path": "~/.localclaw/memory/{agentId}.sqlite"},
        "chunking": {"tokens": 400, "overlap": 40},
        "query": {"maxResults": 8},
        "sync": {"onSearch": false},
        "vector": {
          "enabled": true,
          "provider": "llama.cpp",
          "searchMode": "hybrid",
          "model": {"id": "m", "path": "m.gguf", "primaryUrl": "https://example.invalid/a", "mirrorUrl": "https://example.invalid/b", "sha256": "abc"},
          "server": {"managed": true, "binary": "llama-server", "host": "127.0.0.1", "port": 0, "startupTimeoutSeconds": 20},
          "unexpected": true
        }
      }
    },
    "list": []
  }
}`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func boolPtr(v bool) *bool {
	return &v
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "localclaw.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
