package runtime

import (
	"context"
	"testing"

	"github.com/dgriffin831/localclaw/internal/cron"
	"github.com/dgriffin831/localclaw/internal/llm"
)

func TestRunCronEntryDefaultUsesDefaultSessionPrompt(t *testing.T) {
	_, app, _ := newToolTestApp(t, true)
	llmClient := &captureRequestLLMClient{}
	app.llm = llmClient
	app.llmClients = map[string]llm.Client{
		"claudecode": llmClient,
		"codex":      llmClient,
	}

	outcome := app.runCronEntry(context.Background(), cron.Entry{
		ID:            "job-default",
		AgentID:       "default",
		SessionTarget: cron.SessionTargetDefault,
		Message:       "run the daily review",
	})
	if outcome.Status != cron.RunStatusSuccess {
		t.Fatalf("expected success, got %+v", outcome)
	}
	if llmClient.lastRequest.Input != "run the daily review" {
		t.Fatalf("expected message input, got %q", llmClient.lastRequest.Input)
	}
	if llmClient.lastRequest.Session.SessionKey != "default/default" {
		t.Fatalf("expected default session key, got %q", llmClient.lastRequest.Session.SessionKey)
	}
}

func TestRunCronEntryIsolatedUsesCronSession(t *testing.T) {
	_, app, _ := newToolTestApp(t, true)
	llmClient := &captureRequestLLMClient{}
	app.llm = llmClient
	app.llmClients = map[string]llm.Client{
		"claudecode": llmClient,
		"codex":      llmClient,
	}

	outcome := app.runCronEntry(context.Background(), cron.Entry{
		ID:            "job-isolated",
		AgentID:       "default",
		SessionTarget: cron.SessionTargetIsolated,
		Message:       "summarize overnight updates",
	})
	if outcome.Status != cron.RunStatusSuccess {
		t.Fatalf("expected success, got %+v", outcome)
	}
	if llmClient.lastRequest.Input != "summarize overnight updates" {
		t.Fatalf("expected message input, got %q", llmClient.lastRequest.Input)
	}
	if llmClient.lastRequest.Session.SessionKey != "default/cron-job-isolated" {
		t.Fatalf("expected isolated cron session key, got %q", llmClient.lastRequest.Session.SessionKey)
	}
}

func TestRunCronEntryReturnsSkippedOnUnknownTarget(t *testing.T) {
	_, app, _ := newToolTestApp(t, true)
	llmClient := &captureRequestLLMClient{}
	app.llm = llmClient

	outcome := app.runCronEntry(context.Background(), cron.Entry{
		ID:            "job-invalid",
		SessionTarget: "main",
		Message:       "should not run",
	})
	if outcome.Status != cron.RunStatusSkipped {
		t.Fatalf("expected skipped outcome, got %+v", outcome)
	}
	if len(llmClient.requests) != 0 {
		t.Fatalf("expected no prompt requests for skipped run, got %d", len(llmClient.requests))
	}
}
