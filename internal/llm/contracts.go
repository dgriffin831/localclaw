package llm

import (
	"context"
	"fmt"
	"strings"
)

type StreamEventType string

const (
	StreamEventDelta      StreamEventType = "delta"
	StreamEventFinal      StreamEventType = "final"
	StreamEventToolCall   StreamEventType = "tool_call"
	StreamEventToolResult StreamEventType = "tool_result"
	// StreamEventProviderMetadata carries provider-native tool/model metadata.
	StreamEventProviderMetadata StreamEventType = "provider_metadata"
)

type ToolClass string

const (
	ToolClassUnspecified ToolClass = ""
	// ToolClassDelegated indicates the provider executed the tool call.
	// localclaw tools are exposed to providers through MCP and execute on the
	// provider side; runtime does not host a separate tool execution loop.
	ToolClassDelegated ToolClass = "delegated"
)

type ToolCall struct {
	ID    string
	Name  string
	Args  map[string]interface{}
	Class ToolClass
}

type ToolResult struct {
	CallID string                 `json:"call_id,omitempty"`
	Tool   string                 `json:"tool"`
	Class  ToolClass              `json:"class,omitempty"`
	OK     bool                   `json:"ok"`
	Data   map[string]interface{} `json:"data,omitempty"`
	Error  string                 `json:"error,omitempty"`
	Status string                 `json:"status,omitempty"`
}

type ProviderMetadata struct {
	Provider  string   `json:"provider,omitempty"`
	Model     string   `json:"model,omitempty"`
	Tools     []string `json:"tools,omitempty"`
	SessionID string   `json:"session_id,omitempty"`
}

type ReasoningMetadata struct {
	Supported bool     `json:"supported,omitempty"`
	Levels    []string `json:"levels,omitempty"`
	Default   string   `json:"default,omitempty"`
}

type ProviderModelDescriptor struct {
	Name      string            `json:"name"`
	Reasoning ReasoningMetadata `json:"reasoning,omitempty"`
}

type ProviderModelCatalog struct {
	Provider string                    `json:"provider"`
	Models   []ProviderModelDescriptor `json:"models,omitempty"`
	Partial  bool                      `json:"partial,omitempty"`
}

type StreamEvent struct {
	Type             StreamEventType
	Text             string
	ToolCall         *ToolCall
	ToolResult       *ToolResult
	ProviderMetadata *ProviderMetadata
}

type ToolParameter struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description,omitempty"`
}

type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Class       ToolClass       `json:"class,omitempty"`
	Parameters  []ToolParameter `json:"parameters,omitempty"`
}

type SessionMetadata struct {
	AgentID           string `json:"agent_id,omitempty"`
	SessionID         string `json:"session_id,omitempty"`
	SessionKey        string `json:"session_key,omitempty"`
	Provider          string `json:"provider,omitempty"`
	ProviderSessionID string `json:"provider_session_id,omitempty"`
}

type PromptOptions struct {
	ProviderOverride  string `json:"provider_override,omitempty"`
	ModelOverride     string `json:"model_override,omitempty"`
	ReasoningOverride string `json:"reasoning_override,omitempty"`
}

type Request struct {
	Input           string           `json:"input"`
	SystemContext   string           `json:"system_context,omitempty"`
	SkillPrompt     string           `json:"skill_prompt,omitempty"`
	ToolDefinitions []ToolDefinition `json:"tool_definitions,omitempty"`
	Session         SessionMetadata  `json:"session,omitempty"`
	Options         PromptOptions    `json:"options,omitempty"`
}

type Capabilities struct {
	SupportsRequestOptions bool
}

// Client is the provider-agnostic baseline runtime contract.
type Client interface {
	Prompt(ctx context.Context, input string) (string, error)
	PromptStream(ctx context.Context, input string) (<-chan StreamEvent, <-chan error)
	Capabilities() Capabilities
}

// RequestClient adds structured request support while preserving Prompt compatibility.
type RequestClient interface {
	PromptRequest(ctx context.Context, req Request) (string, error)
	PromptStreamRequest(ctx context.Context, req Request) (<-chan StreamEvent, <-chan error)
}

// ModelCatalogClient adds provider-native model discovery support.
type ModelCatalogClient interface {
	DiscoverModelCatalog(ctx context.Context) (ProviderModelCatalog, error)
}

// NOTE: ComposePromptFallback is intentionally retained as the central prompt composition fallback for now.
func ComposePromptFallback(req Request) string {
	trimmedInput := strings.TrimSpace(req.Input)
	system := strings.TrimSpace(req.SystemContext)
	skill := strings.TrimSpace(req.SkillPrompt)

	if system == "" && skill == "" {
		return trimmedInput
	}

	sections := make([]string, 0, 4)
	if system != "" {
		sections = append(sections, system)
	}
	if skill != "" {
		sections = append(sections, skill)
	}
	if trimmedInput != "" {
		sections = append(sections, fmt.Sprintf("User input:\n%s", trimmedInput))
	}
	return strings.Join(sections, "\n\n")
}
