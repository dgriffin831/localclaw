package claudecode

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/dgriffin831/localclaw/internal/llm"
)

const claudeModelCatalogProbeInput = `Return only valid JSON with schema {"models":[{"name":string}]} listing available models for this session. No markdown or prose.`

func (c *LocalClient) DiscoverModelCatalog(ctx context.Context) (llm.ProviderModelCatalog, error) {
	probeModels, probeErr := c.discoverModelsFromProbe(ctx)
	fallback := c.buildFallbackModelDescriptors()
	merged := mergeModelDescriptors(probeModels, fallback)
	if len(merged) == 0 && probeErr != nil {
		return llm.ProviderModelCatalog{}, fmt.Errorf("discover claude models via probe: %w", probeErr)
	}
	return llm.ProviderModelCatalog{
		Provider: "claudecode",
		Models:   merged,
		Partial:  probeErr != nil || len(probeModels) == 0,
	}, nil
}

func (c *LocalClient) discoverModelsFromProbe(ctx context.Context) ([]llm.ProviderModelDescriptor, error) {
	out, err := c.PromptRequest(ctx, llm.Request{Input: claudeModelCatalogProbeInput})
	if err != nil {
		return nil, err
	}
	models := parseClaudeModelCatalogProbeOutput(out)
	if len(models) == 0 {
		return nil, fmt.Errorf("probe returned no models")
	}
	return models, nil
}

func (c *LocalClient) buildFallbackModelDescriptors() []llm.ProviderModelDescriptor {
	profile := strings.TrimSpace(c.settings.Profile)
	if profile == "" {
		return nil
	}
	return []llm.ProviderModelDescriptor{
		{
			Name: profile,
			Reasoning: llm.ReasoningMetadata{
				Supported: false,
			},
		},
	}
}

func mergeModelDescriptors(primary, secondary []llm.ProviderModelDescriptor) []llm.ProviderModelDescriptor {
	combined := make([]llm.ProviderModelDescriptor, 0, len(primary)+len(secondary))
	combined = append(combined, primary...)
	combined = append(combined, secondary...)
	return normalizeClaudeModelDescriptors(combined)
}

func parseClaudeModelCatalogProbeOutput(raw string) []llm.ProviderModelDescriptor {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	candidates := []string{trimmed}
	if unwrapped := unwrapMarkdownCodeFence(trimmed); unwrapped != trimmed {
		candidates = append(candidates, unwrapped)
	}
	if start := strings.Index(trimmed, "{"); start >= 0 {
		if end := strings.LastIndex(trimmed, "}"); end > start {
			candidates = append(candidates, strings.TrimSpace(trimmed[start:end+1]))
		}
	}
	if start := strings.Index(trimmed, "["); start >= 0 {
		if end := strings.LastIndex(trimmed, "]"); end > start {
			candidates = append(candidates, strings.TrimSpace(trimmed[start:end+1]))
		}
	}

	for _, candidate := range candidates {
		if models := parseClaudeModelCatalogJSONCandidate(candidate); len(models) > 0 {
			return models
		}
	}
	return nil
}

func parseClaudeModelCatalogJSONCandidate(raw string) []llm.ProviderModelDescriptor {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil
	}

	var fromObject struct {
		Models []json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal([]byte(text), &fromObject); err == nil && len(fromObject.Models) > 0 {
		models := make([]llm.ProviderModelDescriptor, 0, len(fromObject.Models))
		for _, rawModel := range fromObject.Models {
			if parsed, ok := parseClaudeModelDescriptor(rawModel); ok {
				models = append(models, parsed)
			}
		}
		return normalizeClaudeModelDescriptors(models)
	}

	var fromArray []json.RawMessage
	if err := json.Unmarshal([]byte(text), &fromArray); err == nil && len(fromArray) > 0 {
		models := make([]llm.ProviderModelDescriptor, 0, len(fromArray))
		for _, rawModel := range fromArray {
			if parsed, ok := parseClaudeModelDescriptor(rawModel); ok {
				models = append(models, parsed)
			}
		}
		return normalizeClaudeModelDescriptors(models)
	}
	return nil
}

func parseClaudeModelDescriptor(raw json.RawMessage) (llm.ProviderModelDescriptor, bool) {
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		name := strings.TrimSpace(asString)
		if name == "" {
			return llm.ProviderModelDescriptor{}, false
		}
		return llm.ProviderModelDescriptor{Name: name}, true
	}

	var asObject struct {
		Name  string `json:"name"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal(raw, &asObject); err != nil {
		return llm.ProviderModelDescriptor{}, false
	}
	name := strings.TrimSpace(firstNonBlankString(asObject.Name, asObject.Model))
	if name == "" {
		return llm.ProviderModelDescriptor{}, false
	}
	return llm.ProviderModelDescriptor{Name: name}, true
}

func normalizeClaudeModelDescriptors(models []llm.ProviderModelDescriptor) []llm.ProviderModelDescriptor {
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
		seen[key] = llm.ProviderModelDescriptor{
			Name: name,
			Reasoning: llm.ReasoningMetadata{
				Supported: false,
			},
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

func unwrapMarkdownCodeFence(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) < 3 {
		return trimmed
	}
	if strings.TrimSpace(lines[len(lines)-1]) != "```" {
		return trimmed
	}
	return strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
}
