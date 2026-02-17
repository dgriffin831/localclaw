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
