package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const (
	EmbeddingProviderNone  = "none"
	EmbeddingProviderLocal = "local"

	defaultLocalEmbeddingModel = "google/embeddinggemma-3-small-v1"
)

var ErrEmbeddingsDisabled = errors.New("embeddings are disabled (provider=none)")

// EmbeddingProvider computes vector embeddings for query and batch input.
type EmbeddingProvider interface {
	ProviderName() string
	Model() string
	ProviderKey() string
	EmbedQuery(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
}

// EmbeddingProviderConfig controls provider mode selection and activation.
type EmbeddingProviderConfig struct {
	Provider string
	Fallback string
	Model    string
	Local    LocalEmbeddingConfig
}

// EmbeddingProviderResolution captures resolved provider mode and fallback details.
type EmbeddingProviderResolution struct {
	Provider          EmbeddingProvider
	ProviderName      string
	Model             string
	FallbackActivated bool
	Message           string
}

// ResolveEmbeddingProvider selects and initializes an embedding provider.
// Supported providers are local-only: none and local.
func ResolveEmbeddingProvider(cfg EmbeddingProviderConfig) (EmbeddingProviderResolution, error) {
	provider := normalizeEmbeddingMode(cfg.Provider)
	fallback := normalizeEmbeddingMode(cfg.Fallback)
	model := strings.TrimSpace(cfg.Model)

	if err := validateEmbeddingMode(provider, "provider"); err != nil {
		return EmbeddingProviderResolution{}, err
	}
	if err := validateEmbeddingMode(fallback, "fallback"); err != nil {
		return EmbeddingProviderResolution{}, err
	}

	if provider == EmbeddingProviderLocal {
		if model == "" {
			model = defaultLocalEmbeddingModel
		}

		localProvider, err := newLocalEmbeddingProvider(cfg.Local, model)
		if err == nil {
			return EmbeddingProviderResolution{
				Provider:     localProvider,
				ProviderName: EmbeddingProviderLocal,
				Model:        model,
			}, nil
		}
		if fallback == EmbeddingProviderNone {
			msg := fmt.Sprintf("local embedding provider is unavailable: %v. Falling back to provider=none. To continue without embeddings, set provider=none explicitly.", err)
			return EmbeddingProviderResolution{
				Provider:          noneEmbeddingProvider{},
				ProviderName:      EmbeddingProviderNone,
				FallbackActivated: true,
				Message:           msg,
			}, nil
		}
		return EmbeddingProviderResolution{}, fmt.Errorf("activate local embedding provider: %w", err)
	}

	return EmbeddingProviderResolution{
		Provider:     noneEmbeddingProvider{},
		ProviderName: EmbeddingProviderNone,
	}, nil
}

func normalizeEmbeddingMode(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" || value == "auto" {
		return EmbeddingProviderNone
	}
	return value
}

func validateEmbeddingMode(mode string, field string) error {
	if mode == EmbeddingProviderNone || mode == EmbeddingProviderLocal {
		return nil
	}
	return fmt.Errorf("unsupported memory embedding %s %q: localclaw is local-only and supports only %q or %q", field, mode, EmbeddingProviderNone, EmbeddingProviderLocal)
}

type noneEmbeddingProvider struct{}

func (noneEmbeddingProvider) ProviderName() string { return EmbeddingProviderNone }

func (noneEmbeddingProvider) Model() string { return "" }

func (noneEmbeddingProvider) ProviderKey() string { return EmbeddingProviderNone }

func (noneEmbeddingProvider) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	_ = ctx
	_ = text
	return nil, ErrEmbeddingsDisabled
}

func (noneEmbeddingProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	_ = ctx
	_ = texts
	return nil, ErrEmbeddingsDisabled
}
