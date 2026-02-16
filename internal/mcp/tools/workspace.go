package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dgriffin831/localclaw/internal/mcp/protocol"
	"github.com/dgriffin831/localclaw/internal/runtime"
	"github.com/dgriffin831/localclaw/internal/workspace"
)

const (
	ToolLocalclawWorkspaceStatus           = "localclaw_workspace_status"
	ToolLocalclawWorkspaceBootstrapContext = "localclaw_workspace_bootstrap_context"
)

type WorkspaceStatusRequest struct {
	AgentID string
}

type WorkspaceStatusResult struct {
	AgentID       string `json:"agentId"`
	WorkspacePath string `json:"workspacePath"`
	Exists        bool   `json:"exists"`
}

type WorkspaceBootstrapContextRequest struct {
	AgentID   string
	SessionID string
	MaxBytes  int
}

type WorkspaceBootstrapContextResult struct {
	AgentID       string                    `json:"agentId"`
	WorkspacePath string                    `json:"workspacePath"`
	Files         []workspace.BootstrapFile `json:"files"`
}

type WorkspaceBackend interface {
	Status(ctx context.Context, req WorkspaceStatusRequest) (WorkspaceStatusResult, error)
	BootstrapContext(ctx context.Context, req WorkspaceBootstrapContextRequest) (WorkspaceBootstrapContextResult, error)
}

type WorkspaceStatusTool struct {
	backend WorkspaceBackend
}

type WorkspaceBootstrapContextTool struct {
	backend WorkspaceBackend
}

func NewWorkspaceStatusTool(backend WorkspaceBackend) WorkspaceStatusTool {
	return WorkspaceStatusTool{backend: backend}
}

func NewWorkspaceBootstrapContextTool(backend WorkspaceBackend) WorkspaceBootstrapContextTool {
	return WorkspaceBootstrapContextTool{backend: backend}
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

func WorkspaceBootstrapContextDefinition() protocol.Tool {
	return protocol.Tool{
		Name:        ToolLocalclawWorkspaceBootstrapContext,
		Description: "Load workspace bootstrap context files bounded to the agent workspace",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"agent_id":   map[string]interface{}{"type": "string"},
				"session_id": map[string]interface{}{"type": "string"},
				"max_bytes":  map[string]interface{}{"type": "integer"},
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

func (t WorkspaceBootstrapContextTool) Call(ctx context.Context, args map[string]interface{}) protocol.CallToolResult {
	agentID, err := optionalStringArg(args, "agent_id")
	if err != nil {
		return errorResult(err)
	}
	sessionID, err := optionalStringArg(args, "session_id")
	if err != nil {
		return errorResult(err)
	}
	maxBytes, err := optionalIntArg(args, "max_bytes")
	if err != nil {
		return errorResult(err)
	}
	if maxBytes <= 0 {
		maxBytes = 64 * 1024
	}
	if maxBytes > 256*1024 {
		return errorResult(fmt.Errorf("max_bytes cannot exceed 262144"))
	}

	result, runErr := t.backend.BootstrapContext(ctx, WorkspaceBootstrapContextRequest{AgentID: agentID, SessionID: sessionID, MaxBytes: maxBytes})
	if runErr != nil {
		return errorResult(fmt.Errorf("workspace_bootstrap_context failed: %w", runErr))
	}
	root := filepath.Clean(result.WorkspacePath)
	total := 0
	filtered := make([]workspace.BootstrapFile, 0, len(result.Files))
	for _, file := range result.Files {
		if file.Missing {
			continue
		}
		if strings.TrimSpace(file.Path) == "" {
			continue
		}
		if !pathWithinRoot(root, file.Path) {
			return errorResult(fmt.Errorf("bootstrap file %q is outside workspace boundary", file.Name))
		}
		content := file.Content
		remaining := maxBytes - total
		if remaining <= 0 {
			break
		}
		if len(content) > remaining {
			content = content[:remaining]
		}
		total += len(content)
		file.Content = content
		filtered = append(filtered, file)
	}
	return protocol.CallToolResult{StructuredContent: map[string]interface{}{"ok": true, "workspacePath": result.WorkspacePath, "files": filtered, "bytes": total}}
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

func (b RuntimeWorkspaceBackend) BootstrapContext(ctx context.Context, req WorkspaceBootstrapContextRequest) (WorkspaceBootstrapContextResult, error) {
	workspacePath, files, err := b.App.MCPWorkspaceBootstrapContext(ctx, req.AgentID, req.SessionID)
	if err != nil {
		return WorkspaceBootstrapContextResult{}, err
	}
	return WorkspaceBootstrapContextResult{AgentID: runtime.ResolveAgentID(req.AgentID), WorkspacePath: workspacePath, Files: files}, nil
}

func pathWithinRoot(root string, path string) bool {
	cleanRoot := filepath.Clean(strings.TrimSpace(root))
	cleanPath := filepath.Clean(strings.TrimSpace(path))
	if cleanRoot == "" || cleanPath == "" {
		return false
	}

	// Lexical check catches simple traversal attempts.
	if !pathWithinBase(cleanRoot, cleanPath) {
		return false
	}

	// Symlink-aware check prevents links inside workspace from escaping root.
	resolvedRoot, rootErr := filepath.EvalSymlinks(cleanRoot)
	resolvedPath, pathErr := filepath.EvalSymlinks(cleanPath)
	if rootErr == nil && pathErr == nil {
		return pathWithinBase(resolvedRoot, resolvedPath)
	}
	if errorsIsNotExist(rootErr) || errorsIsNotExist(pathErr) {
		// Missing paths are handled by callers; keep lexical decision.
		return true
	}
	if rootErr == nil && pathErr != nil {
		// If file path cannot be resolved for any non-not-exist reason, fail closed.
		return false
	}
	if rootErr != nil && pathErr == nil {
		return pathWithinBase(cleanRoot, resolvedPath)
	}
	return false
}

func pathWithinBase(base string, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return !strings.HasPrefix(rel, "..") && !strings.HasPrefix(filepath.ToSlash(rel), "../")
}

func errorsIsNotExist(err error) bool {
	return err != nil && os.IsNotExist(err)
}
