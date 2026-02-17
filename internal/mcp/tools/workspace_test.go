package tools

import (
	"context"
	"testing"
)

type stubWorkspaceBackend struct {
	statusFn func(ctx context.Context, req WorkspaceStatusRequest) (WorkspaceStatusResult, error)
}

func (s stubWorkspaceBackend) Status(ctx context.Context, req WorkspaceStatusRequest) (WorkspaceStatusResult, error) {
	return s.statusFn(ctx, req)
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
