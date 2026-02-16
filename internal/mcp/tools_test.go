package mcp

import (
	"testing"

	tooldefs "github.com/dgriffin831/localclaw/internal/mcp/tools"
)

func TestMemoryToolsIncludeGrepAndAlias(t *testing.T) {
	defs := []string{
		tooldefs.MemorySearchDefinition().Name,
		tooldefs.MemorySearchAliasDefinition().Name,
		tooldefs.MemoryGetDefinition().Name,
		tooldefs.MemoryGetAliasDefinition().Name,
		tooldefs.MemoryGrepDefinition().Name,
		tooldefs.MemoryGrepAliasDefinition().Name,
	}
	got := map[string]bool{}
	for _, name := range defs {
		got[name] = true
	}
	if !got["localclaw_memory_grep"] || !got["memory_grep"] {
		t.Fatalf("expected grep definitions and alias, got %v", defs)
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
