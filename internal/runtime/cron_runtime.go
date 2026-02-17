package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/dgriffin831/localclaw/internal/cron"
	"github.com/dgriffin831/localclaw/internal/llm"
)

func (a *App) runCronEntry(ctx context.Context, entry cron.Entry) cron.RunOutcome {
	agentID := ResolveAgentID(entry.AgentID)
	prompt := strings.TrimSpace(entry.Message)
	if prompt == "" {
		return cron.RunOutcome{Status: cron.RunStatusSkipped, Error: "cron message is blank"}
	}

	sessionID := ""
	switch entry.SessionTarget {
	case cron.SessionTargetDefault:
		sessionID = "default"
	case cron.SessionTargetIsolated:
		sessionID = fmt.Sprintf("cron-%s", entry.ID)
	default:
		return cron.RunOutcome{Status: cron.RunStatusSkipped, Error: fmt.Sprintf("unsupported sessionTarget %q", entry.SessionTarget)}
	}

	_, err := a.PromptForSessionWithOptions(ctx, agentID, sessionID, prompt, llm.PromptOptions{})
	if err != nil {
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			return cron.RunOutcome{Status: cron.RunStatusTimeout, Error: "cron prompt timed out"}
		case errors.Is(ctx.Err(), context.Canceled):
			return cron.RunOutcome{Status: cron.RunStatusCanceled, Error: "cron prompt canceled"}
		default:
			return cron.RunOutcome{Status: cron.RunStatusError, Error: strings.TrimSpace(err.Error())}
		}
	}
	return cron.RunOutcome{Status: cron.RunStatusSuccess}
}
