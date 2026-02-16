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
	grepFn   func(ctx context.Context, req MemoryGrepRequest) (memory.GrepResult, error)
}

func (s stubMemoryBackend) Search(ctx context.Context, req MemorySearchRequest) ([]memory.SearchResult, error) {
	return s.searchFn(ctx, req)
}

func (s stubMemoryBackend) Get(ctx context.Context, req MemoryGetRequest) (memory.GetResult, error) {
	return s.getFn(ctx, req)
}

func (s stubMemoryBackend) Grep(ctx context.Context, req MemoryGrepRequest) (memory.GrepResult, error) {
	return s.grepFn(ctx, req)
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

func TestMemoryGrepToolRejectsMissingQuery(t *testing.T) {
	h := NewMemoryGrepTool(stubMemoryBackend{
		grepFn: func(ctx context.Context, req MemoryGrepRequest) (memory.GrepResult, error) {
			return memory.GrepResult{}, nil
		},
	})
	res := h.Call(context.Background(), map[string]interface{}{})
	if !res.IsError {
		t.Fatalf("expected error result")
	}
}

func TestMemoryGrepToolReturnsMatches(t *testing.T) {
	h := NewMemoryGrepTool(stubMemoryBackend{
		grepFn: func(ctx context.Context, req MemoryGrepRequest) (memory.GrepResult, error) {
			if req.Mode != "literal" {
				t.Fatalf("expected mode literal, got %q", req.Mode)
			}
			if len(req.PathGlob) != 2 {
				t.Fatalf("expected 2 path globs, got %d", len(req.PathGlob))
			}
			return memory.GrepResult{
				Count: 1,
				Matches: []memory.GrepMatch{
					{Path: "MEMORY.md", Line: 1, Text: "token-123", Source: "memory"},
				},
			}, nil
		},
	})
	res := h.Call(context.Background(), map[string]interface{}{
		"query":          "token-123",
		"mode":           "literal",
		"case_sensitive": true,
		"path_glob":      []interface{}{"MEMORY.md", "memory/*.md"},
	})
	if res.IsError {
		t.Fatalf("expected success result, got %+v", res.StructuredContent)
	}
	if got := res.StructuredContent["count"]; got != 1 {
		t.Fatalf("expected count=1, got %v", got)
	}
}
