package cron

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type commandRunOutcome struct {
	status       string
	exitCode     *int
	errorMessage string
}

func runLocalCommand(ctx context.Context, command string) commandRunOutcome {
	cmd := exec.CommandContext(ctx, "/bin/sh", "-lc", command)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return commandRunOutcome{status: RunStatusTimeout, errorMessage: "command timed out"}
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return commandRunOutcome{status: RunStatusCanceled, errorMessage: "command canceled"}
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code := exitErr.ExitCode()
			message := strings.TrimSpace(stderr.String())
			if message == "" {
				message = fmt.Sprintf("command exited with code %d", code)
			}
			return commandRunOutcome{status: RunStatusError, exitCode: &code, errorMessage: message}
		}
		message := strings.TrimSpace(err.Error())
		stderrText := strings.TrimSpace(stderr.String())
		if stderrText != "" {
			if message == "" {
				message = stderrText
			} else if !strings.Contains(message, stderrText) {
				message = message + ": " + stderrText
			}
		}
		if message == "" {
			message = "command execution failed"
		}
		return commandRunOutcome{status: RunStatusError, errorMessage: message}
	}
	zero := 0
	return commandRunOutcome{status: RunStatusSuccess, exitCode: &zero}
}
