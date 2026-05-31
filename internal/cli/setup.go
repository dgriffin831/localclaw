package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

var errMissingSetupTarget = errors.New("setup target is required")

type setupTarget struct {
	Name       string
	Binary     string
	Args       []string
	PromptMode string
}

var setupTargets = map[string]setupTarget{
	"claude": {
		Name:       "claude",
		Binary:     "claude",
		Args:       []string{"-p", "{prompt}"},
		PromptMode: "arg",
	},
	"codex": {
		Name:       "codex",
		Binary:     "codex",
		Args:       []string{"exec", "--skip-git-repo-check", "-"},
		PromptMode: "stdin",
	},
	"opencode": {
		Name:       "opencode",
		Binary:     "opencode",
		Args:       []string{"run", "{prompt}"},
		PromptMode: "arg",
	},
}

// RunSetupCommand asks a target coding-agent harness to configure LocalClaw MCP.
func RunSetupCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	if len(args) == 0 {
		return errMissingSetupTarget
	}
	targetName := strings.ToLower(strings.TrimSpace(args[0]))
	target, ok := setupTargets[targetName]
	if !ok {
		return fmt.Errorf("unknown setup target %q (supported: claude, codex, opencode)", args[0])
	}

	fs := flag.NewFlagSet("setup "+targetName, flag.ContinueOnError)
	fs.SetOutput(stderr)
	dryRun := fs.Bool("dry-run", false, "print setup prompt without invoking harness")
	binary := fs.String("binary", target.Binary, "harness binary path")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("setup %s does not accept positional arguments", targetName)
	}

	prompt := BuildSetupPrompt(target.Name)
	if *dryRun {
		fmt.Fprintln(stdout, prompt)
		return nil
	}

	resolvedBinary, err := resolveHarnessBinary(*binary)
	if err != nil {
		return err
	}
	target.Binary = resolvedBinary
	return invokeSetupHarness(ctx, target, prompt, stdout, stderr)
}

func BuildSetupPrompt(target string) string {
	name := strings.TrimSpace(target)
	if name == "" {
		name = "this coding agent"
	}
	return fmt.Sprintf(`You are configuring %s to use a local stdio MCP server named "localclaw".

Goal:
- Add or update the MCP server named "localclaw".
- Configure it with command "localclaw" and args ["mcp", "serve"].
- Use the current supported configuration mechanism for this harness/version.
- Preserve unrelated configuration exactly.
- Do not remove or rewrite unrelated MCP servers.
- If there is a built-in MCP management command, prefer that over manual file edits.
- After configuration, verify or explain how to verify that the "localclaw" MCP server is available.

Return a concise summary of what changed and any verification result.`, name)
}

func resolveHarnessBinary(binary string) (string, error) {
	trimmed := strings.TrimSpace(binary)
	if trimmed == "" {
		return "", errors.New("harness binary is required")
	}
	if strings.ContainsAny(trimmed, `/\`) {
		if _, err := os.Stat(trimmed); err != nil {
			return "", fmt.Errorf("harness binary %q is not available: %w", trimmed, err)
		}
		return trimmed, nil
	}
	resolved, err := exec.LookPath(trimmed)
	if err != nil {
		return "", fmt.Errorf("harness binary %q is not available: %w", trimmed, err)
	}
	return resolved, nil
}

func invokeSetupHarness(ctx context.Context, target setupTarget, prompt string, stdout, stderr io.Writer) error {
	args := expandPromptArgs(target.Args, prompt)
	cmd := exec.CommandContext(ctx, target.Binary, args...)
	if target.PromptMode == "stdin" {
		cmd.Stdin = strings.NewReader(prompt)
	}

	var combined bytes.Buffer
	cmd.Stdout = io.MultiWriter(stdout, &combined)
	cmd.Stderr = io.MultiWriter(stderr, &combined)
	if err := cmd.Run(); err != nil {
		output := strings.TrimSpace(combined.String())
		if output != "" {
			return fmt.Errorf("setup %s harness failed: %w: %s", target.Name, err, output)
		}
		return fmt.Errorf("setup %s harness failed: %w", target.Name, err)
	}
	return nil
}

func expandPromptArgs(args []string, prompt string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		out = append(out, strings.ReplaceAll(arg, "{prompt}", prompt))
	}
	return out
}
