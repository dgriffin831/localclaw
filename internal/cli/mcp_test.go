package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/dgriffin831/localclaw/internal/config"
	mcpTools "github.com/dgriffin831/localclaw/internal/mcp/tools"
	"github.com/dgriffin831/localclaw/internal/runtime"
)

func TestRunMCPCommandRejectsUnknownSubcommand(t *testing.T) {
	cfg := config.Default()
	cfg.App.Root = t.TempDir()
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
	cfg.App.Root = filepath.Join(t.TempDir(), "state")
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

func TestMCPServerExposesFullV1ToolSurface(t *testing.T) {
	home := t.TempDir()
	if err := os.Setenv("HOME", home); err != nil {
		t.Fatalf("set HOME: %v", err)
	}
	cfg := config.Default()
	cfg.App.Root = filepath.Join(t.TempDir(), "state")
	app, err := runtime.New(cfg)
	if err != nil {
		t.Fatalf("runtime.New error: %v", err)
	}
	server, err := newMCPServer(app)
	if err != nil {
		t.Fatalf("newMCPServer error: %v", err)
	}

	input := bytes.NewBufferString("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\",\"params\":{}}\n")
	var out bytes.Buffer
	if err := server.Serve(context.Background(), input, &out); err != nil {
		t.Fatalf("Serve error: %v", err)
	}

	var resp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.NewDecoder(&out).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	names := make([]string, 0, len(resp.Result.Tools))
	for _, tool := range resp.Result.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	want := []string{
		"localclaw_cron_add",
		"localclaw_cron_list",
		"localclaw_cron_remove",
		"localclaw_cron_run",
		"localclaw_memory_get",
		"localclaw_memory_grep",
		"localclaw_memory_search",
		"localclaw_session_status",
		"localclaw_sessions_delete",
		"localclaw_sessions_history",
		"localclaw_sessions_list",
		"localclaw_signal_send",
		"localclaw_slack_send",
		"localclaw_workspace_status",
	}
	if len(names) != len(want) {
		t.Fatalf("unexpected tool count %d: %v", len(names), names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("unexpected tools list\nwant=%v\ngot=%v", want, names)
		}
	}
}

func TestMCPServerDispatchesPhase4ToolsByName(t *testing.T) {
	home := t.TempDir()
	if err := os.Setenv("HOME", home); err != nil {
		t.Fatalf("set HOME: %v", err)
	}
	cfg := config.Default()
	cfg.App.Root = filepath.Join(t.TempDir(), "state")
	cfg.Cron.Enabled = true
	app, err := runtime.New(cfg)
	if err != nil {
		t.Fatalf("runtime.New error: %v", err)
	}
	server, err := newMCPServer(app)
	if err != nil {
		t.Fatalf("newMCPServer error: %v", err)
	}

	cases := []struct {
		name string
		args string
	}{
		{name: "localclaw_workspace_status", args: "{}"},
		{name: "localclaw_cron_list", args: "{}"},
		{name: "localclaw_sessions_list", args: "{}"},
	}
	for _, tc := range cases {
		input := bytes.NewBufferString("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"" + tc.name + "\",\"arguments\":" + tc.args + "}}\n")
		var out bytes.Buffer
		if err := server.Serve(context.Background(), input, &out); err != nil {
			t.Fatalf("Serve error for %s: %v", tc.name, err)
		}
		var resp struct {
			Result struct {
				StructuredContent map[string]interface{} `json:"structuredContent"`
			} `json:"result"`
		}
		if err := json.NewDecoder(&out).Decode(&resp); err != nil {
			t.Fatalf("decode response for %s: %v", tc.name, err)
		}
		if len(resp.Result.StructuredContent) == 0 {
			t.Fatalf("expected structured content for %s", tc.name)
		}
		if ok, _ := resp.Result.StructuredContent["ok"].(bool); !ok {
			t.Fatalf("expected ok=true for %s, got %v", tc.name, resp.Result.StructuredContent)
		}
	}
}

func TestMCPServerAppliesToolPolicyDenials(t *testing.T) {
	home := t.TempDir()
	if err := os.Setenv("HOME", home); err != nil {
		t.Fatalf("set HOME: %v", err)
	}
	cfg := config.Default()
	cfg.App.Root = filepath.Join(t.TempDir(), "state")
	app, err := runtime.New(cfg)
	if err != nil {
		t.Fatalf("runtime.New error: %v", err)
	}
	policy, err := mcpTools.NewPolicy(nil, []string{"localclaw_cron_list"})
	if err != nil {
		t.Fatalf("NewPolicy error: %v", err)
	}
	server, err := newMCPServerWithPolicy(app, policy)
	if err != nil {
		t.Fatalf("newMCPServerWithPolicy error: %v", err)
	}

	input := bytes.NewBufferString("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"localclaw_cron_list\",\"arguments\":{}}}\n")
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
		t.Fatalf("expected policy denied tool call to return isError=true, got %+v", resp.Result)
	}
	message, _ := resp.Result.StructuredContent["error"].(string)
	if !strings.Contains(message, "denied by policy") {
		t.Fatalf("expected policy denial message, got %q", message)
	}
}
