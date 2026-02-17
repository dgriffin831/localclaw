package signal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Client is the Signal channel boundary.
type Client interface {
	Send(ctx context.Context, req SendRequest) (SendResult, error)
}

type SendRequest struct {
	Text      string
	Recipient string
}

type SendResult struct {
	OK        bool   `json:"ok"`
	Recipient string `json:"recipient"`
	SentAt    string `json:"sent_at"`
}

type Settings struct {
	CLIPath          string
	Account          string
	DefaultRecipient string
	Timeout          time.Duration
	Now              func() time.Time
}

type LocalAdapter struct {
	cliPath          string
	account          string
	defaultRecipient string
	timeout          time.Duration
	now              func() time.Time
}

func NewLocalAdapter(settings Settings) *LocalAdapter {
	cliPath := strings.TrimSpace(settings.CLIPath)
	if cliPath == "" {
		cliPath = "signal-cli"
	}
	timeout := settings.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	now := settings.Now
	if now == nil {
		now = time.Now
	}

	return &LocalAdapter{
		cliPath:          cliPath,
		account:          strings.TrimSpace(settings.Account),
		defaultRecipient: strings.TrimSpace(settings.DefaultRecipient),
		timeout:          timeout,
		now:              now,
	}
}

func (a *LocalAdapter) Send(ctx context.Context, req SendRequest) (SendResult, error) {
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return SendResult{}, errors.New("text is required")
	}
	if strings.TrimSpace(a.account) == "" {
		return SendResult{}, errors.New("signal account is required")
	}

	recipient := strings.TrimSpace(req.Recipient)
	if recipient == "" {
		recipient = a.defaultRecipient
	}
	if recipient == "" {
		return SendResult{}, errors.New("recipient is required")
	}

	args := []string{"-a", a.account, "send", "-m", text}
	if groupID := parseGroupRecipient(recipient); groupID != "" {
		args = append(args, "-g", groupID)
	} else {
		args = append(args, recipient)
	}

	runCtx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, a.cliPath, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		stderrText := strings.TrimSpace(stderr.String())
		if runCtx.Err() != nil {
			if stderrText == "" {
				return SendResult{}, fmt.Errorf("signal-cli send failed: %w", runCtx.Err())
			}
			return SendResult{}, fmt.Errorf("signal-cli send failed: %s: %w", stderrText, runCtx.Err())
		}
		if stderrText == "" {
			stderrText = strings.TrimSpace(stdout.String())
		}
		if stderrText == "" {
			return SendResult{}, fmt.Errorf("signal-cli send failed: %w", err)
		}
		return SendResult{}, fmt.Errorf("signal-cli send failed: %s: %w", stderrText, err)
	}

	return SendResult{
		OK:        true,
		Recipient: recipient,
		SentAt:    a.now().UTC().Format(time.RFC3339Nano),
	}, nil
}

func parseGroupRecipient(value string) string {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	switch {
	case strings.HasPrefix(lower, "signal:group:"):
		return strings.TrimSpace(trimmed[len("signal:group:"):])
	case strings.HasPrefix(lower, "group:"):
		return strings.TrimSpace(trimmed[len("group:"):])
	default:
		return ""
	}
}
