package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/dgriffin831/localclaw/internal/memory"
)

type stubMemoryBackend struct {
	searchFn func(ctx context.Context, req MemorySearchRequest) ([]memory.SearchResult, error)
	getFn    func(ctx context.Context, req MemoryGetRequest) (memory.GetResult, error)
}

func (s stubMemoryBackend) Search(ctx context.Context, req MemorySearchRequest) ([]memory.SearchResult, error) {
	return s.searchFn(ctx, req)
}

func (s stubMemoryBackend) Get(ctx context.Context, req MemoryGetRequest) (memory.GetResult, error) {
	return s.getFn(ctx, req)
}

func TestMemorySearchToolRejectsInvalidArgs(t *testing.T) {
	h := NewMemorySearchTool(stubMemoryBackend{
		searchFn: func(ctx context.Context, req MemorySearchRequest) ([]memory.SearchResult, error) {
			return nil, nil
		},
	})

	res := h.Call(context.Background(), map[string]interface{}{"query": "   "})
	if !res.IsError {
		t.Fatalf("expected error result")
	}
	if got := res.StructuredContent["ok"]; got != false {
		t.Fatalf("expected ok=false, got %v", got)
	}
}

func TestMemorySearchToolReturnsNormalizedStorageError(t *testing.T) {
	h := NewMemorySearchTool(stubMemoryBackend{
		searchFn: func(ctx context.Context, req MemorySearchRequest) ([]memory.SearchResult, error) {
			return nil, errors.New("boom")
		},
	})

	res := h.Call(context.Background(), map[string]interface{}{"query": "needle"})
	if !res.IsError {
		t.Fatalf("expected error result")
	}
	if got := res.StructuredContent["ok"]; got != false {
		t.Fatalf("expected ok=false, got %v", got)
	}
	if _, ok := res.StructuredContent["error"].(string); !ok {
		t.Fatalf("expected string error payload")
	}
}

func TestMemoryGetToolRejectsMissingPath(t *testing.T) {
	h := NewMemoryGetTool(stubMemoryBackend{
		getFn: func(ctx context.Context, req MemoryGetRequest) (memory.GetResult, error) {
			return memory.GetResult{}, nil
		},
	})
	res := h.Call(context.Background(), map[string]interface{}{})
	if !res.IsError {
		t.Fatalf("expected error result")
	}
}
