package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/dgriffin831/localclaw/internal/llm"
	"github.com/dgriffin831/localclaw/internal/session"
)

func (a *App) configuredProvider() string {
	provider := strings.ToLower(strings.TrimSpace(a.cfg.LLM.Provider))
	if provider == "" {
		return "claudecode"
	}
	return provider
}

func (a *App) resolveProvider(provider string) string {
	normalized := strings.ToLower(strings.TrimSpace(provider))
	if normalized == "" {
		return a.configuredProvider()
	}
	return normalized
}

func (a *App) clientForProvider(provider string) (llm.Client, error) {
	resolved := a.resolveProvider(provider)
	if a.llm != nil && strings.EqualFold(resolved, a.configuredProvider()) {
		return a.llm, nil
	}
	if len(a.llmClients) > 0 {
		if client, ok := a.llmClients[resolved]; ok && client != nil {
			return client, nil
		}
	}
	return nil, fmt.Errorf("provider %q is not configured", resolved)
}

func (a *App) promptStreamFromClient(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, <-chan error) {
	provider := a.resolveProvider(req.Session.Provider)
	req.Session.Provider = provider
	clientInstance, err := a.clientForProvider(provider)
	if err != nil {
		events := make(chan llm.StreamEvent)
		errs := make(chan error, 1)
		close(events)
		errs <- err
		close(errs)
		return events, errs
	}
	client, ok := clientInstance.(llm.RequestClient)
	if !ok || !clientInstance.Capabilities().SupportsRequestOptions {
		events := make(chan llm.StreamEvent)
		errs := make(chan error, 1)
		close(events)
		errs <- fmt.Errorf("llm provider %q does not support request-based prompt streaming", provider)
		close(errs)
		return events, errs
	}
	return client.PromptStreamRequest(ctx, req)
}

func (a *App) promptStreamWithSessionContinuation(ctx context.Context, resolution SessionResolution, req llm.Request) (<-chan llm.StreamEvent, <-chan error) {
	events := make(chan llm.StreamEvent, 32)
	errs := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errs)

		attemptReq := req
		retried := false
		for {
			providerEvents, providerErrs := a.promptStreamFromClient(ctx, attemptReq)
			retry := false
			seenErr := false

			for providerEvents != nil || providerErrs != nil {
				select {
				case evt, ok := <-providerEvents:
					if !ok {
						providerEvents = nil
						continue
					}
					if evt.Type == llm.StreamEventProviderMetadata && evt.ProviderMetadata != nil {
						a.persistProviderSessionID(ctx, resolution, attemptReq, evt.ProviderMetadata)
					}
					if !emitRuntimeEvent(ctx, events, evt) {
						return
					}
				case err, ok := <-providerErrs:
					if !ok {
						providerErrs = nil
						continue
					}
					if err == nil {
						continue
					}
					seenErr = true
					if !retried && strings.TrimSpace(attemptReq.Session.ProviderSessionID) != "" && isSessionResumeError(err) {
						a.clearPersistedProviderSessionID(ctx, resolution, attemptReq.Session.Provider)
						attemptReq.Session.ProviderSessionID = ""
						retried = true
						retry = true
						providerEvents = nil
						providerErrs = nil
						continue
					}
					_ = emitRuntimeError(ctx, errs, err)
					return
				}
			}

			if retry {
				continue
			}
			if !seenErr {
				return
			}
			return
		}
	}()

	return events, errs
}

func (a *App) persistProviderSessionID(ctx context.Context, resolution SessionResolution, req llm.Request, meta *llm.ProviderMetadata) {
	if a.sessions == nil || meta == nil {
		return
	}
	sessionID := strings.TrimSpace(meta.SessionID)
	if sessionID == "" {
		return
	}
	provider := strings.TrimSpace(meta.Provider)
	if provider == "" {
		provider = req.Session.Provider
	}
	if strings.TrimSpace(provider) == "" {
		return
	}
	_, _ = a.sessions.Update(ctx, resolution.AgentID, resolution.SessionID, func(entry *session.SessionEntry) error {
		entry.Key = resolution.SessionKey
		session.SetProviderSessionID(entry, provider, sessionID)
		return nil
	})
}

func (a *App) clearPersistedProviderSessionID(ctx context.Context, resolution SessionResolution, provider string) {
	if a.sessions == nil {
		return
	}
	_, _ = a.sessions.Update(ctx, resolution.AgentID, resolution.SessionID, func(entry *session.SessionEntry) error {
		session.ClearProviderSessionID(entry, provider)
		return nil
	})
}

func isSessionResumeError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if msg == "" {
		return false
	}
	if strings.Contains(msg, "invalid session") ||
		strings.Contains(msg, "expired session") ||
		strings.Contains(msg, "unknown session") ||
		strings.Contains(msg, "missing session") ||
		strings.Contains(msg, "session not found") ||
		strings.Contains(msg, "no conversation found") {
		return true
	}
	if strings.Contains(msg, "resume") && (strings.Contains(msg, "invalid") || strings.Contains(msg, "expired") || strings.Contains(msg, "missing") || strings.Contains(msg, "not found")) {
		return true
	}
	return false
}

func emitRuntimeEvent(ctx context.Context, ch chan<- llm.StreamEvent, evt llm.StreamEvent) bool {
	select {
	case ch <- evt:
		return true
	case <-ctx.Done():
		return false
	}
}

func emitRuntimeError(ctx context.Context, ch chan<- error, err error) bool {
	if err == nil {
		return true
	}
	select {
	case ch <- err:
		return true
	case <-ctx.Done():
		return false
	}
}
