package claudecode

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Client defines local Claude Code CLI invocation behavior.
type Client interface {
	Prompt(ctx context.Context, input string) (string, error)
	PromptStream(ctx context.Context, input string) (<-chan StreamEvent, <-chan error)
}

type StreamEventType string

const (
	StreamEventDelta StreamEventType = "delta"
	StreamEventFinal StreamEventType = "final"
)

type StreamEvent struct {
	Type StreamEventType
	Text string
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
	events, errs := c.PromptStream(ctx, input)
	var streamed strings.Builder
	final := ""

	for events != nil || errs != nil {
		select {
		case evt, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if evt.Type == StreamEventDelta {
				streamed.WriteString(evt.Text)
				continue
			}
			if evt.Type == StreamEventFinal {
				final = strings.TrimSpace(evt.Text)
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err != nil {
				return "", err
			}
		}
	}

	if final != "" {
		return final, nil
	}
	return strings.TrimSpace(streamed.String()), nil
}

func (c *LocalClient) PromptStream(ctx context.Context, input string) (<-chan StreamEvent, <-chan error) {
	if strings.TrimSpace(input) == "" {
		events := make(chan StreamEvent)
		errs := make(chan error, 1)
		defer close(events)
		defer close(errs)
		errs <- fmt.Errorf("input is required")
		return events, errs
	}

	cmd := exec.CommandContext(ctx, c.settings.BinaryPath, "-p", input)
	cmd.Env = append(os.Environ(), c.buildEnv()...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		events := make(chan StreamEvent)
		errs := make(chan error, 1)
		defer close(events)
		defer close(errs)
		errs <- fmt.Errorf("create stdout pipe: %w", err)
		return events, errs
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		events := make(chan StreamEvent)
		errs := make(chan error, 1)
		defer close(events)
		defer close(errs)
		errs <- fmt.Errorf("create stderr pipe: %w", err)
		return events, errs
	}
	if err := cmd.Start(); err != nil {
		events := make(chan StreamEvent)
		errs := make(chan error, 1)
		defer close(events)
		defer close(errs)
		errs <- fmt.Errorf("start claude code cli: %w", err)
		return events, errs
	}

	events := make(chan StreamEvent, 32)
	errs := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errs)

		stderrTextCh := make(chan string, 1)
		go func() {
			data, _ := io.ReadAll(stderr)
			stderrTextCh <- strings.TrimSpace(string(data))
		}()

		reader := bufio.NewReader(stdout)
		buf := make([]byte, 1024)
		var full strings.Builder
		for {
			n, readErr := reader.Read(buf)
			if n > 0 {
				chunk := string(buf[:n])
				full.WriteString(chunk)
				if !emitEvent(ctx, events, StreamEvent{Type: StreamEventDelta, Text: chunk}) {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
					return
				}
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				emitError(ctx, errs, fmt.Errorf("read claude code output: %w", readErr))
				return
			}
		}

		waitErr := cmd.Wait()
		stderrText := <-stderrTextCh
		if waitErr != nil {
			if stderrText != "" {
				emitError(ctx, errs, fmt.Errorf("claude code cli execution failed: %w: %s", waitErr, stderrText))
				return
			}
			emitError(ctx, errs, fmt.Errorf("claude code cli execution failed: %w", waitErr))
			return
		}

		finalText := strings.TrimSpace(full.String())
		emitEvent(ctx, events, StreamEvent{Type: StreamEventFinal, Text: finalText})
	}()

	return events, errs
}

func emitEvent(ctx context.Context, events chan<- StreamEvent, evt StreamEvent) bool {
	select {
	case events <- evt:
		return true
	case <-ctx.Done():
		return false
	}
}

func emitError(ctx context.Context, errs chan<- error, err error) {
	select {
	case errs <- err:
	case <-ctx.Done():
	}
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
