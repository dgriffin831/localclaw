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
	ToolClassLocal       ToolClass = "local"
	ToolClassDelegated   ToolClass = "delegated"
)

type ToolResultResponder func(ctx context.Context, result ToolResult) error

type ToolCall struct {
	ID      string
	Name    string
	Args    map[string]interface{}
	Class   ToolClass
	Respond ToolResultResponder
}

type ToolResult struct {
	CallID string                 `json:"call_id,omitempty"`
	Tool   string                 `json:"tool"`
	OK     bool                   `json:"ok"`
	Data   map[string]interface{} `json:"data,omitempty"`
	Error  string                 `json:"error,omitempty"`
	Status string                 `json:"status,omitempty"`
}

type ProviderMetadata struct {
	Provider string   `json:"provider,omitempty"`
	Model    string   `json:"model,omitempty"`
	Tools    []string `json:"tools,omitempty"`
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
	AgentID    string `json:"agent_id,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	SessionKey string `json:"session_key,omitempty"`
}

type Request struct {
	Input           string           `json:"input"`
	SystemContext   string           `json:"system_context,omitempty"`
	SkillPrompt     string           `json:"skill_prompt,omitempty"`
	ToolDefinitions []ToolDefinition `json:"tool_definitions,omitempty"`
	Session         SessionMetadata  `json:"session,omitempty"`
}

type Capabilities struct {
	SupportsRequestOptions bool
	StructuredToolCalls    bool
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
