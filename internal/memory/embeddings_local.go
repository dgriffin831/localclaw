package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultLocalEmbeddingQueryTimeout = 5 * time.Second
	defaultLocalEmbeddingBatchTimeout = 20 * time.Second
)

var errLocalEmbeddingOutputEmpty = errors.New("local embedding runtime returned no embeddings")

// LocalEmbeddingConfig controls local embedding runtime behavior.
type LocalEmbeddingConfig struct {
	RuntimePath   string
	ModelPath     string
	ModelCacheDir string
	QueryTimeout  time.Duration
	BatchTimeout  time.Duration
}

type localEmbeddingProvider struct {
	runtimePath   string
	model         string
	modelPath     string
	modelCacheDir string
	queryTimeout  time.Duration
	batchTimeout  time.Duration
	runner        localEmbeddingRunner
}

type localEmbeddingRunner interface {
	Embed(ctx context.Context, req localEmbeddingRequest) (localEmbeddingResponse, error)
}

type localEmbeddingRequest struct {
	Model         string
	ModelPath     string
	ModelCacheDir string
	Texts         []string
}

type localEmbeddingResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

func newLocalEmbeddingProvider(cfg LocalEmbeddingConfig, model string) (EmbeddingProvider, error) {
	runtimePath, err := resolveLocalRuntimePath(cfg.RuntimePath)
	if err != nil {
		return nil, err
	}
	if err := validateLocalModelPath(cfg.ModelPath); err != nil {
		return nil, err
	}
	if strings.TrimSpace(model) == "" {
		return nil, errors.New("local embedding model is empty; set memorySearch.model or set provider=none")
	}

	p := &localEmbeddingProvider{
		runtimePath:   runtimePath,
		model:         strings.TrimSpace(model),
		modelPath:     strings.TrimSpace(cfg.ModelPath),
		modelCacheDir: strings.TrimSpace(cfg.ModelCacheDir),
		queryTimeout:  cfg.QueryTimeout,
		batchTimeout:  cfg.BatchTimeout,
	}
	p.runner = localEmbeddingCLIRunner{runtimePath: p.runtimePath}
	return p, nil
}

func newLocalEmbeddingProviderWithRunner(cfg LocalEmbeddingConfig, model string, runner localEmbeddingRunner) (*localEmbeddingProvider, error) {
	provider, err := newLocalEmbeddingProvider(cfg, model)
	if err != nil {
		return nil, err
	}
	p, ok := provider.(*localEmbeddingProvider)
	if !ok {
		return nil, errors.New("unexpected local provider type")
	}
	p.runner = runner
	return p, nil
}

func (p *localEmbeddingProvider) ProviderName() string { return EmbeddingProviderLocal }

func (p *localEmbeddingProvider) Model() string { return p.model }

func (p *localEmbeddingProvider) ProviderKey() string {
	if p.modelPath != "" {
		return fmt.Sprintf("%s|%s", p.model, p.modelPath)
	}
	return p.model
}

func (p *localEmbeddingProvider) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	vectors, err := p.embed(ctx, []string{text}, p.queryTimeout, "query")
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return nil, errLocalEmbeddingOutputEmpty
	}
	return vectors[0], nil
}

func (p *localEmbeddingProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}
	return p.embed(ctx, texts, p.batchTimeout, "batch")
}

func (p *localEmbeddingProvider) embed(ctx context.Context, texts []string, timeout time.Duration, op string) ([][]float32, error) {
	if timeout <= 0 {
		if op == "query" {
			timeout = defaultLocalEmbeddingQueryTimeout
		} else {
			timeout = defaultLocalEmbeddingBatchTimeout
		}
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := p.runner.Embed(timeoutCtx, localEmbeddingRequest{
		Model:         p.model,
		ModelPath:     p.modelPath,
		ModelCacheDir: p.modelCacheDir,
		Texts:         texts,
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(timeoutCtx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("local embedding %s timed out after %s", op, timeout)
		}
		return nil, fmt.Errorf("local embedding %s failed: %w", op, err)
	}
	if len(resp.Embeddings) == 0 {
		return nil, errLocalEmbeddingOutputEmpty
	}
	return resp.Embeddings, nil
}

type localEmbeddingCLIRunner struct {
	runtimePath string
}

func (r localEmbeddingCLIRunner) Embed(ctx context.Context, req localEmbeddingRequest) (localEmbeddingResponse, error) {
	payload := struct {
		Model         string   `json:"model"`
		ModelPath     string   `json:"modelPath,omitempty"`
		ModelCacheDir string   `json:"modelCacheDir,omitempty"`
		Input         []string `json:"input"`
	}{
		Model:         req.Model,
		ModelPath:     req.ModelPath,
		ModelCacheDir: req.ModelCacheDir,
		Input:         req.Texts,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return localEmbeddingResponse{}, fmt.Errorf("marshal local embedding request: %w", err)
	}

	cmd := exec.CommandContext(ctx, r.runtimePath, "embed", "--format", "json")
	cmd.Stdin = bytes.NewReader(body)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderrText := strings.TrimSpace(stderr.String())
		if stderrText != "" {
			return localEmbeddingResponse{}, fmt.Errorf("run local embedding runtime: %v (stderr: %s)", err, stderrText)
		}
		return localEmbeddingResponse{}, fmt.Errorf("run local embedding runtime: %w", err)
	}

	var out localEmbeddingResponse
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return localEmbeddingResponse{}, fmt.Errorf("decode local embedding response: %w", err)
	}
	return out, nil
}

func resolveLocalRuntimePath(runtimePath string) (string, error) {
	trimmed := strings.TrimSpace(runtimePath)
	if trimmed == "" {
		return "", errors.New("local embedding runtime is not configured; set memorySearch.local.runtimePath or set provider=none")
	}

	if strings.Contains(trimmed, string(filepath.Separator)) {
		info, err := os.Stat(trimmed)
		if err != nil {
			return "", fmt.Errorf("local embedding runtime %q is not available: %w. Set provider=none to disable embeddings", trimmed, err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("local embedding runtime %q is a directory; set memorySearch.local.runtimePath to an executable or set provider=none", trimmed)
		}
		return trimmed, nil
	}

	resolved, err := exec.LookPath(trimmed)
	if err != nil {
		return "", fmt.Errorf("local embedding runtime %q was not found in PATH. Install/configure the runtime or set provider=none", trimmed)
	}
	return resolved, nil
}

func validateLocalModelPath(modelPath string) error {
	trimmed := strings.TrimSpace(modelPath)
	if trimmed == "" {
		return nil
	}
	info, err := os.Stat(trimmed)
	if err != nil {
		return fmt.Errorf("local embedding model path %q is not available: %w. Update memorySearch.local.modelPath or set provider=none", trimmed, err)
	}
	if info.IsDir() {
		return fmt.Errorf("local embedding model path %q is a directory; set memorySearch.local.modelPath to a model file or set provider=none", trimmed)
	}
	return nil
}
