package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/dgriffin831/localclaw/internal/llm"
)

const (
	ProviderToolsProbeSessionID = "__localclaw_tools_probe__"
	providerToolsProbeInput     = "Reply with exactly: ok"
	providerToolsJSONProbeInput = "Return only valid JSON with schema {\"tools\":[string,...]} listing every tool name you can invoke right now in this session. No markdown or prose."
)

// DiscoverProviderMetadata probes the provider for current model/tool metadata
// without persisting provider session IDs into localclaw session state.
func (a *App) DiscoverProviderMetadata(ctx context.Context, agentID string, opts llm.PromptOptions) (llm.ProviderMetadata, error) {
	resolvedAgentID := ResolveAgentID(agentID)
	configuredProvider := strings.ToLower(strings.TrimSpace(a.cfg.LLM.Provider))
	resolution := ResolveSession(resolvedAgentID, ProviderToolsProbeSessionID)

	req := llm.Request{
		Input: providerToolsProbeInput,
		Session: llm.SessionMetadata{
			AgentID:    resolution.AgentID,
			SessionID:  resolution.SessionID,
			SessionKey: resolution.SessionKey,
			Provider:   configuredProvider,
		},
		Options: llm.PromptOptions{
			ModelOverride: strings.TrimSpace(opts.ModelOverride),
		},
	}

	probeCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	events, errs := a.promptStreamFromClient(probeCtx, req)
	discovered := llm.ProviderMetadata{Provider: configuredProvider}

	for events != nil || errs != nil {
		select {
		case evt, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if evt.Type != llm.StreamEventProviderMetadata || evt.ProviderMetadata == nil {
				continue
			}
			if provider := strings.TrimSpace(evt.ProviderMetadata.Provider); provider != "" {
				discovered.Provider = provider
			}
			if model := strings.TrimSpace(evt.ProviderMetadata.Model); model != "" {
				discovered.Model = model
			}
			if len(evt.ProviderMetadata.Tools) > 0 {
				discovered.Tools = append(discovered.Tools, evt.ProviderMetadata.Tools...)
				// Tools are discovered; stop probe early.
				cancel()
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err == nil || errors.Is(err, context.Canceled) {
				continue
			}
			if len(discovered.Tools) > 0 {
				continue
			}
			return llm.ProviderMetadata{}, err
		}
	}

	discovered.Tools = normalizeProviderMetadataTools(discovered.Tools)
	if len(discovered.Tools) == 0 && strings.EqualFold(configuredProvider, "codex") {
		if tools := a.discoverCodexToolsViaJSONProbe(ctx, resolvedAgentID, opts, discovered.Provider); len(tools) > 0 {
			discovered.Tools = normalizeProviderMetadataTools(tools)
		}
	}
	return discovered, nil
}

func (a *App) discoverCodexToolsViaJSONProbe(ctx context.Context, agentID string, opts llm.PromptOptions, provider string) []string {
	probeProvider := strings.TrimSpace(provider)
	if probeProvider == "" {
		probeProvider = "codex"
	}
	resolution := ResolveSession(agentID, ProviderToolsProbeSessionID)
	req := llm.Request{
		Input: providerToolsJSONProbeInput,
		Session: llm.SessionMetadata{
			AgentID:    resolution.AgentID,
			SessionID:  resolution.SessionID,
			SessionKey: resolution.SessionKey,
			Provider:   probeProvider,
		},
		Options: llm.PromptOptions{
			ModelOverride: strings.TrimSpace(opts.ModelOverride),
		},
	}

	events, errs := a.promptStreamFromClient(ctx, req)
	var streamed strings.Builder
	finalText := ""
	for events != nil || errs != nil {
		select {
		case evt, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if evt.Type == llm.StreamEventFinal {
				finalText = strings.TrimSpace(evt.Text)
				continue
			}
			if evt.Type == llm.StreamEventDelta {
				streamed.WriteString(evt.Text)
			}
		case _, ok := <-errs:
			if !ok {
				errs = nil
			}
		}
	}

	payload := strings.TrimSpace(finalText)
	if payload == "" {
		payload = strings.TrimSpace(streamed.String())
	}
	return parseToolNamesFromJSONProbeOutput(payload)
}

func parseToolNamesFromJSONProbeOutput(raw string) []string {
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
		if tools := parseToolNamesJSONCandidate(candidate); len(tools) > 0 {
			return normalizeProviderMetadataTools(tools)
		}
	}
	return nil
}

func parseToolNamesJSONCandidate(raw string) []string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil
	}

	var fromObject struct {
		Tools []string `json:"tools"`
	}
	if err := json.Unmarshal([]byte(text), &fromObject); err == nil {
		return fromObject.Tools
	}

	var fromArray []string
	if err := json.Unmarshal([]byte(text), &fromArray); err == nil {
		return fromArray
	}
	return nil
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

func normalizeProviderMetadataTools(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]string{}
	for _, raw := range values {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = name
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for _, name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
