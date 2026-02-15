package memory

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type stubLocalEmbeddingRunner struct {
	embedFn func(ctx context.Context, req localEmbeddingRequest) (localEmbeddingResponse, error)
}

func (s stubLocalEmbeddingRunner) Embed(ctx context.Context, req localEmbeddingRequest) (localEmbeddingResponse, error) {
	return s.embedFn(ctx, req)
}

func TestLocalEmbeddingProviderQueryTimeout(t *testing.T) {
	provider, err := newLocalEmbeddingProviderWithRunner(LocalEmbeddingConfig{
		RuntimePath:  "sh",
		QueryTimeout: 20 * time.Millisecond,
	}, "example/model", stubLocalEmbeddingRunner{embedFn: func(ctx context.Context, req localEmbeddingRequest) (localEmbeddingResponse, error) {
		<-ctx.Done()
		return localEmbeddingResponse{}, ctx.Err()
	}})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	_, err = provider.EmbedQuery(context.Background(), "alpha")
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "timed out") {
		t.Fatalf("expected timeout error, got %q", err.Error())
	}
}

func TestLocalEmbeddingProviderBatchTimeout(t *testing.T) {
	provider, err := newLocalEmbeddingProviderWithRunner(LocalEmbeddingConfig{
		RuntimePath:  "sh",
		BatchTimeout: 20 * time.Millisecond,
	}, "example/model", stubLocalEmbeddingRunner{embedFn: func(ctx context.Context, req localEmbeddingRequest) (localEmbeddingResponse, error) {
		<-ctx.Done()
		return localEmbeddingResponse{}, ctx.Err()
	}})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	_, err = provider.EmbedBatch(context.Background(), []string{"alpha", "beta"})
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "timed out") {
		t.Fatalf("expected timeout error, got %q", err.Error())
	}
}

func TestNewLocalEmbeddingProviderMissingRuntimeIsActionable(t *testing.T) {
	_, err := newLocalEmbeddingProvider(LocalEmbeddingConfig{}, "example/model")
	if err == nil {
		t.Fatalf("expected runtime setup error")
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "runtime") {
		t.Fatalf("expected runtime setup message, got %q", err.Error())
	}
	if !strings.Contains(msg, "provider=none") {
		t.Fatalf("expected actionable fallback guidance, got %q", err.Error())
	}
}

func TestLocalEmbeddingProviderPassesModelAndInputToRunner(t *testing.T) {
	provider, err := newLocalEmbeddingProviderWithRunner(LocalEmbeddingConfig{RuntimePath: "sh"}, "example/model", stubLocalEmbeddingRunner{embedFn: func(ctx context.Context, req localEmbeddingRequest) (localEmbeddingResponse, error) {
		if req.Model != "example/model" {
			t.Fatalf("expected model to propagate, got %q", req.Model)
		}
		if len(req.Texts) != 2 || req.Texts[0] != "alpha" || req.Texts[1] != "beta" {
			t.Fatalf("unexpected texts payload: %v", req.Texts)
		}
		return localEmbeddingResponse{Embeddings: [][]float32{{1, 2}, {3, 4}}}, nil
	}})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	vectors, err := provider.EmbedBatch(context.Background(), []string{"alpha", "beta"})
	if err != nil {
		t.Fatalf("embed batch: %v", err)
	}
	if len(vectors) != 2 {
		t.Fatalf("expected two vectors, got %d", len(vectors))
	}
}

func TestLocalEmbeddingProviderQueryRequiresEmbeddingOutput(t *testing.T) {
	provider, err := newLocalEmbeddingProviderWithRunner(LocalEmbeddingConfig{RuntimePath: "sh"}, "example/model", stubLocalEmbeddingRunner{embedFn: func(ctx context.Context, req localEmbeddingRequest) (localEmbeddingResponse, error) {
		return localEmbeddingResponse{}, nil
	}})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	_, err = provider.EmbedQuery(context.Background(), "alpha")
	if err == nil {
		t.Fatalf("expected query output validation error")
	}
	if !errors.Is(err, errLocalEmbeddingOutputEmpty) {
		t.Fatalf("expected errLocalEmbeddingOutputEmpty, got %v", err)
	}
}
