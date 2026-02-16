package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dgriffin831/localclaw/internal/workspace"
)

type stubWorkspaceBackend struct {
	statusFn  func(ctx context.Context, req WorkspaceStatusRequest) (WorkspaceStatusResult, error)
	contextFn func(ctx context.Context, req WorkspaceBootstrapContextRequest) (WorkspaceBootstrapContextResult, error)
}

func (s stubWorkspaceBackend) Status(ctx context.Context, req WorkspaceStatusRequest) (WorkspaceStatusResult, error) {
	return s.statusFn(ctx, req)
}

func (s stubWorkspaceBackend) BootstrapContext(ctx context.Context, req WorkspaceBootstrapContextRequest) (WorkspaceBootstrapContextResult, error) {
	return s.contextFn(ctx, req)
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

func TestWorkspaceBootstrapContextToolRejectsOutOfBoundaryFiles(t *testing.T) {
	h := NewWorkspaceBootstrapContextTool(stubWorkspaceBackend{contextFn: func(ctx context.Context, req WorkspaceBootstrapContextRequest) (WorkspaceBootstrapContextResult, error) {
		return WorkspaceBootstrapContextResult{
			AgentID:       "default",
			WorkspacePath: "/tmp/ws",
			Files:         []workspace.BootstrapFile{{Name: "AGENTS.md", Path: "/etc/passwd", Content: "x"}},
		}, nil
	}})

	res := h.Call(context.Background(), map[string]interface{}{})
	if !res.IsError {
		t.Fatalf("expected boundary error")
	}
	if !strings.Contains(res.StructuredContent["error"].(string), "outside workspace") {
		t.Fatalf("unexpected error: %v", res.StructuredContent["error"])
	}
}

func TestWorkspaceBootstrapContextToolMapsBackendErrors(t *testing.T) {
	h := NewWorkspaceBootstrapContextTool(stubWorkspaceBackend{contextFn: func(ctx context.Context, req WorkspaceBootstrapContextRequest) (WorkspaceBootstrapContextResult, error) {
		return WorkspaceBootstrapContextResult{}, errors.New("boom")
	}})

	res := h.Call(context.Background(), map[string]interface{}{})
	if !res.IsError {
		t.Fatalf("expected error")
	}
}

func TestWorkspaceBootstrapContextToolRejectsSymlinkEscapes(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	linkPath := filepath.Join(root, "AGENTS.md")
	if err := os.Symlink(target, linkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	h := NewWorkspaceBootstrapContextTool(stubWorkspaceBackend{contextFn: func(ctx context.Context, req WorkspaceBootstrapContextRequest) (WorkspaceBootstrapContextResult, error) {
		return WorkspaceBootstrapContextResult{
			WorkspacePath: root,
			Files:         []workspace.BootstrapFile{{Name: "AGENTS.md", Path: linkPath, Content: "secret"}},
		}, nil
	}})

	res := h.Call(context.Background(), map[string]interface{}{})
	if !res.IsError {
		t.Fatalf("expected boundary error for symlink escape")
	}
}
