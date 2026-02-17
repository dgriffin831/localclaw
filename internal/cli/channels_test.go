package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/runtime"
)

func TestRunChannelsCommandRequiresSubcommand(t *testing.T) {
	cfg := config.Default()
	cfg.App.Root = t.TempDir()
	cfg.Channels.Enabled = []string{"signal"}
	cfg.Channels.Signal.Inbound.Enabled = true
	cfg.Channels.Signal.Inbound.AllowFrom = []string{"+15550000001"}
	cfg.Channels.Signal.Inbound.DefaultAgent = "default"
	cfg.Cron.Enabled = false
	cfg.Heartbeat.Enabled = false

	app, err := runtime.New(cfg)
	if err != nil {
		t.Fatalf("runtime.New: %v", err)
	}

	err = RunChannelsCommand(context.Background(), cfg, app, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("expected missing channels subcommand error")
	}
}

func TestRunChannelsCommandRejectsUnknownSubcommand(t *testing.T) {
	cfg := config.Default()
	cfg.App.Root = t.TempDir()
	cfg.Channels.Enabled = []string{"signal"}
	cfg.Channels.Signal.Inbound.Enabled = true
	cfg.Channels.Signal.Inbound.AllowFrom = []string{"+15550000001"}
	cfg.Channels.Signal.Inbound.DefaultAgent = "default"
	cfg.Cron.Enabled = false
	cfg.Heartbeat.Enabled = false

	app, err := runtime.New(cfg)
	if err != nil {
		t.Fatalf("runtime.New: %v", err)
	}

	err = RunChannelsCommand(context.Background(), cfg, app, []string{"unknown"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unknown channels subcommand") {
		t.Fatalf("expected unknown subcommand error, got %v", err)
	}
}

func TestRunChannelsServeStartsBackupLoopsForLongRunningMode(t *testing.T) {
	cfg, app := newChannelsCommandTestApp(t)

	originalStarter := startBackgroundBackupLoops
	defer func() { startBackgroundBackupLoops = originalStarter }()
	startCalls := 0
	startBackgroundBackupLoops = func(ctx context.Context, cfg config.Config, app *runtime.App) {
		startCalls++
	}

	err := RunChannelsCommand(context.Background(), cfg, app, []string{"serve"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "channels.signal.inbound.enabled must be true") {
		t.Fatalf("expected inbound-enabled validation error, got %v", err)
	}
	if startCalls != 1 {
		t.Fatalf("expected backup loops to start once for channels serve, got %d", startCalls)
	}
}

func TestRunChannelsServeOnceSkipsBackupLoops(t *testing.T) {
	cfg, app := newChannelsCommandTestApp(t)

	originalStarter := startBackgroundBackupLoops
	defer func() { startBackgroundBackupLoops = originalStarter }()
	startCalls := 0
	startBackgroundBackupLoops = func(ctx context.Context, cfg config.Config, app *runtime.App) {
		startCalls++
	}

	err := RunChannelsCommand(context.Background(), cfg, app, []string{"serve", "--once"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "channels.signal.inbound.enabled must be true") {
		t.Fatalf("expected inbound-enabled validation error, got %v", err)
	}
	if startCalls != 0 {
		t.Fatalf("expected backup loops not to start for channels serve --once, got %d", startCalls)
	}
}

func newChannelsCommandTestApp(t *testing.T) (config.Config, *runtime.App) {
	t.Helper()

	cfg := config.Default()
	cfg.App.Root = t.TempDir()
	cfg.Channels.Enabled = []string{"signal"}
	cfg.Channels.Signal.Inbound.Enabled = false
	cfg.Cron.Enabled = false
	cfg.Heartbeat.Enabled = false

	app, err := runtime.New(cfg)
	if err != nil {
		t.Fatalf("runtime.New: %v", err)
	}
	return cfg, app
}
