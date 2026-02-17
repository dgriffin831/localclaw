package tools

import (
	"strings"
	"testing"

	"github.com/dgriffin831/localclaw/internal/mcp/protocol"
)

func TestToolDefinitionsIncludeWhenToUseGuidance(t *testing.T) {
	for _, def := range exposedToolDefinitions() {
		desc := strings.TrimSpace(def.Description)
		if desc == "" {
			t.Fatalf("tool %q is missing description", def.Name)
		}
		if !strings.Contains(strings.ToLower(desc), "use when") {
			t.Fatalf("tool %q description must include \"use when\" guidance, got %q", def.Name, def.Description)
		}
	}
}

func TestToolDefinitionInputFieldsIncludeDescriptionsAndExamples(t *testing.T) {
	for _, def := range exposedToolDefinitions() {
		propsRaw, ok := def.InputSchema["properties"]
		if !ok {
			continue
		}
		props, ok := propsRaw.(map[string]interface{})
		if !ok {
			t.Fatalf("tool %q has invalid properties type %T", def.Name, propsRaw)
		}
		for field, raw := range props {
			fieldDef, ok := raw.(map[string]interface{})
			if !ok {
				t.Fatalf("tool %q field %q has invalid type %T", def.Name, field, raw)
			}
			desc, _ := fieldDef["description"].(string)
			if strings.TrimSpace(desc) == "" {
				t.Fatalf("tool %q field %q is missing description", def.Name, field)
			}
			examplesRaw, ok := fieldDef["examples"]
			if !ok {
				t.Fatalf("tool %q field %q is missing examples", def.Name, field)
			}
			switch examples := examplesRaw.(type) {
			case []interface{}:
				if len(examples) == 0 {
					t.Fatalf("tool %q field %q must include at least one example", def.Name, field)
				}
			case []string:
				if len(examples) == 0 {
					t.Fatalf("tool %q field %q must include at least one example", def.Name, field)
				}
			default:
				t.Fatalf("tool %q field %q has invalid examples type %T", def.Name, field, examplesRaw)
			}
		}
	}
}

func exposedToolDefinitions() []protocol.Tool {
	return []protocol.Tool{
		MemorySearchDefinition(),
		MemoryGetDefinition(),
		MemoryGrepDefinition(),
		WorkspaceStatusDefinition(),
		CronListDefinition(),
		CronAddDefinition(),
		CronRemoveDefinition(),
		CronRunDefinition(),
		SessionsListDefinition(),
		SessionsHistoryDefinition(),
		SessionsDeleteDefinition(),
		SessionStatusDefinition(),
		SlackSendDefinition(),
		SignalSendDefinition(),
	}
}
