package tools

import (
	"context"
	"fmt"

	"github.com/dgriffin831/localclaw/internal/mcp/protocol"
	"github.com/dgriffin831/localclaw/internal/memory"
	"github.com/dgriffin831/localclaw/internal/runtime"
)

const (
	ToolLocalclawWorkspaceStatus = "localclaw_workspace_status"
	ToolLocalclawWorkspaceList   = "localclaw_workspace_list"
	ToolLocalclawWorkspaceRead   = "localclaw_workspace_read"
)

type WorkspaceStatusRequest struct {
	AgentID string
}

type WorkspaceReadRequest struct {
	AgentID  string
	Path     string
	FromLine int
	Lines    int
}

type WorkspaceStatusResult struct {
	AgentID       string `json:"agentId"`
	WorkspacePath string `json:"workspacePath"`
	Exists        bool   `json:"exists"`
}

type WorkspaceBackend interface {
	Status(ctx context.Context, req WorkspaceStatusRequest) (WorkspaceStatusResult, error)
	List(ctx context.Context, req WorkspaceStatusRequest) ([]runtime.MCPWorkspaceFile, error)
	Read(ctx context.Context, req WorkspaceReadRequest) (runtime.MCPWorkspaceReadResult, error)
}

type WorkspaceStatusTool struct {
	backend WorkspaceBackend
}

type WorkspaceListTool struct {
	backend WorkspaceBackend
}

type WorkspaceReadTool struct {
	backend WorkspaceBackend
}

func NewWorkspaceStatusTool(backend WorkspaceBackend) WorkspaceStatusTool {
	return WorkspaceStatusTool{backend: backend}
}

func NewWorkspaceListTool(backend WorkspaceBackend) WorkspaceListTool {
	return WorkspaceListTool{backend: backend}
}

func NewWorkspaceReadTool(backend WorkspaceBackend) WorkspaceReadTool {
	return WorkspaceReadTool{backend: backend}
}

func WorkspaceStatusDefinition() protocol.Tool {
	return protocol.Tool{
		Name:        ToolLocalclawWorkspaceStatus,
		Description: "Return resolved workspace path and availability for an agent. Use when you need to confirm filesystem routing before workspace or memory work.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"agent_id": schemaStringField("Optional agent ID to resolve workspace path; omit to use current/default agent.", "default"),
			},
		},
	}
}

func WorkspaceListDefinition() protocol.Tool {
	return protocol.Tool{
		Name:        ToolLocalclawWorkspaceList,
		Description: "List readable workspace control markdown and memory files for an agent. Use when discovering available behavior or memory files.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"agent_id": schemaStringField("Optional agent ID to resolve workspace path; omit to use current/default agent.", "default"),
			},
		},
	}
}

func WorkspaceReadDefinition() protocol.Tool {
	return protocol.Tool{
		Name:        ToolLocalclawWorkspaceRead,
		Description: "Read allowed workspace markdown files such as SOUL.md or memory/*.md. Use when you need exact workspace instructions or stored memory content.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":      schemaStringField("Workspace-relative markdown path.", "SOUL.md"),
				"from_line": schemaIntegerField("1-based starting line; omit to start at line 1.", 1),
				"lines":     schemaIntegerField("Number of lines to return from from_line; omit for the rest of file.", 40),
				"agent_id":  schemaStringField("Optional agent ID to resolve workspace path; omit to use current/default agent.", "default"),
			},
			"required": []string{"path"},
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

func (t WorkspaceListTool) Call(ctx context.Context, args map[string]interface{}) protocol.CallToolResult {
	agentID, err := optionalStringArg(args, "agent_id")
	if err != nil {
		return errorResult(err)
	}
	files, runErr := t.backend.List(ctx, WorkspaceStatusRequest{AgentID: agentID})
	if runErr != nil {
		return errorResult(fmt.Errorf("workspace_list failed: %w", runErr))
	}
	return protocol.CallToolResult{StructuredContent: map[string]interface{}{"ok": true, "files": files, "count": len(files)}}
}

func (t WorkspaceReadTool) Call(ctx context.Context, args map[string]interface{}) protocol.CallToolResult {
	path, err := requiredStringArg(args, "path")
	if err != nil {
		return errorResult(err)
	}
	fromLine, err := optionalIntArg(args, "from_line")
	if err != nil {
		return errorResult(err)
	}
	lines, err := optionalIntArg(args, "lines")
	if err != nil {
		return errorResult(err)
	}
	agentID, err := optionalStringArg(args, "agent_id")
	if err != nil {
		return errorResult(err)
	}
	result, runErr := t.backend.Read(ctx, WorkspaceReadRequest{
		AgentID:  agentID,
		Path:     path,
		FromLine: fromLine,
		Lines:    lines,
	})
	if runErr != nil {
		return errorResult(fmt.Errorf("workspace_read failed: %w", runErr))
	}
	return protocol.CallToolResult{StructuredContent: map[string]interface{}{"ok": true, "result": result}}
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

func (b RuntimeWorkspaceBackend) List(ctx context.Context, req WorkspaceStatusRequest) ([]runtime.MCPWorkspaceFile, error) {
	return b.App.MCPWorkspaceList(ctx, req.AgentID)
}

func (b RuntimeWorkspaceBackend) Read(ctx context.Context, req WorkspaceReadRequest) (runtime.MCPWorkspaceReadResult, error) {
	return b.App.MCPWorkspaceRead(ctx, req.AgentID, req.Path, memory.GetOptions{FromLine: req.FromLine, Lines: req.Lines})
}
