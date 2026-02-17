package runtime

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/dgriffin831/localclaw/internal/llm"
)

const (
	ProviderToolsProbeSessionID = "__localclaw_tools_probe__"
	providerToolsProbeInput     = "Reply with exactly: ok"
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
	return discovered, nil
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
