package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/runtime"
)

func TestRunMCPCommandRejectsUnknownSubcommand(t *testing.T) {
	cfg := config.Default()
	cfg.State.Root = t.TempDir()
	app, err := runtime.New(cfg)
	if err != nil {
		t.Fatalf("runtime.New error: %v", err)
	}

	err = RunMCPCommand(context.Background(), cfg, app, []string{"unknown"}, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "unknown mcp subcommand") {
		t.Fatalf("expected unknown mcp subcommand error, got %v", err)
	}
}

func TestRunMCPCommandServeAccepted(t *testing.T) {
	home := t.TempDir()
	if err := os.Setenv("HOME", home); err != nil {
		t.Fatalf("set HOME: %v", err)
	}
	cfg := config.Default()
	cfg.State.Root = filepath.Join(t.TempDir(), "state")
	app, err := runtime.New(cfg)
	if err != nil {
		t.Fatalf("runtime.New error: %v", err)
	}

	if err := RunMCPCommand(context.Background(), cfg, app, []string{"serve"}, bytes.NewBuffer(nil), &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunMCPCommand serve error: %v", err)
	}
}

func TestRunMCPCommandServeRequiresRuntimeApp(t *testing.T) {
	cfg := config.Default()
	err := RunMCPCommand(context.Background(), cfg, nil, []string{"serve"}, bytes.NewBuffer(nil), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "runtime app is required") {
		t.Fatalf("expected runtime app required error, got %v", err)
	}
}

func TestMCPServerAppliesRuntimeToolPolicy(t *testing.T) {
	home := t.TempDir()
	if err := os.Setenv("HOME", home); err != nil {
		t.Fatalf("set HOME: %v", err)
	}
	cfg := config.Default()
	cfg.State.Root = filepath.Join(t.TempDir(), "state")
	cfg.Tools.Deny = []string{"memory_search"}
	app, err := runtime.New(cfg)
	if err != nil {
		t.Fatalf("runtime.New error: %v", err)
	}
	server, err := newMCPServer(app)
	if err != nil {
		t.Fatalf("newMCPServer error: %v", err)
	}

	input := bytes.NewBufferString("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"localclaw_memory_search\",\"arguments\":{\"query\":\"needle\"}}}\n")
	var out bytes.Buffer
	if err := server.Serve(context.Background(), input, &out); err != nil {
		t.Fatalf("Serve error: %v", err)
	}

	var resp struct {
		Result struct {
			IsError           bool                   `json:"isError"`
			StructuredContent map[string]interface{} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.NewDecoder(&out).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Result.IsError {
		t.Fatalf("expected tool policy error response")
	}
	if got := resp.Result.StructuredContent["ok"]; got != false {
		t.Fatalf("expected ok=false, got %v", got)
	}
}
