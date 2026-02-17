package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/dgriffin831/localclaw/internal/session"
)

type stubOrchestrationBackend struct {
	listFn    func(ctx context.Context, req SessionsListRequest) (SessionsListResult, error)
	historyFn func(ctx context.Context, req SessionsHistoryRequest) (SessionsHistoryResult, error)
	deleteFn  func(ctx context.Context, req SessionsDeleteRequest) (SessionsDeleteResult, error)
	statusFn  func(ctx context.Context, req SessionStatusRequest) (SessionStatusResult, error)
}

func (s stubOrchestrationBackend) SessionsList(ctx context.Context, req SessionsListRequest) (SessionsListResult, error) {
	return s.listFn(ctx, req)
}

func (s stubOrchestrationBackend) SessionsHistory(ctx context.Context, req SessionsHistoryRequest) (SessionsHistoryResult, error) {
	return s.historyFn(ctx, req)
}

func (s stubOrchestrationBackend) SessionsDelete(ctx context.Context, req SessionsDeleteRequest) (SessionsDeleteResult, error) {
	return s.deleteFn(ctx, req)
}

func (s stubOrchestrationBackend) SessionStatus(ctx context.Context, req SessionStatusRequest) (SessionStatusResult, error) {
	return s.statusFn(ctx, req)
}

func TestSessionsListToolAppliesPaginationDefaults(t *testing.T) {
	h := NewSessionsListTool(stubOrchestrationBackend{listFn: func(ctx context.Context, req SessionsListRequest) (SessionsListResult, error) {
		if req.Limit != 20 || req.Offset != 0 {
			t.Fatalf("unexpected defaults: %+v", req)
		}
		return SessionsListResult{Sessions: []session.SessionEntry{}, Total: 0}, nil
	}})

	res := h.Call(context.Background(), map[string]interface{}{})
	if res.IsError {
		t.Fatalf("expected success")
	}
}

func TestSessionStatusToolRequiresSessionID(t *testing.T) {
	h := NewSessionStatusTool(stubOrchestrationBackend{statusFn: func(ctx context.Context, req SessionStatusRequest) (SessionStatusResult, error) {
		return SessionStatusResult{}, nil
	}})

	res := h.Call(context.Background(), map[string]interface{}{})
	if !res.IsError {
		t.Fatalf("expected validation error")
	}
}

func TestSessionsHistoryToolMapsBackendErrors(t *testing.T) {
	h := NewSessionsHistoryTool(stubOrchestrationBackend{historyFn: func(ctx context.Context, req SessionsHistoryRequest) (SessionsHistoryResult, error) {
		return SessionsHistoryResult{}, errors.New("boom")
	}})

	res := h.Call(context.Background(), map[string]interface{}{"session_id": "main"})
	if !res.IsError {
		t.Fatalf("expected error")
	}
}

func TestSessionsHistoryToolCapsLimitAndOffset(t *testing.T) {
	h := NewSessionsHistoryTool(stubOrchestrationBackend{historyFn: func(ctx context.Context, req SessionsHistoryRequest) (SessionsHistoryResult, error) {
		if req.Limit != 200 {
			t.Fatalf("expected capped limit=200, got %d", req.Limit)
		}
		if req.Offset != 0 {
			t.Fatalf("expected normalized offset=0, got %d", req.Offset)
		}
		return SessionsHistoryResult{}, nil
	}})

	res := h.Call(context.Background(), map[string]interface{}{"session_id": "main", "limit": 9999, "offset": -10})
	if res.IsError {
		t.Fatalf("expected success")
	}
}

func TestSessionsDeleteToolRequiresSessionID(t *testing.T) {
	h := NewSessionsDeleteTool(stubOrchestrationBackend{deleteFn: func(ctx context.Context, req SessionsDeleteRequest) (SessionsDeleteResult, error) {
		return SessionsDeleteResult{Deleted: true}, nil
	}})

	res := h.Call(context.Background(), map[string]interface{}{})
	if !res.IsError {
		t.Fatalf("expected validation error")
	}
}

func TestSessionsDeleteToolMapsNotFound(t *testing.T) {
	h := NewSessionsDeleteTool(stubOrchestrationBackend{deleteFn: func(ctx context.Context, req SessionsDeleteRequest) (SessionsDeleteResult, error) {
		return SessionsDeleteResult{}, ErrSessionNotFound
	}})

	res := h.Call(context.Background(), map[string]interface{}{"session_id": "main"})
	if !res.IsError {
		t.Fatalf("expected not found error")
	}
}
