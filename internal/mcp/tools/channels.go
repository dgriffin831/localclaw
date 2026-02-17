package tools

import (
	"context"
	"fmt"

	"github.com/dgriffin831/localclaw/internal/mcp/protocol"
	"github.com/dgriffin831/localclaw/internal/runtime"
)

const (
	ToolLocalclawSlackSend  = "localclaw_slack_send"
	ToolLocalclawSignalSend = "localclaw_signal_send"
)

type SlackSendRequest struct {
	Text      string
	Channel   string
	ThreadID  string
	AgentID   string
	SessionID string
}

type SlackSendResult struct {
	OK        bool   `json:"ok"`
	Channel   string `json:"channel"`
	MessageID string `json:"message_id"`
	ThreadID  string `json:"thread_id,omitempty"`
}

type SignalSendRequest struct {
	Text      string
	Recipient string
	AgentID   string
	SessionID string
}

type SignalSendResult struct {
	OK        bool   `json:"ok"`
	Recipient string `json:"recipient"`
	SentAt    string `json:"sent_at"`
}

type ChannelsBackend interface {
	SlackSend(ctx context.Context, req SlackSendRequest) (SlackSendResult, error)
	SignalSend(ctx context.Context, req SignalSendRequest) (SignalSendResult, error)
}

type SlackSendTool struct{ backend ChannelsBackend }
type SignalSendTool struct{ backend ChannelsBackend }

func NewSlackSendTool(backend ChannelsBackend) SlackSendTool {
	return SlackSendTool{backend: backend}
}

func NewSignalSendTool(backend ChannelsBackend) SignalSendTool {
	return SignalSendTool{backend: backend}
}

func SlackSendDefinition() protocol.Tool {
	return protocol.Tool{
		Name:        ToolLocalclawSlackSend,
		Description: "Send a message to Slack via configured local channel adapter",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"text":       map[string]interface{}{"type": "string"},
				"channel":    map[string]interface{}{"type": "string"},
				"thread_id":  map[string]interface{}{"type": "string"},
				"agent_id":   map[string]interface{}{"type": "string"},
				"session_id": map[string]interface{}{"type": "string"},
			},
			"required": []string{"text"},
		},
	}
}

func SignalSendDefinition() protocol.Tool {
	return protocol.Tool{
		Name:        ToolLocalclawSignalSend,
		Description: "Send a message to Signal via configured local signal-cli adapter",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"text":       map[string]interface{}{"type": "string"},
				"recipient":  map[string]interface{}{"type": "string"},
				"agent_id":   map[string]interface{}{"type": "string"},
				"session_id": map[string]interface{}{"type": "string"},
			},
			"required": []string{"text"},
		},
	}
}

func (t SlackSendTool) Call(ctx context.Context, args map[string]interface{}) protocol.CallToolResult {
	text, err := requiredStringArg(args, "text")
	if err != nil {
		return errorResult(err)
	}
	channel, err := optionalStringArg(args, "channel")
	if err != nil {
		return errorResult(err)
	}
	threadID, err := optionalStringArg(args, "thread_id")
	if err != nil {
		return errorResult(err)
	}
	agentID, err := optionalStringArg(args, "agent_id")
	if err != nil {
		return errorResult(err)
	}
	sessionID, err := optionalStringArg(args, "session_id")
	if err != nil {
		return errorResult(err)
	}

	result, runErr := t.backend.SlackSend(ctx, SlackSendRequest{
		Text:      text,
		Channel:   channel,
		ThreadID:  threadID,
		AgentID:   agentID,
		SessionID: sessionID,
	})
	if runErr != nil {
		return errorResult(fmt.Errorf("slack_send failed: %w", runErr))
	}
	return protocol.CallToolResult{
		StructuredContent: map[string]interface{}{
			"ok":         result.OK,
			"channel":    result.Channel,
			"message_id": result.MessageID,
			"thread_id":  result.ThreadID,
		},
	}
}

func (t SignalSendTool) Call(ctx context.Context, args map[string]interface{}) protocol.CallToolResult {
	text, err := requiredStringArg(args, "text")
	if err != nil {
		return errorResult(err)
	}
	recipient, err := optionalStringArg(args, "recipient")
	if err != nil {
		return errorResult(err)
	}
	agentID, err := optionalStringArg(args, "agent_id")
	if err != nil {
		return errorResult(err)
	}
	sessionID, err := optionalStringArg(args, "session_id")
	if err != nil {
		return errorResult(err)
	}

	result, runErr := t.backend.SignalSend(ctx, SignalSendRequest{
		Text:      text,
		Recipient: recipient,
		AgentID:   agentID,
		SessionID: sessionID,
	})
	if runErr != nil {
		return errorResult(fmt.Errorf("signal_send failed: %w", runErr))
	}
	return protocol.CallToolResult{
		StructuredContent: map[string]interface{}{
			"ok":        result.OK,
			"recipient": result.Recipient,
			"sent_at":   result.SentAt,
		},
	}
}

type RuntimeChannelsBackend struct {
	App *runtime.App
}

func (b RuntimeChannelsBackend) SlackSend(ctx context.Context, req SlackSendRequest) (SlackSendResult, error) {
	result, err := b.App.MCPSlackSend(ctx, req.Text, req.Channel, req.ThreadID, req.AgentID, req.SessionID)
	if err != nil {
		return SlackSendResult{}, err
	}
	return SlackSendResult{
		OK:        result.OK,
		Channel:   result.Channel,
		MessageID: result.MessageID,
		ThreadID:  result.ThreadID,
	}, nil
}

func (b RuntimeChannelsBackend) SignalSend(ctx context.Context, req SignalSendRequest) (SignalSendResult, error) {
	result, err := b.App.MCPSignalSend(ctx, req.Text, req.Recipient, req.AgentID, req.SessionID)
	if err != nil {
		return SignalSendResult{}, err
	}
	return SignalSendResult{
		OK:        result.OK,
		Recipient: result.Recipient,
		SentAt:    result.SentAt,
	}, nil
}
