package tools

import (
	"context"
	"fmt"

	"github.com/dgriffin831/localclaw/internal/mcp/protocol"
	"github.com/dgriffin831/localclaw/internal/runtime"
)

const (
	ToolLocalclawWorkspaceStatus = "localclaw_workspace_status"
)

type WorkspaceStatusRequest struct {
	AgentID string
}

type WorkspaceStatusResult struct {
	AgentID       string `json:"agentId"`
	WorkspacePath string `json:"workspacePath"`
	Exists        bool   `json:"exists"`
}

type WorkspaceBackend interface {
	Status(ctx context.Context, req WorkspaceStatusRequest) (WorkspaceStatusResult, error)
}

type WorkspaceStatusTool struct {
	backend WorkspaceBackend
}

func NewWorkspaceStatusTool(backend WorkspaceBackend) WorkspaceStatusTool {
	return WorkspaceStatusTool{backend: backend}
}

func WorkspaceStatusDefinition() protocol.Tool {
	return protocol.Tool{
		Name:        ToolLocalclawWorkspaceStatus,
		Description: "Return local workspace path and availability for an agent",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"agent_id": map[string]interface{}{"type": "string"},
			},
		},
	}
}

func (t WorkspaceStatusTool) Call(ctx context.Context, args map[string]interface{}) protocol.CallToolResult {
	agentID, err := optionalStringArg(args, "agent_id")
	if err != nil {
		return errorResult(err)
	}
	result, runErr := t.backend.Status(ctx, WorkspaceStatusRequest{AgentID: agentID})
	if runErr != nil {
		return errorResult(fmt.Errorf("workspace_status failed: %w", runErr))
	}
	return protocol.CallToolResult{StructuredContent: map[string]interface{}{"ok": true, "status": result}}
}

type RuntimeWorkspaceBackend struct {
	App *runtime.App
}

func (b RuntimeWorkspaceBackend) Status(ctx context.Context, req WorkspaceStatusRequest) (WorkspaceStatusResult, error) {
	status, err := b.App.MCPWorkspaceStatus(ctx, req.AgentID)
	if err != nil {
		return WorkspaceStatusResult{}, err
	}
	return WorkspaceStatusResult{AgentID: status.AgentID, WorkspacePath: status.WorkspacePath, Exists: status.Exists}, nil
}
