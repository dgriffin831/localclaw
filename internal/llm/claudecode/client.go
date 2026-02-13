package claudecode

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Client defines local Claude Code CLI invocation behavior.
type Client interface {
	Prompt(ctx context.Context, input string) (string, error)
}

type Settings struct {
	BinaryPath    string
	Profile       string
	UseGovCloud   bool
	BedrockRegion string
	AuthMode      string
}

type LocalClient struct {
	settings Settings
}

func NewClient(settings Settings) *LocalClient {
	return &LocalClient{settings: settings}
}

func (c *LocalClient) Prompt(ctx context.Context, input string) (string, error) {
	if strings.TrimSpace(input) == "" {
		return "", fmt.Errorf("input is required")
	}

	cmd := exec.CommandContext(ctx, c.settings.BinaryPath, "-p", input)
	cmd.Env = append(os.Environ(), c.buildEnv()...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("claude code cli execution failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (c *LocalClient) buildEnv() []string {
	env := []string{}

	if c.settings.Profile != "" {
		env = append(env, "AWS_PROFILE="+c.settings.Profile)
	}
	if c.settings.BedrockRegion != "" {
		env = append(env, "AWS_REGION="+c.settings.BedrockRegion)
		env = append(env, "AWS_DEFAULT_REGION="+c.settings.BedrockRegion)
	}
	if c.settings.UseGovCloud {
		// TODO: replace with exact Claude Code CLI GovCloud knobs once pinned.
		env = append(env, "LOCALCLAW_GOVCLOUD_MODE=1")
	}

	return env
}
