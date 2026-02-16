package mcp

import (
	"testing"

	tooldefs "github.com/dgriffin831/localclaw/internal/mcp/tools"
)

func TestMemoryToolsIncludeLocalclawNames(t *testing.T) {
	defs := []string{
		tooldefs.MemorySearchDefinition().Name,
		tooldefs.MemoryGetDefinition().Name,
		tooldefs.MemoryGrepDefinition().Name,
	}
	got := map[string]bool{}
	for _, name := range defs {
		got[name] = true
	}
	if !got["localclaw_memory_search"] || !got["localclaw_memory_get"] || !got["localclaw_memory_grep"] {
		t.Fatalf("expected localclaw memory tool names, got %v", defs)
	}
	if got["memory_search"] || got["memory_get"] || got["memory_grep"] {
		t.Fatalf("did not expect legacy memory_* aliases, got %v", defs)
	}
}

func TestMemoryGrepToolSchemaIncludesContractFields(t *testing.T) {
	def := tooldefs.MemoryGrepDefinition()
	propsRaw, ok := def.InputSchema["properties"]
	if !ok {
		t.Fatalf("missing properties in schema")
	}
	props, ok := propsRaw.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected properties type %T", propsRaw)
	}
	requiredFields := []string{
		"query",
		"mode",
		"case_sensitive",
		"word",
		"max_matches",
		"context_lines",
		"path_glob",
		"source",
	}
	for _, field := range requiredFields {
		if _, ok := props[field]; !ok {
			t.Fatalf("missing field %q in memory_grep schema", field)
		}
	}
}
