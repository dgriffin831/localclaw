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

func TestResolveEffectiveMCPConfigPathIsolatedHome(t *testing.T) {
	isolated := filepath.Join(t.TempDir(), "isolated-home")
	client := NewClient(Settings{
		BinaryPath: "codex",
		MCP: MCPSettings{
			UseIsolatedHome: true,
			HomePath:        isolated,
		},
	})

	path, env, err := client.resolveEffectiveMCPConfigPath()
	if err != nil {
		t.Fatalf("resolveEffectiveMCPConfigPath: %v", err)
	}
	wantPath := filepath.Join(isolated, "config.toml")
	if path != wantPath {
		t.Fatalf("expected isolated config path %q, got %q", wantPath, path)
	}
	if env["CODEX_HOME"] != isolated {
		t.Fatalf("expected CODEX_HOME=%q, got %q", isolated, env["CODEX_HOME"])
	}
}

func TestResolveEffectiveMCPConfigPathNormalizesConfiguredAndIsolatedPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	t.Run("explicit path resolves tilde", func(t *testing.T) {
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
	})

	t.Run("isolated home resolves tilde", func(t *testing.T) {
		client := NewClient(Settings{
			BinaryPath: "codex",
			MCP: MCPSettings{
				UseIsolatedHome: true,
				HomePath:        "~/.isolated-codex",
			},
		})
		path, env, err := client.resolveEffectiveMCPConfigPath()
		if err != nil {
			t.Fatalf("resolveEffectiveMCPConfigPath: %v", err)
		}
		wantHome := filepath.Join(home, ".isolated-codex")
		wantPath := filepath.Join(wantHome, "config.toml")
		if path != wantPath {
			t.Fatalf("expected normalized isolated path %q, got %q", wantPath, path)
		}
		if env["CODEX_HOME"] != wantHome {
			t.Fatalf("expected normalized CODEX_HOME=%q, got %q", wantHome, env["CODEX_HOME"])
		}
	})
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
	})
	args := client.buildCommandArgsForRequest(llm.Request{
		Input:         "hello",
		SystemContext: "system",
		SkillPrompt:   "skill",
	})
	want := []string{"exec", "--json", "-C", "/tmp/workspace", "-p", "dev", "-m", "gpt-5-codex", "system\n\nskill\n\nUser input:\nhello"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("unexpected args:\n got: %#v\nwant: %#v", args, want)
	}
}

func TestPromptStreamRequestParsesJSONStream(t *testing.T) {
	tmpDir := t.TempDir()
	codexArgsPath := filepath.Join(tmpDir, "codex-args.txt")
	codexEnvPath := filepath.Join(tmpDir, "codex-env.txt")
	fakeCodexPath := filepath.Join(tmpDir, "codex")
	script := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
printf "%%s\n" "$@" > %q
env | grep '^CODEX_HOME=' > %q || true
printf '%%s\n' '{"type":"item.completed","item":{"type":"agent_message","text":"hello"}}'
printf '%%s\n' '{"type":"item.completed","item":{"type":"agent_message","text":" world"}}'
`, codexArgsPath, codexEnvPath)
	if err := os.WriteFile(fakeCodexPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}

	isolated := filepath.Join(tmpDir, "isolated-home")
	client := NewClient(Settings{
		BinaryPath:       fakeCodexPath,
		WorkingDirectory: "/tmp/workspace",
		MCP: MCPSettings{
			UseIsolatedHome: true,
			HomePath:        isolated,
		},
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

	envPayload, err := os.ReadFile(codexEnvPath)
	if err != nil {
		t.Fatalf("read env capture: %v", err)
	}
	if !strings.Contains(string(envPayload), "CODEX_HOME="+isolated) {
		t.Fatalf("expected CODEX_HOME isolated override, got %q", strings.TrimSpace(string(envPayload)))
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
