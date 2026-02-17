package signal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Client is the Signal channel boundary.
type Client interface {
	Send(ctx context.Context, req SendRequest) (SendResult, error)
}

type TypingClient interface {
	SendTyping(ctx context.Context, req TypingRequest) error
}

type ReceiptClient interface {
	SendReceipt(ctx context.Context, req ReceiptRequest) error
}

type SendRequest struct {
	Text      string
	Recipient string
}

type TypingRequest struct {
	Recipient string
	Stop      bool
}

type ReceiptType string

const (
	ReceiptTypeRead   ReceiptType = "read"
	ReceiptTypeViewed ReceiptType = "viewed"
)

type ReceiptRequest struct {
	Recipient       string
	TargetTimestamp int64
	Type            ReceiptType
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
	if err := a.runCommand(ctx, args, "send"); err != nil {
		return SendResult{}, err
	}

	return SendResult{
		OK:        true,
		Recipient: recipient,
		SentAt:    a.now().UTC().Format(time.RFC3339Nano),
	}, nil
}

func (a *LocalAdapter) SendTyping(ctx context.Context, req TypingRequest) error {
	if strings.TrimSpace(a.account) == "" {
		return errors.New("signal account is required")
	}
	recipient := strings.TrimSpace(req.Recipient)
	if recipient == "" {
		recipient = a.defaultRecipient
	}
	if recipient == "" {
		return errors.New("recipient is required")
	}

	args := []string{"-a", a.account, "sendTyping"}
	if req.Stop {
		args = append(args, "-s")
	}
	if groupID := parseGroupRecipient(recipient); groupID != "" {
		args = append(args, "-g", groupID)
	} else {
		args = append(args, recipient)
	}

	return a.runCommand(ctx, args, "sendTyping")
}

func (a *LocalAdapter) SendReceipt(ctx context.Context, req ReceiptRequest) error {
	if strings.TrimSpace(a.account) == "" {
		return errors.New("signal account is required")
	}
	recipient := strings.TrimSpace(req.Recipient)
	if recipient == "" {
		recipient = a.defaultRecipient
	}
	if recipient == "" {
		return errors.New("recipient is required")
	}
	if parseGroupRecipient(recipient) != "" {
		return errors.New("read receipts are only supported for direct recipients")
	}
	if req.TargetTimestamp <= 0 {
		return errors.New("target timestamp must be > 0")
	}

	receiptType := strings.TrimSpace(string(req.Type))
	if receiptType == "" {
		receiptType = string(ReceiptTypeRead)
	}
	switch ReceiptType(receiptType) {
	case ReceiptTypeRead, ReceiptTypeViewed:
	default:
		return fmt.Errorf("unsupported receipt type %q", receiptType)
	}

	args := []string{
		"-a", a.account,
		"sendReceipt",
		"-t", strconv.FormatInt(req.TargetTimestamp, 10),
		"--type", receiptType,
		recipient,
	}
	return a.runCommand(ctx, args, "sendReceipt")
}

func (a *LocalAdapter) runCommand(ctx context.Context, args []string, action string) error {
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
				return fmt.Errorf("signal-cli %s failed: %w", action, runCtx.Err())
			}
			return fmt.Errorf("signal-cli %s failed: %s: %w", action, stderrText, runCtx.Err())
		}
		if stderrText == "" {
			stderrText = strings.TrimSpace(stdout.String())
		}
		if stderrText == "" {
			return fmt.Errorf("signal-cli %s failed: %w", action, err)
		}
		return fmt.Errorf("signal-cli %s failed: %s: %w", action, stderrText, err)
	}
	return nil
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
