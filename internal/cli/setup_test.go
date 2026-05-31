package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSetupCommandDryRunPrintsPrompt(t *testing.T) {
	var out bytes.Buffer
	if err := RunSetupCommand(context.Background(), []string{"opencode", "--dry-run"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunSetupCommand dry-run: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, `MCP server named "localclaw"`) {
		t.Fatalf("expected localclaw setup prompt, got %q", got)
	}
	if !strings.Contains(got, `args ["mcp", "serve"]`) {
		t.Fatalf("expected mcp serve args in prompt, got %q", got)
	}
}

func TestRunSetupCommandInvokesClaudeHarnessWithPromptArg(t *testing.T) {
	tmp := t.TempDir()
	argsPath := filepath.Join(tmp, "args.txt")
	fake := writeFakeHarness(t, tmp, "claude", argsPath)

	if err := RunSetupCommand(context.Background(), []string{"claude", "--binary", fake}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunSetupCommand: %v", err)
	}
	args := readFile(t, argsPath)
	if !strings.Contains(args, "-p") {
		t.Fatalf("expected claude -p arg, got %q", args)
	}
	if !strings.Contains(args, "localclaw") || !strings.Contains(args, "mcp") {
		t.Fatalf("expected setup prompt in args, got %q", args)
	}
}

func TestRunSetupCommandInvokesCodexHarnessWithPromptOnStdin(t *testing.T) {
	tmp := t.TempDir()
	argsPath := filepath.Join(tmp, "args.txt")
	stdinPath := filepath.Join(tmp, "stdin.txt")
	fake := writeFakeHarnessWithStdin(t, tmp, "codex", argsPath, stdinPath)

	if err := RunSetupCommand(context.Background(), []string{"codex", "--binary", fake}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunSetupCommand: %v", err)
	}
	args := readFile(t, argsPath)
	if !strings.Contains(args, "exec") || !strings.Contains(args, "-") {
		t.Fatalf("expected codex exec stdin args, got %q", args)
	}
	stdin := readFile(t, stdinPath)
	if !strings.Contains(stdin, "localclaw") || !strings.Contains(stdin, "mcp") {
		t.Fatalf("expected setup prompt on stdin, got %q", stdin)
	}
}

func TestRunSetupCommandInvokesOpencodeRun(t *testing.T) {
	tmp := t.TempDir()
	argsPath := filepath.Join(tmp, "args.txt")
	fake := writeFakeHarness(t, tmp, "opencode", argsPath)

	if err := RunSetupCommand(context.Background(), []string{"opencode", "--binary", fake}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunSetupCommand: %v", err)
	}
	args := readFile(t, argsPath)
	if !strings.Contains(args, "run") {
		t.Fatalf("expected opencode run arg, got %q", args)
	}
	if !strings.Contains(args, "localclaw") || !strings.Contains(args, "mcp") {
		t.Fatalf("expected setup prompt in args, got %q", args)
	}
}

func writeFakeHarness(t *testing.T, dir, name, argsPath string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" > " + shellQuote(argsPath) + "\necho ok\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake harness: %v", err)
	}
	return path
}

func writeFakeHarnessWithStdin(t *testing.T, dir, name, argsPath, stdinPath string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" > " + shellQuote(argsPath) + "\ncat > " + shellQuote(stdinPath) + "\necho ok\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake harness: %v", err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func shellQuote(path string) string {
	return "'" + strings.ReplaceAll(path, "'", "'\\''") + "'"
}
