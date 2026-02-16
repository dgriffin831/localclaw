package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/dgriffin831/localclaw/internal/mcp/protocol"
	"github.com/dgriffin831/localclaw/internal/runtime"
	"github.com/dgriffin831/localclaw/internal/session"
)

const (
	ToolLocalclawSessionsList    = "localclaw_sessions_list"
	ToolLocalclawSessionsHistory = "localclaw_sessions_history"
	ToolLocalclawSessionsSend    = "localclaw_sessions_send"
	ToolLocalclawSessionStatus   = "localclaw_session_status"
)

var ErrSessionNotFound = errors.New("session not found")

type SessionsListRequest struct {
	AgentID string
	Limit   int
	Offset  int
}

type SessionsListResult struct {
	Sessions []session.SessionEntry `json:"sessions"`
	Total    int                    `json:"total"`
}

type SessionsHistoryRequest struct {
	AgentID   string
	SessionID string
	Limit     int
	Offset    int
}

type SessionHistoryItem struct {
	Role      string `json:"role,omitempty"`
	Content   string `json:"content"`
	CreatedAt string `json:"createdAt,omitempty"`
}

type SessionsHistoryResult struct {
	Items []SessionHistoryItem `json:"items"`
	Total int                  `json:"total"`
}

type SessionsSendRequest struct {
	AgentID   string
	SessionID string
	Input     string
}

type SessionsSendResult struct {
	Output string `json:"output"`
}

type SessionStatusRequest struct {
	AgentID   string
	SessionID string
}

type SessionStatusResult struct {
	Session session.SessionEntry `json:"session"`
}

type OrchestrationBackend interface {
	SessionsList(ctx context.Context, req SessionsListRequest) (SessionsListResult, error)
	SessionsHistory(ctx context.Context, req SessionsHistoryRequest) (SessionsHistoryResult, error)
	SessionsSend(ctx context.Context, req SessionsSendRequest) (SessionsSendResult, error)
	SessionStatus(ctx context.Context, req SessionStatusRequest) (SessionStatusResult, error)
}

type SessionsListTool struct{ backend OrchestrationBackend }
type SessionsHistoryTool struct{ backend OrchestrationBackend }
type SessionsSendTool struct{ backend OrchestrationBackend }
type SessionStatusTool struct{ backend OrchestrationBackend }

func NewSessionsListTool(backend OrchestrationBackend) SessionsListTool {
	return SessionsListTool{backend: backend}
}
func NewSessionsHistoryTool(backend OrchestrationBackend) SessionsHistoryTool {
	return SessionsHistoryTool{backend: backend}
}
func NewSessionsSendTool(backend OrchestrationBackend) SessionsSendTool {
	return SessionsSendTool{backend: backend}
}
func NewSessionStatusTool(backend OrchestrationBackend) SessionStatusTool {
	return SessionStatusTool{backend: backend}
}

func SessionsListDefinition() protocol.Tool {
	return protocol.Tool{
		Name:        ToolLocalclawSessionsList,
		Description: "List sessions for an agent with pagination",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"agent_id": map[string]interface{}{"type": "string"},
				"limit":    map[string]interface{}{"type": "integer"},
				"offset":   map[string]interface{}{"type": "integer"},
			},
		},
	}
}

func SessionsHistoryDefinition() protocol.Tool {
	return protocol.Tool{
		Name:        ToolLocalclawSessionsHistory,
		Description: "Read transcript history for a session",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"agent_id":   map[string]interface{}{"type": "string"},
				"session_id": map[string]interface{}{"type": "string"},
				"limit":      map[string]interface{}{"type": "integer"},
				"offset":     map[string]interface{}{"type": "integer"},
			},
			"required": []string{"session_id"},
		},
	}
}

func SessionsSendDefinition() protocol.Tool {
	return protocol.Tool{
		Name:        ToolLocalclawSessionsSend,
		Description: "Send a prompt to an existing session and return response",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"agent_id":   map[string]interface{}{"type": "string"},
				"session_id": map[string]interface{}{"type": "string"},
				"input":      map[string]interface{}{"type": "string"},
			},
			"required": []string{"session_id", "input"},
		},
	}
}

func SessionStatusDefinition() protocol.Tool {
	return protocol.Tool{
		Name:        ToolLocalclawSessionStatus,
		Description: "Get metadata for an existing session",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"agent_id":   map[string]interface{}{"type": "string"},
				"session_id": map[string]interface{}{"type": "string"},
			},
			"required": []string{"session_id"},
		},
	}
}

func (t SessionsListTool) Call(ctx context.Context, args map[string]interface{}) protocol.CallToolResult {
	agentID, err := optionalStringArg(args, "agent_id")
	if err != nil {
		return errorResult(err)
	}
	limit, err := optionalIntArg(args, "limit")
	if err != nil {
		return errorResult(err)
	}
	offset, err := optionalIntArg(args, "offset")
	if err != nil {
		return errorResult(err)
	}
	limit = normalizeBounded(limit, 20, 100)
	if offset < 0 {
		offset = 0
	}
	result, runErr := t.backend.SessionsList(ctx, SessionsListRequest{AgentID: agentID, Limit: limit, Offset: offset})
	if runErr != nil {
		return errorResult(fmt.Errorf("sessions_list failed: %w", runErr))
	}
	return protocol.CallToolResult{StructuredContent: map[string]interface{}{"ok": true, "sessions": result.Sessions, "total": result.Total, "limit": limit, "offset": offset}}
}

func (t SessionsHistoryTool) Call(ctx context.Context, args map[string]interface{}) protocol.CallToolResult {
	agentID, err := optionalStringArg(args, "agent_id")
	if err != nil {
		return errorResult(err)
	}
	sessionID, err := requiredStringArg(args, "session_id")
	if err != nil {
		return errorResult(err)
	}
	limit, err := optionalIntArg(args, "limit")
	if err != nil {
		return errorResult(err)
	}
	offset, err := optionalIntArg(args, "offset")
	if err != nil {
		return errorResult(err)
	}
	limit = normalizeBounded(limit, 50, 200)
	if offset < 0 {
		offset = 0
	}
	result, runErr := t.backend.SessionsHistory(ctx, SessionsHistoryRequest{AgentID: agentID, SessionID: sessionID, Limit: limit, Offset: offset})
	if runErr != nil {
		if errors.Is(runErr, ErrSessionNotFound) {
			return errorResult(runErr)
		}
		return errorResult(fmt.Errorf("sessions_history failed: %w", runErr))
	}
	return protocol.CallToolResult{StructuredContent: map[string]interface{}{"ok": true, "items": result.Items, "total": result.Total, "limit": limit, "offset": offset}}
}

func (t SessionsSendTool) Call(ctx context.Context, args map[string]interface{}) protocol.CallToolResult {
	agentID, err := optionalStringArg(args, "agent_id")
	if err != nil {
		return errorResult(err)
	}
	sessionID, err := requiredStringArg(args, "session_id")
	if err != nil {
		return errorResult(err)
	}
	input, err := requiredStringArg(args, "input")
	if err != nil {
		return errorResult(err)
	}
	if len(input) > 8192 {
		return errorResult(fmt.Errorf("input cannot exceed 8192 characters"))
	}
	result, runErr := t.backend.SessionsSend(ctx, SessionsSendRequest{AgentID: agentID, SessionID: sessionID, Input: input})
	if runErr != nil {
		if errors.Is(runErr, ErrSessionNotFound) {
			return errorResult(runErr)
		}
		return errorResult(fmt.Errorf("sessions_send failed: %w", runErr))
	}
	return protocol.CallToolResult{StructuredContent: map[string]interface{}{"ok": true, "output": result.Output}}
}

func (t SessionStatusTool) Call(ctx context.Context, args map[string]interface{}) protocol.CallToolResult {
	agentID, err := optionalStringArg(args, "agent_id")
	if err != nil {
		return errorResult(err)
	}
	sessionID, err := requiredStringArg(args, "session_id")
	if err != nil {
		return errorResult(err)
	}
	result, runErr := t.backend.SessionStatus(ctx, SessionStatusRequest{AgentID: agentID, SessionID: sessionID})
	if runErr != nil {
		if errors.Is(runErr, ErrSessionNotFound) {
			return errorResult(runErr)
		}
		return errorResult(fmt.Errorf("session_status failed: %w", runErr))
	}
	return protocol.CallToolResult{StructuredContent: map[string]interface{}{"ok": true, "session": result.Session}}
}

type RuntimeOrchestrationBackend struct {
	App *runtime.App
}

func (b RuntimeOrchestrationBackend) SessionsList(ctx context.Context, req SessionsListRequest) (SessionsListResult, error) {
	out, err := b.App.MCPSessionsList(ctx, req.AgentID, req.Limit, req.Offset)
	if err != nil {
		return SessionsListResult{}, err
	}
	return SessionsListResult{Sessions: out.Sessions, Total: out.Total}, nil
}

func (b RuntimeOrchestrationBackend) SessionsHistory(ctx context.Context, req SessionsHistoryRequest) (SessionsHistoryResult, error) {
	out, err := b.App.MCPSessionsHistory(ctx, req.AgentID, req.SessionID, req.Limit, req.Offset)
	if err != nil {
		if errors.Is(err, runtime.ErrMCPNotFound) {
			return SessionsHistoryResult{}, ErrSessionNotFound
		}
		return SessionsHistoryResult{}, err
	}
	items := make([]SessionHistoryItem, 0, len(out.Items))
	for _, item := range out.Items {
		items = append(items, SessionHistoryItem{Role: item.Role, Content: item.Content, CreatedAt: item.CreatedAt})
	}
	return SessionsHistoryResult{Items: items, Total: out.Total}, nil
}

func (b RuntimeOrchestrationBackend) SessionsSend(ctx context.Context, req SessionsSendRequest) (SessionsSendResult, error) {
	output, err := b.App.MCPSessionsSend(ctx, req.AgentID, req.SessionID, req.Input)
	if err != nil {
		if errors.Is(err, runtime.ErrMCPNotFound) {
			return SessionsSendResult{}, ErrSessionNotFound
		}
		return SessionsSendResult{}, err
	}
	return SessionsSendResult{Output: output}, nil
}

func (b RuntimeOrchestrationBackend) SessionStatus(ctx context.Context, req SessionStatusRequest) (SessionStatusResult, error) {
	entry, err := b.App.MCPSessionStatus(ctx, req.AgentID, req.SessionID)
	if err != nil {
		if errors.Is(err, runtime.ErrMCPNotFound) {
			return SessionStatusResult{}, ErrSessionNotFound
		}
		return SessionStatusResult{}, err
	}
	return SessionStatusResult{Session: entry}, nil
}

func normalizeBounded(value, defaultValue, maxValue int) int {
	if value <= 0 {
		return defaultValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}
