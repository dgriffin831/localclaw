package codex

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pelletier/go-toml"

	"github.com/dgriffin831/localclaw/internal/llm"
)

func TestResolveEffectiveMCPConfigPathPrecedence(t *testing.T) {
	t.Run("explicit config path wins", func(t *testing.T) {
		client := NewClient(Settings{
			BinaryPath: "codex",
			MCP: MCPSettings{
				ConfigPath: filepath.Join(t.TempDir(), "explicit.toml"),
			},
		})
		path, env, err := client.resolveEffectiveMCPConfigPath()
		if err != nil {
			t.Fatalf("resolveEffectiveMCPConfigPath: %v", err)
		}
		if path != client.settings.MCP.ConfigPath {
			t.Fatalf("expected explicit path %q, got %q", client.settings.MCP.ConfigPath, path)
		}
		if _, ok := env["CODEX_HOME"]; ok {
			t.Fatalf("did not expect CODEX_HOME override for explicit path")
		}
	})

	t.Run("codex home env path is second", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("CODEX_HOME", home)
		client := NewClient(Settings{BinaryPath: "codex"})
		path, _, err := client.resolveEffectiveMCPConfigPath()
		if err != nil {
			t.Fatalf("resolveEffectiveMCPConfigPath: %v", err)
		}
		want := filepath.Join(home, "config.toml")
		if path != want {
			t.Fatalf("expected CODEX_HOME config path %q, got %q", want, path)
		}
	})

	t.Run("home default path is fallback", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("CODEX_HOME", "")
		client := NewClient(Settings{BinaryPath: "codex"})
		path, _, err := client.resolveEffectiveMCPConfigPath()
		if err != nil {
			t.Fatalf("resolveEffectiveMCPConfigPath: %v", err)
		}
		want := filepath.Join(home, ".codex", "config.toml")
		if path != want {
			t.Fatalf("expected default config path %q, got %q", want, path)
		}
	})
}

func TestResolveEffectiveMCPConfigPathNormalizesConfiguredPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	client := NewClient(Settings{
		BinaryPath: "codex",
		MCP: MCPSettings{
			ConfigPath: "~/.codex/custom.toml",
		},
	})
	path, env, err := client.resolveEffectiveMCPConfigPath()
	if err != nil {
		t.Fatalf("resolveEffectiveMCPConfigPath: %v", err)
	}
	want := filepath.Join(home, ".codex", "custom.toml")
	if path != want {
		t.Fatalf("expected normalized explicit path %q, got %q", want, path)
	}
	if len(env) != 0 {
		t.Fatalf("expected no env overrides for explicit path, got %#v", env)
	}
}

func TestEnsureMCPServerConfigMergesExistingToml(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	original := `model = "gpt-5-codex"

[mcp_servers.github]
command = "gh"
args = ["mcp"]

[mcp_servers.localclaw]
command = "wrong"
args = ["bad"]
`
	if err := os.WriteFile(cfgPath, []byte(original), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	client := NewClient(Settings{
		BinaryPath: "codex",
		MCP: MCPSettings{
			ServerName:       "localclaw",
			ServerBinaryPath: "localclaw",
			ServerArgs:       []string{"mcp", "serve"},
		},
	})
	if err := client.ensureMCPServerConfig(cfgPath); err != nil {
		t.Fatalf("ensureMCPServerConfig: %v", err)
	}

	tree, err := toml.LoadFile(cfgPath)
	if err != nil {
		t.Fatalf("load toml: %v", err)
	}
	if got := tree.Get("model"); got != "gpt-5-codex" {
		t.Fatalf("expected model preserved, got %#v", got)
	}
	if got := tree.Get("mcp_servers.github.command"); got != "gh" {
		t.Fatalf("expected unrelated mcp server preserved, got %#v", got)
	}
	if got := tree.Get("mcp_servers.localclaw.command"); got != "localclaw" {
		t.Fatalf("expected localclaw command normalized, got %#v", got)
	}
	if got := tree.Get("mcp_servers.localclaw.args"); !reflect.DeepEqual(got, []interface{}{"mcp", "serve"}) {
		t.Fatalf("expected localclaw args normalized, got %#v", got)
	}
}

func TestEnsureMCPServerConfigRejectsMalformedTOML(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, []byte("[mcp_servers\ncommand='oops'"), 0o600); err != nil {
		t.Fatalf("write malformed config: %v", err)
	}

	client := NewClient(Settings{BinaryPath: "codex"})
	if err := client.ensureMCPServerConfig(cfgPath); err == nil {
		t.Fatalf("expected malformed TOML error")
	}
}

func TestEnsureMCPServerConfigReturnsWriteError(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("model = 'x'\n"), 0o400); err != nil {
		t.Fatalf("write readonly config: %v", err)
	}

	client := NewClient(Settings{BinaryPath: "codex"})
	if err := client.ensureMCPServerConfig(cfgPath); err == nil {
		t.Fatalf("expected write error")
	}
}

func TestBuildCommandArgsForRequestIncludesExpectedShape(t *testing.T) {
	client := NewClient(Settings{
		BinaryPath:       "codex",
		Profile:          "dev",
		Model:            "gpt-5-codex",
		WorkingDirectory: "/tmp/workspace",
		ExtraArgs:        []string{"--sandbox", "workspace-write"},
	})
	args := client.buildCommandArgsForRequest(llm.Request{
		Input:         "hello",
		SystemContext: "system",
		SkillPrompt:   "skill",
	})
	want := []string{"exec", "--json", "-C", "/tmp/workspace", "-p", "dev", "-m", "gpt-5-codex", "--sandbox", "workspace-write", "-"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("unexpected args:\n got: %#v\nwant: %#v", args, want)
	}
}

func TestBuildCommandArgsForRequestUsesModelOverride(t *testing.T) {
	client := NewClient(Settings{
		BinaryPath:       "codex",
		Profile:          "dev",
		Model:            "gpt-5-codex",
		WorkingDirectory: "/tmp/workspace",
	})
	args := client.buildCommandArgsForRequest(llm.Request{
		Input: "hello",
		Options: llm.PromptOptions{
			ModelOverride: "gpt-5-mini",
		},
	})
	want := []string{"exec", "--json", "-C", "/tmp/workspace", "-p", "dev", "-m", "gpt-5-mini", "-"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("unexpected args:\n got: %#v\nwant: %#v", args, want)
	}
}

func TestBuildCommandArgsForRequestUsesResumeArgsWhenPersistedSessionExists(t *testing.T) {
	client := NewClient(Settings{
		BinaryPath:       "codex",
		Profile:          "dev",
		WorkingDirectory: "/tmp/workspace",
		SessionMode:      "existing",
		ResumeArgs:       []string{"resume", "{sessionId}"},
	})
	args := client.buildCommandArgsForRequest(llm.Request{
		Input: "hello",
		Session: llm.SessionMetadata{
			ProviderSessionID: "thread-42",
		},
	})
	wantPrefix := []string{"exec", "resume", "thread-42"}
	if len(args) < len(wantPrefix) {
		t.Fatalf("unexpected short args for resume path: %v", args)
	}
	for i := range wantPrefix {
		if args[i] != wantPrefix[i] {
			t.Fatalf("unexpected resume args prefix[%d]: got %q want %q (all=%v)", i, args[i], wantPrefix[i], args)
		}
	}
	for _, arg := range args {
		if arg == "-C" {
			t.Fatalf("did not expect -C in resume args: %v", args)
		}
		if arg == "-p" {
			t.Fatalf("did not expect -p in resume args: %v", args)
		}
	}
	if args[len(args)-1] != "-" {
		t.Fatalf("expected stdin prompt marker '-' in resume args: %v", args)
	}
}

func TestBuildCommandArgsForRequestResumeTextOutputOmitsJSONFlag(t *testing.T) {
	client := NewClient(Settings{
		BinaryPath:       "codex",
		WorkingDirectory: "/tmp/workspace",
		SessionMode:      "existing",
		ResumeArgs:       []string{"resume", "{sessionId}"},
		ResumeOutput:     "text",
	})
	args := client.buildCommandArgsForRequest(llm.Request{
		Input: "hello",
		Session: llm.SessionMetadata{
			ProviderSessionID: "thread-42",
		},
	})
	for _, arg := range args {
		if arg == "--json" {
			t.Fatalf("expected --json to be omitted for resume text output mode, got %v", args)
		}
	}
}

func TestPromptStreamRequestParsesJSONStream(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CODEX_HOME", "")
	codexArgsPath := filepath.Join(tmpDir, "codex-args.txt")
	codexEnvPath := filepath.Join(tmpDir, "codex-env.txt")
	codexStdinPath := filepath.Join(tmpDir, "codex-stdin.txt")
	fakeCodexPath := filepath.Join(tmpDir, "codex")
	script := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
printf "%%s\n" "$@" > %q
env | grep '^CODEX_HOME=' > %q || true
cat > %q
printf '%%s\n' '{"type":"item.completed","item":{"type":"agent_message","text":"hello"}}'
printf '%%s\n' '{"type":"item.completed","item":{"type":"agent_message","text":" world"}}'
`, codexArgsPath, codexEnvPath, codexStdinPath)
	if err := os.WriteFile(fakeCodexPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}

	client := NewClient(Settings{
		BinaryPath:       fakeCodexPath,
		WorkingDirectory: "/tmp/workspace",
	})

	events, errs := client.PromptStreamRequest(context.Background(), llm.Request{Input: "prompt"})
	var deltas []string
	final := ""
	for events != nil || errs != nil {
		select {
		case evt, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if evt.Type == llm.StreamEventDelta {
				deltas = append(deltas, evt.Text)
			}
			if evt.Type == llm.StreamEventFinal {
				final = evt.Text
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err != nil {
				t.Fatalf("unexpected prompt error: %v", err)
			}
		}
	}
	if strings.Join(deltas, "") != "hello world" {
		t.Fatalf("unexpected deltas: %v", deltas)
	}
	if final != "hello world" {
		t.Fatalf("unexpected final: %q", final)
	}

	argsFile, err := os.Open(codexArgsPath)
	if err != nil {
		t.Fatalf("open args file: %v", err)
	}
	defer argsFile.Close()
	var args []string
	scanner := bufio.NewScanner(argsFile)
	for scanner.Scan() {
		args = append(args, strings.TrimSpace(scanner.Text()))
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan args file: %v", err)
	}
	wantPrefix := []string{"exec", "--json", "-C", "/tmp/workspace"}
	if len(args) < len(wantPrefix) {
		t.Fatalf("args shorter than expected prefix: %v", args)
	}
	for i := range wantPrefix {
		if args[i] != wantPrefix[i] {
			t.Fatalf("unexpected args prefix[%d]: got %q want %q (all=%v)", i, args[i], wantPrefix[i], args)
		}
	}
	if args[len(args)-1] != "-" {
		t.Fatalf("expected codex prompt arg to use stdin marker '-', got %v", args)
	}

	stdinPayload, err := os.ReadFile(codexStdinPath)
	if err != nil {
		t.Fatalf("read stdin capture: %v", err)
	}
	if strings.TrimSpace(string(stdinPayload)) != "prompt" {
		t.Fatalf("expected prompt text via stdin, got %q", strings.TrimSpace(string(stdinPayload)))
	}

	envPayload, err := os.ReadFile(codexEnvPath)
	if err != nil {
		t.Fatalf("read env capture: %v", err)
	}
	gotEnv := strings.TrimSpace(string(envPayload))
	if gotEnv != "" && gotEnv != "CODEX_HOME=" {
		t.Fatalf("expected no non-empty CODEX_HOME override, got %q", gotEnv)
	}
}

func TestPromptStreamRequestReturnsErrorWhenBinaryMissing(t *testing.T) {
	client := NewClient(Settings{BinaryPath: filepath.Join(t.TempDir(), "does-not-exist")})
	events, errs := client.PromptStreamRequest(context.Background(), llm.Request{Input: "hello"})
	if _, ok := <-events; ok {
		t.Fatalf("expected closed events channel")
	}
	err := <-errs
	if err == nil || !strings.Contains(err.Error(), "start codex cli") {
		t.Fatalf("expected start codex cli error, got %v", err)
	}
}

func TestParseStreamJSONLineCommandExecutionStarted(t *testing.T) {
	line := `{"type":"item.started","item":{"type":"command_execution","id":"cmd_1","command":"ls -la"}}`
	events, err := parseStreamJSONLine(line, nil)
	if err != nil {
		t.Fatalf("parse stream line: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	if events[0].Type != llm.StreamEventToolCall {
		t.Fatalf("expected tool_call event, got %q", events[0].Type)
	}
	if events[0].ToolCall == nil {
		t.Fatalf("expected tool call payload")
	}
	if events[0].ToolCall.ID != "cmd_1" {
		t.Fatalf("expected call id cmd_1, got %q", events[0].ToolCall.ID)
	}
	if events[0].ToolCall.Name != "command_execution" {
		t.Fatalf("expected command_execution tool name, got %q", events[0].ToolCall.Name)
	}
	if events[0].ToolCall.Class != llm.ToolClassDelegated {
		t.Fatalf("expected delegated tool class, got %q", events[0].ToolCall.Class)
	}
	if got, _ := events[0].ToolCall.Args["command"].(string); got != "ls -la" {
		t.Fatalf("expected command arg ls -la, got %#v", events[0].ToolCall.Args["command"])
	}
}

func TestParseStreamJSONLineWebSearchStarted(t *testing.T) {
	line := `{"type":"item.started","item":{"id":"ws_123","type":"web_search","query":"","action":{"type":"other"}}}`
	events, err := parseStreamJSONLine(line, nil)
	if err != nil {
		t.Fatalf("parse stream line: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	if events[0].Type != llm.StreamEventToolCall {
		t.Fatalf("expected tool_call event, got %q", events[0].Type)
	}
	if events[0].ToolCall == nil {
		t.Fatalf("expected tool call payload")
	}
	if events[0].ToolCall.ID != "ws_123" {
		t.Fatalf("expected call id ws_123, got %q", events[0].ToolCall.ID)
	}
	if events[0].ToolCall.Name != "web_search" {
		t.Fatalf("expected web_search tool name, got %q", events[0].ToolCall.Name)
	}
	if events[0].ToolCall.Class != llm.ToolClassDelegated {
		t.Fatalf("expected delegated tool class, got %q", events[0].ToolCall.Class)
	}
	if got, _ := events[0].ToolCall.Args["query"].(string); got != "" {
		t.Fatalf("expected query arg to be empty string, got %#v", events[0].ToolCall.Args["query"])
	}
	if _, ok := events[0].ToolCall.Args["action"]; !ok {
		t.Fatalf("expected action arg in tool call args")
	}
}

func TestParseStreamJSONLineCommandExecutionCompleted(t *testing.T) {
	line := `{"type":"item.completed","item":{"type":"command_execution","id":"cmd_1","command":"ls -la","status":"completed","exit_code":0,"aggregated_output":"file.txt"}}`
	events, err := parseStreamJSONLine(line, nil)
	if err != nil {
		t.Fatalf("parse stream line: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	if events[0].Type != llm.StreamEventToolResult {
		t.Fatalf("expected tool_result event, got %q", events[0].Type)
	}
	if events[0].ToolResult == nil {
		t.Fatalf("expected tool result payload")
	}
	if events[0].ToolResult.CallID != "cmd_1" {
		t.Fatalf("expected call id cmd_1, got %q", events[0].ToolResult.CallID)
	}
	if !events[0].ToolResult.OK {
		t.Fatalf("expected successful command result")
	}
	if events[0].ToolResult.Status != "completed" {
		t.Fatalf("expected status completed, got %q", events[0].ToolResult.Status)
	}
	if got, _ := events[0].ToolResult.Data["aggregated_output"].(string); got != "file.txt" {
		t.Fatalf("expected aggregated output file.txt, got %#v", events[0].ToolResult.Data["aggregated_output"])
	}
}

func TestParseStreamJSONLineWebSearchCompleted(t *testing.T) {
	line := `{"type":"item.completed","item":{"id":"ws_123","type":"web_search","query":"OpenClaw news","action":{"type":"search","query":"OpenClaw news","queries":["OpenClaw news"]}}}`
	events, err := parseStreamJSONLine(line, nil)
	if err != nil {
		t.Fatalf("parse stream line: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	if events[0].Type != llm.StreamEventToolResult {
		t.Fatalf("expected tool_result event, got %q", events[0].Type)
	}
	if events[0].ToolResult == nil {
		t.Fatalf("expected tool result payload")
	}
	if events[0].ToolResult.CallID != "ws_123" {
		t.Fatalf("expected call id ws_123, got %q", events[0].ToolResult.CallID)
	}
	if events[0].ToolResult.Tool != "web_search" {
		t.Fatalf("expected web_search tool result name, got %q", events[0].ToolResult.Tool)
	}
	if !events[0].ToolResult.OK {
		t.Fatalf("expected successful web_search result")
	}
	if got, _ := events[0].ToolResult.Data["query"].(string); got != "OpenClaw news" {
		t.Fatalf("expected query in tool result data, got %#v", events[0].ToolResult.Data["query"])
	}
}

func TestParseStreamJSONLineMCPToolCallEvents(t *testing.T) {
	startLine := `{"type":"item.started","item":{"id":"item_2","type":"mcp_tool_call","server":"localclaw","tool":"localclaw_workspace_status","arguments":{"agent_id":"default"},"status":"in_progress"}}`
	completeLine := `{"type":"item.completed","item":{"id":"item_2","type":"mcp_tool_call","server":"localclaw","tool":"localclaw_workspace_status","arguments":{"agent_id":"default"},"result":{"structured_content":{"ok":true}},"status":"completed"}}`

	startEvents, err := parseStreamJSONLine(startLine, nil)
	if err != nil {
		t.Fatalf("parse mcp started line: %v", err)
	}
	if len(startEvents) != 1 || startEvents[0].Type != llm.StreamEventToolCall || startEvents[0].ToolCall == nil {
		t.Fatalf("expected mcp started line to emit tool_call, got %#v", startEvents)
	}
	if startEvents[0].ToolCall.Name != "localclaw_workspace_status" {
		t.Fatalf("expected mcp tool name localclaw_workspace_status, got %q", startEvents[0].ToolCall.Name)
	}

	completeEvents, err := parseStreamJSONLine(completeLine, nil)
	if err != nil {
		t.Fatalf("parse mcp completed line: %v", err)
	}
	if len(completeEvents) != 1 || completeEvents[0].Type != llm.StreamEventToolResult || completeEvents[0].ToolResult == nil {
		t.Fatalf("expected mcp completed line to emit tool_result, got %#v", completeEvents)
	}
	if completeEvents[0].ToolResult.Tool != "localclaw_workspace_status" {
		t.Fatalf("expected mcp tool result name localclaw_workspace_status, got %q", completeEvents[0].ToolResult.Tool)
	}
	if !completeEvents[0].ToolResult.OK {
		t.Fatalf("expected mcp completed result to be OK")
	}
}

func TestParseStreamJSONLineEmitsProviderSessionMetadata(t *testing.T) {
	line := `{"type":"session.configured","model":"gpt-5-codex","thread_id":"thread-123","tools":["mcp__localclaw__memory_search"]}`
	events, err := parseStreamJSONLine(line, nil)
	if err != nil {
		t.Fatalf("parse stream line: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	if events[0].Type != llm.StreamEventProviderMetadata || events[0].ProviderMetadata == nil {
		t.Fatalf("expected provider metadata event, got %#v", events[0])
	}
	if events[0].ProviderMetadata.Provider != "codex" {
		t.Fatalf("expected codex provider, got %q", events[0].ProviderMetadata.Provider)
	}
	if events[0].ProviderMetadata.SessionID != "thread-123" {
		t.Fatalf("expected thread id in provider metadata, got %q", events[0].ProviderMetadata.SessionID)
	}
}

func TestParseStreamJSONLineCommandExecutionFailed(t *testing.T) {
	line := `{"type":"item.completed","item":{"type":"command_execution","id":"cmd_1","command":"ls -la","status":"failed","exit_code":2,"aggregated_output":"permission denied"}}`
	events, err := parseStreamJSONLine(line, nil)
	if err != nil {
		t.Fatalf("parse stream line: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	if events[0].Type != llm.StreamEventToolResult {
		t.Fatalf("expected tool_result event, got %q", events[0].Type)
	}
	if events[0].ToolResult == nil {
		t.Fatalf("expected tool result payload")
	}
	if events[0].ToolResult.OK {
		t.Fatalf("expected failed command result")
	}
	if strings.TrimSpace(events[0].ToolResult.Error) == "" {
		t.Fatalf("expected non-empty tool result error for failed command")
	}
}
