package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/pelletier/go-toml"

	"github.com/dgriffin831/localclaw/internal/llm"
)

const codexModelCatalogProbeInput = `Return only valid JSON with schema {"models":[{"name":string,"reasoning":{"supported":bool,"levels":[string,...],"default":string}}]} listing available models for this session. No markdown or prose.`

var codexReasoningLevels = []string{"xlow", "low", "medium", "high", "xhigh"}

func (c *LocalClient) DiscoverModelCatalog(ctx context.Context) (llm.ProviderModelCatalog, error) {
	modelsFromConfig, configErr := c.discoverModelsFromConfig()
	probeModels, probeErr := c.discoverModelsFromProbe(ctx)

	fallbackModels := c.buildFallbackModelDescriptors(modelsFromConfig)
	merged := mergeModelDescriptorSets(probeModels, fallbackModels)
	if len(merged) == 0 {
		if probeErr != nil {
			return llm.ProviderModelCatalog{}, fmt.Errorf("discover codex models via probe: %w", probeErr)
		}
		if configErr != nil {
			return llm.ProviderModelCatalog{}, fmt.Errorf("discover codex models from config: %w", configErr)
		}
	}

	partial := probeErr != nil || len(probeModels) == 0 || configErr != nil
	return llm.ProviderModelCatalog{
		Provider: "codex",
		Models:   merged,
		Partial:  partial,
	}, nil
}

func (c *LocalClient) buildFallbackModelDescriptors(models []string) []llm.ProviderModelDescriptor {
	if len(models) == 0 {
		if configured := strings.TrimSpace(c.settings.Model); configured != "" {
			models = append(models, configured)
		}
	}
	if len(models) == 0 {
		return nil
	}
	out := make([]llm.ProviderModelDescriptor, 0, len(models))
	for _, name := range models {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		out = append(out, llm.ProviderModelDescriptor{
			Name: trimmed,
			Reasoning: llm.ReasoningMetadata{
				Supported: true,
				Levels:    append([]string{}, codexReasoningLevels...),
				Default:   normalizeReasoningDefault(c.settings.ReasoningDefault),
			},
		})
	}
	return normalizeCodexModelDescriptors(out, c.settings.ReasoningDefault)
}

func (c *LocalClient) discoverModelsFromProbe(ctx context.Context) ([]llm.ProviderModelDescriptor, error) {
	out, err := c.PromptRequest(ctx, llm.Request{
		Input: codexModelCatalogProbeInput,
		Options: llm.PromptOptions{
			ModelOverride:     strings.TrimSpace(c.settings.Model),
			ReasoningOverride: normalizeReasoningDefault(c.settings.ReasoningDefault),
		},
	})
	if err != nil {
		return nil, err
	}
	parsed := parseCodexModelCatalogProbeOutput(out, c.settings.ReasoningDefault)
	if len(parsed) == 0 {
		return nil, fmt.Errorf("probe returned no models")
	}
	return parsed, nil
}

func (c *LocalClient) discoverModelsFromConfig() ([]string, error) {
	configPath, _, err := c.resolveEffectiveMCPConfigPath()
	if err != nil {
		return nil, err
	}

	buf, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	tree, err := toml.LoadBytes(buf)
	if err != nil {
		return nil, err
	}

	models := make([]string, 0, 8)
	if configured := strings.TrimSpace(asLooseString(tree.Get("model"))); configured != "" {
		models = append(models, configured)
	}
	if migrationTree, ok := tree.Get("notice.model_migrations").(*toml.Tree); ok {
		for _, key := range migrationTree.Keys() {
			if model := strings.TrimSpace(key); model != "" {
				models = append(models, model)
			}
			value := migrationTree.GetPath([]string{key})
			if value == nil {
				value = migrationTree.Get(key)
			}
			if migrated := strings.TrimSpace(asLooseString(value)); migrated != "" {
				models = append(models, migrated)
			}
		}
	}
	return normalizeModelNames(models), nil
}

func mergeModelDescriptorSets(primary, secondary []llm.ProviderModelDescriptor) []llm.ProviderModelDescriptor {
	combined := make([]llm.ProviderModelDescriptor, 0, len(primary)+len(secondary))
	combined = append(combined, primary...)
	combined = append(combined, secondary...)
	return normalizeCodexModelDescriptors(combined, "")
}

func parseCodexModelCatalogProbeOutput(raw string, defaultReasoning string) []llm.ProviderModelDescriptor {
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
		if models := parseCodexModelCatalogJSONCandidate(candidate, defaultReasoning); len(models) > 0 {
			return models
		}
	}
	return nil
}

func parseCodexModelCatalogJSONCandidate(raw string, defaultReasoning string) []llm.ProviderModelDescriptor {
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
			if parsed, ok := parseCodexModelDescriptor(rawModel); ok {
				models = append(models, parsed)
			}
		}
		return normalizeCodexModelDescriptors(models, defaultReasoning)
	}

	var fromArray []json.RawMessage
	if err := json.Unmarshal([]byte(text), &fromArray); err == nil && len(fromArray) > 0 {
		models := make([]llm.ProviderModelDescriptor, 0, len(fromArray))
		for _, rawModel := range fromArray {
			if parsed, ok := parseCodexModelDescriptor(rawModel); ok {
				models = append(models, parsed)
			}
		}
		return normalizeCodexModelDescriptors(models, defaultReasoning)
	}
	return nil
}

func parseCodexModelDescriptor(raw json.RawMessage) (llm.ProviderModelDescriptor, bool) {
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		name := strings.TrimSpace(asString)
		if name == "" {
			return llm.ProviderModelDescriptor{}, false
		}
		return llm.ProviderModelDescriptor{Name: name}, true
	}

	var asObject struct {
		Name      string `json:"name"`
		Model     string `json:"model"`
		Reasoning struct {
			Supported bool     `json:"supported"`
			Levels    []string `json:"levels"`
			Default   string   `json:"default"`
		} `json:"reasoning"`
	}
	if err := json.Unmarshal(raw, &asObject); err != nil {
		return llm.ProviderModelDescriptor{}, false
	}
	name := firstNonBlankString(asObject.Name, asObject.Model)
	if name == "" {
		return llm.ProviderModelDescriptor{}, false
	}
	reasoning := llm.ReasoningMetadata{
		Supported: asObject.Reasoning.Supported,
		Levels:    normalizeModelNames(asObject.Reasoning.Levels),
		Default:   strings.ToLower(strings.TrimSpace(asObject.Reasoning.Default)),
	}
	if len(reasoning.Levels) > 0 || reasoning.Default != "" {
		reasoning.Supported = true
	}
	return llm.ProviderModelDescriptor{
		Name:      name,
		Reasoning: reasoning,
	}, true
}

func normalizeCodexModelDescriptors(models []llm.ProviderModelDescriptor, defaultReasoning string) []llm.ProviderModelDescriptor {
	if len(models) == 0 {
		return nil
	}
	seen := map[string]llm.ProviderModelDescriptor{}
	resolvedDefault := normalizeReasoningDefault(defaultReasoning)
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
			Levels:    normalizeModelNames(model.Reasoning.Levels),
			Default:   strings.ToLower(strings.TrimSpace(model.Reasoning.Default)),
		}
		if !reasoning.Supported && len(reasoning.Levels) == 0 && reasoning.Default == "" {
			reasoning.Supported = true
		}
		if reasoning.Supported && len(reasoning.Levels) == 0 {
			reasoning.Levels = append([]string{}, codexReasoningLevels...)
		}
		if reasoning.Supported && reasoning.Default == "" {
			reasoning.Default = resolvedDefault
		}
		if reasoning.Supported && reasoning.Default != "" && !containsLevel(reasoning.Levels, reasoning.Default) {
			reasoning.Levels = append(reasoning.Levels, reasoning.Default)
			sort.Strings(reasoning.Levels)
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

func normalizeModelNames(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]string{}
	for _, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = normalized
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

func normalizeReasoningDefault(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return "medium"
	}
	return normalized
}

func containsLevel(values []string, target string) bool {
	normalized := strings.ToLower(strings.TrimSpace(target))
	if normalized == "" {
		return false
	}
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), normalized) {
			return true
		}
	}
	return false
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
