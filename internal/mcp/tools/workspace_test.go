package tools

import (
	"context"
	"testing"

	"github.com/dgriffin831/localclaw/internal/runtime"
)

type stubWorkspaceBackend struct {
	statusFn func(ctx context.Context, req WorkspaceStatusRequest) (WorkspaceStatusResult, error)
	listFn   func(ctx context.Context, req WorkspaceStatusRequest) ([]runtime.MCPWorkspaceFile, error)
	readFn   func(ctx context.Context, req WorkspaceReadRequest) (runtime.MCPWorkspaceReadResult, error)
}

func (s stubWorkspaceBackend) Status(ctx context.Context, req WorkspaceStatusRequest) (WorkspaceStatusResult, error) {
	return s.statusFn(ctx, req)
}

func (s stubWorkspaceBackend) List(ctx context.Context, req WorkspaceStatusRequest) ([]runtime.MCPWorkspaceFile, error) {
	return s.listFn(ctx, req)
}

func (s stubWorkspaceBackend) Read(ctx context.Context, req WorkspaceReadRequest) (runtime.MCPWorkspaceReadResult, error) {
	return s.readFn(ctx, req)
}

func TestWorkspaceStatusToolSuccess(t *testing.T) {
	h := NewWorkspaceStatusTool(stubWorkspaceBackend{statusFn: func(ctx context.Context, req WorkspaceStatusRequest) (WorkspaceStatusResult, error) {
		return WorkspaceStatusResult{AgentID: "default", WorkspacePath: "/tmp/ws", Exists: true}, nil
	}})

	res := h.Call(context.Background(), map[string]interface{}{"agent_id": "default"})
	if res.IsError {
		t.Fatalf("expected success")
	}
	if res.StructuredContent["ok"] != true {
		t.Fatalf("expected ok=true")
	}
}

func TestWorkspaceListToolSuccess(t *testing.T) {
	h := NewWorkspaceListTool(stubWorkspaceBackend{listFn: func(ctx context.Context, req WorkspaceStatusRequest) ([]runtime.MCPWorkspaceFile, error) {
		return []runtime.MCPWorkspaceFile{{Path: "SOUL.md", Bytes: 12}}, nil
	}})

	res := h.Call(context.Background(), map[string]interface{}{})
	if res.IsError {
		t.Fatalf("expected success")
	}
	if res.StructuredContent["count"] != 1 {
		t.Fatalf("expected count=1, got %v", res.StructuredContent["count"])
	}
}

func TestWorkspaceReadToolRequiresPath(t *testing.T) {
	h := NewWorkspaceReadTool(stubWorkspaceBackend{readFn: func(ctx context.Context, req WorkspaceReadRequest) (runtime.MCPWorkspaceReadResult, error) {
		return runtime.MCPWorkspaceReadResult{}, nil
	}})
	res := h.Call(context.Background(), map[string]interface{}{})
	if !res.IsError {
		t.Fatalf("expected error")
	}
}
