package memory

import (
	"strings"
	"testing"
)

func TestResolveEmbeddingProviderDefaultsToNone(t *testing.T) {
	resolved, err := ResolveEmbeddingProvider(EmbeddingProviderConfig{})
	if err != nil {
		t.Fatalf("resolve provider: %v", err)
	}
	if resolved.ProviderName != EmbeddingProviderNone {
		t.Fatalf("expected provider %q, got %q", EmbeddingProviderNone, resolved.ProviderName)
	}
	if resolved.Provider == nil {
		t.Fatalf("expected a provider implementation")
	}
	if resolved.Model != "" {
		t.Fatalf("expected empty model for none provider, got %q", resolved.Model)
	}
}

func TestResolveEmbeddingProviderRejectsUnsupportedProvider(t *testing.T) {
	_, err := ResolveEmbeddingProvider(EmbeddingProviderConfig{Provider: "openai"})
	if err == nil {
		t.Fatalf("expected provider validation error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "local-only") {
		t.Fatalf("expected local-only messaging, got %q", msg)
	}
	if !strings.Contains(msg, `"none"`) || !strings.Contains(msg, `"local"`) {
		t.Fatalf("expected supported provider list in error, got %q", msg)
	}
}

func TestResolveEmbeddingProviderLocalDefaultsModelWhenUnset(t *testing.T) {
	resolved, err := ResolveEmbeddingProvider(EmbeddingProviderConfig{
		Provider: "local",
		Local: LocalEmbeddingConfig{
			RuntimePath: "sh",
		},
	})
	if err != nil {
		t.Fatalf("resolve provider: %v", err)
	}
	if resolved.ProviderName != EmbeddingProviderLocal {
		t.Fatalf("expected provider %q, got %q", EmbeddingProviderLocal, resolved.ProviderName)
	}
	if resolved.Model != defaultLocalEmbeddingModel {
		t.Fatalf("expected default local model %q, got %q", defaultLocalEmbeddingModel, resolved.Model)
	}
}

func TestResolveEmbeddingProviderFallsBackToNoneWhenLocalUnavailable(t *testing.T) {
	resolved, err := ResolveEmbeddingProvider(EmbeddingProviderConfig{
		Provider: "local",
		Fallback: "none",
	})
	if err != nil {
		t.Fatalf("resolve provider: %v", err)
	}
	if resolved.ProviderName != EmbeddingProviderNone {
		t.Fatalf("expected provider fallback to %q, got %q", EmbeddingProviderNone, resolved.ProviderName)
	}
	if !resolved.FallbackActivated {
		t.Fatalf("expected fallback activation")
	}
	if !strings.Contains(strings.ToLower(resolved.Message), "set provider=none") {
		t.Fatalf("expected actionable fallback message, got %q", resolved.Message)
	}
}

func TestResolveEmbeddingProviderLocalFailureWithoutFallbackReturnsError(t *testing.T) {
	_, err := ResolveEmbeddingProvider(EmbeddingProviderConfig{
		Provider: "local",
		Fallback: "local",
	})
	if err == nil {
		t.Fatalf("expected local activation error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "runtime") {
		t.Fatalf("expected runtime setup message, got %q", err.Error())
	}
}
