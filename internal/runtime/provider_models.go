package runtime

import (
	"context"
	"sort"
	"strings"

	"github.com/dgriffin831/localclaw/internal/llm"
)

// DiscoverProviderModelCatalogs discovers model catalogs for configured providers.
// Discovery failures are isolated per provider.
func (a *App) DiscoverProviderModelCatalogs(ctx context.Context, refresh bool) (map[string]llm.ProviderModelCatalog, map[string]error) {
	results := map[string]llm.ProviderModelCatalog{}
	failures := map[string]error{}

	providers := a.configuredProviders()
	for _, provider := range providers {
		if !refresh {
			if cached, ok := a.loadCachedProviderModelCatalog(provider); ok {
				results[provider] = cached
				continue
			}
		}

		client, err := a.clientForProvider(provider)
		if err != nil {
			failures[provider] = err
			continue
		}
		discoverer, ok := client.(llm.ModelCatalogClient)
		if !ok {
			failures[provider] = errProviderDoesNotSupportModelDiscovery(provider)
			continue
		}

		catalog, err := discoverer.DiscoverModelCatalog(ctx)
		if err != nil {
			failures[provider] = err
			continue
		}
		normalized := normalizeProviderModelCatalog(provider, catalog)
		results[provider] = normalized
		a.storeProviderModelCatalog(provider, normalized)
	}

	return results, failures
}

func (a *App) configuredProviders() []string {
	if len(a.llmClients) == 0 {
		return []string{a.configuredProvider()}
	}
	out := make([]string, 0, len(a.llmClients))
	for provider := range a.llmClients {
		trimmed := strings.ToLower(strings.TrimSpace(provider))
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}

func (a *App) loadCachedProviderModelCatalog(provider string) (llm.ProviderModelCatalog, bool) {
	a.providerModelsMu.Lock()
	defer a.providerModelsMu.Unlock()
	if a.providerModelsCache == nil {
		return llm.ProviderModelCatalog{}, false
	}
	catalog, ok := a.providerModelsCache[strings.ToLower(strings.TrimSpace(provider))]
	return catalog, ok
}

func (a *App) storeProviderModelCatalog(provider string, catalog llm.ProviderModelCatalog) {
	a.providerModelsMu.Lock()
	defer a.providerModelsMu.Unlock()
	if a.providerModelsCache == nil {
		a.providerModelsCache = map[string]llm.ProviderModelCatalog{}
	}
	a.providerModelsCache[strings.ToLower(strings.TrimSpace(provider))] = catalog
}

func normalizeProviderModelCatalog(provider string, catalog llm.ProviderModelCatalog) llm.ProviderModelCatalog {
	normalizedProvider := strings.ToLower(strings.TrimSpace(provider))
	if normalizedProvider == "" {
		normalizedProvider = strings.ToLower(strings.TrimSpace(catalog.Provider))
	}
	if normalizedProvider == "" {
		normalizedProvider = "unknown"
	}

	return llm.ProviderModelCatalog{
		Provider: normalizedProvider,
		Models:   normalizeProviderModels(catalog.Models),
		Partial:  catalog.Partial,
	}
}

func normalizeProviderModels(models []llm.ProviderModelDescriptor) []llm.ProviderModelDescriptor {
	if len(models) == 0 {
		return nil
	}
	seen := map[string]llm.ProviderModelDescriptor{}
	for _, model := range models {
		name := strings.TrimSpace(model.Name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		reasoning := llm.ReasoningMetadata{
			Supported: model.Reasoning.Supported,
			Levels:    normalizeReasoningLevels(model.Reasoning.Levels),
			Default:   strings.ToLower(strings.TrimSpace(model.Reasoning.Default)),
		}
		if len(reasoning.Levels) > 0 {
			reasoning.Supported = true
		}
		seen[key] = llm.ProviderModelDescriptor{
			Name:      name,
			Reasoning: reasoning,
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]llm.ProviderModelDescriptor, 0, len(seen))
	for _, model := range seen {
		out = append(out, model)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func normalizeReasoningLevels(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]string{}
	for _, value := range values {
		level := strings.ToLower(strings.TrimSpace(value))
		if level == "" {
			continue
		}
		if _, ok := seen[level]; ok {
			continue
		}
		seen[level] = level
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for _, value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func errProviderDoesNotSupportModelDiscovery(provider string) error {
	return &providerModelDiscoveryNotSupportedError{provider: provider}
}

type providerModelDiscoveryNotSupportedError struct {
	provider string
}

func (e *providerModelDiscoveryNotSupportedError) Error() string {
	return "provider " + e.provider + " does not support model discovery"
}
