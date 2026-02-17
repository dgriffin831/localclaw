package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/heartbeat"
	"github.com/dgriffin831/localclaw/internal/llm"
)

type recordingHeartbeatMonitor struct {
	pingMessages []string
	startCalls   int
	run          heartbeat.Runner
}

func (m *recordingHeartbeatMonitor) Ping(ctx context.Context, message string) error {
	m.pingMessages = append(m.pingMessages, message)
	return nil
}

func (m *recordingHeartbeatMonitor) Start(ctx context.Context, run heartbeat.Runner) {
	m.startCalls++
	m.run = run
}

func newHeartbeatRuntimeApp(t *testing.T) *App {
	t.Helper()
	t.Setenv("HOME", t.TempDir())

	cfg := config.Default()
	cfg.App.Root = t.TempDir()
	cfg.Agents.Defaults.Workspace = "."
	cfg.Session.Store = "agents/{agentId}/sessions/sessions.json"
	cfg.Cron.Enabled = false
	cfg.Heartbeat.Enabled = true
	cfg.Heartbeat.IntervalSeconds = 1

	app, err := New(cfg)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	return app
}

func TestRunStartsHeartbeatRunner(t *testing.T) {
	app := newHeartbeatRuntimeApp(t)
	monitor := &recordingHeartbeatMonitor{}
	app.heartbeat = monitor

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := app.Run(ctx); err != nil {
		t.Fatalf("run app: %v", err)
	}
	if monitor.startCalls != 1 {
		t.Fatalf("expected heartbeat runner to start once, got %d", monitor.startCalls)
	}
	if len(monitor.pingMessages) != 1 || monitor.pingMessages[0] != "localclaw startup heartbeat" {
		t.Fatalf("expected startup heartbeat ping, got %#v", monitor.pingMessages)
	}
	if monitor.run == nil {
		t.Fatalf("expected heartbeat tick callback to be registered")
	}
}

func TestHeartbeatTickBuildsPromptReferencingWorkspaceHeartbeatFile(t *testing.T) {
	app := newHeartbeatRuntimeApp(t)
	monitor := &recordingHeartbeatMonitor{}
	llmClient := &captureRequestLLMClient{}
	app.heartbeat = monitor
	app.llm = llmClient

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := app.Run(ctx); err != nil {
		t.Fatalf("run app: %v", err)
	}
	workspacePath, err := app.ResolveWorkspacePath("")
	if err != nil {
		t.Fatalf("resolve workspace: %v", err)
	}
	heartbeatPath := filepath.Join(workspacePath, "HEARTBEAT.md")

	if err := monitor.run(context.Background()); err != nil {
		t.Fatalf("run heartbeat tick: %v", err)
	}
	if len(llmClient.requests) != 1 {
		t.Fatalf("expected exactly one heartbeat prompt request, got %d", len(llmClient.requests))
	}
	input := llmClient.lastRequest.Input
	if !strings.Contains(input, "HEARTBEAT.md") {
		t.Fatalf("expected heartbeat prompt to reference HEARTBEAT.md, got %q", input)
	}
	if !strings.Contains(input, heartbeatPath) {
		t.Fatalf("expected heartbeat prompt to include resolved heartbeat path %q, got %q", heartbeatPath, input)
	}
}

func TestHeartbeatTickSkipsWhenHeartbeatFileMissing(t *testing.T) {
	app := newHeartbeatRuntimeApp(t)
	monitor := &recordingHeartbeatMonitor{}
	llmClient := &captureRequestLLMClient{}
	app.heartbeat = monitor
	app.llm = llmClient

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := app.Run(ctx); err != nil {
		t.Fatalf("run app: %v", err)
	}
	workspacePath, err := app.ResolveWorkspacePath("")
	if err != nil {
		t.Fatalf("resolve workspace: %v", err)
	}
	heartbeatPath := filepath.Join(workspacePath, "HEARTBEAT.md")
	if err := os.Remove(heartbeatPath); err != nil {
		t.Fatalf("remove heartbeat file: %v", err)
	}

	if err := monitor.run(context.Background()); err != nil {
		t.Fatalf("expected missing heartbeat file tick to be skipped without error, got %v", err)
	}
	if len(llmClient.requests) != 0 {
		t.Fatalf("expected no heartbeat prompt request when heartbeat file is missing, got %d", len(llmClient.requests))
	}
}

func TestHeartbeatTickFailureDoesNotBlockSubsequentTicks(t *testing.T) {
	app := newHeartbeatRuntimeApp(t)
	monitor := &recordingHeartbeatMonitor{}
	callCount := 0
	llmClient := &captureRequestLLMClient{streamFn: func(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, <-chan error) {
		callCount++
		events := make(chan llm.StreamEvent, 1)
		errs := make(chan error, 1)
		if callCount == 1 {
			errs <- errors.New("llm unavailable")
		} else {
			events <- llm.StreamEvent{Type: llm.StreamEventFinal, Text: "ok"}
		}
		close(events)
		close(errs)
		return events, errs
	}}
	app.heartbeat = monitor
	app.llm = llmClient

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := app.Run(ctx); err != nil {
		t.Fatalf("run app: %v", err)
	}
	if err := monitor.run(context.Background()); err == nil || !strings.Contains(err.Error(), "prompt heartbeat") {
		t.Fatalf("expected first heartbeat tick to surface prompt failure, got %v", err)
	}
	if err := monitor.run(context.Background()); err != nil {
		t.Fatalf("expected subsequent heartbeat tick to still run, got %v", err)
	}
	if len(llmClient.requests) != 2 {
		t.Fatalf("expected two heartbeat prompt attempts, got %d", len(llmClient.requests))
	}
}
