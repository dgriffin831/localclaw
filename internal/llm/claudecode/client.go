package claudecode

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Client defines local Claude Code CLI invocation behavior.
type Client interface {
	Prompt(ctx context.Context, input string) (string, error)
}

type LocalClient struct {
	binaryPath string
}

func NewClient(binaryPath string) *LocalClient {
	return &LocalClient{binaryPath: binaryPath}
}

func (c *LocalClient) Prompt(ctx context.Context, input string) (string, error) {
	if strings.TrimSpace(input) == "" {
		return "", fmt.Errorf("input is required")
	}

	cmd := exec.CommandContext(ctx, c.binaryPath, "-p", input)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("claude code cli execution failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
