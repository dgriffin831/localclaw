package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dgriffin831/localclaw/internal/channels/signal"
)

type SignalInboundRunOptions struct {
	Once         bool
	ErrorBackoff time.Duration
	Logf         func(format string, args ...interface{})
}

func (a *App) RunSignalInbound(ctx context.Context, opts SignalInboundRunOptions) error {
	if !a.channelEnabled("signal") || a.signal == nil {
		return disabledChannelError("signal")
	}
	if a.signalReceive == nil {
		return errors.New("signal receive adapter is unavailable")
	}
	inboundCfg := a.cfg.Channels.Signal.Inbound
	if !inboundCfg.Enabled {
		return errors.New("channels.signal.inbound.enabled must be true to run signal inbound")
	}

	allowFrom := normalizeInboundAllowList(inboundCfg.AllowFrom)
	if len(allowFrom) == 0 {
		return errors.New("channels.signal.inbound.allow_from must include at least one sender")
	}
	agentBySender := normalizeInboundAgentBySender(inboundCfg.AgentBySender)
	defaultAgent := strings.TrimSpace(inboundCfg.DefaultAgent)
	if defaultAgent == "" {
		defaultAgent = DefaultAgentID
	}

	receiveSettings := signal.ReceiveSettings{
		CLIPath:            a.cfg.Channels.Signal.CLIPath,
		Account:            a.cfg.Channels.Signal.Account,
		Timeout:            time.Duration(inboundCfg.PollTimeoutSeconds) * time.Second,
		MaxMessagesPerPoll: inboundCfg.MaxMessagesPerPoll,
		IgnoreAttachments:  true,
		IgnoreStories:      true,
	}
	if receiveSettings.Timeout <= 0 {
		receiveSettings.Timeout = 5 * time.Second
	}
	if receiveSettings.MaxMessagesPerPoll <= 0 {
		receiveSettings.MaxMessagesPerPoll = 10
	}

	errorBackoff := opts.ErrorBackoff
	if errorBackoff <= 0 {
		errorBackoff = time.Second
	}

	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		messages, err := a.signalReceive(ctx, receiveSettings)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			inboundLogf(opts.Logf, "signal inbound receive error: %v", err)
			if opts.Once {
				return err
			}
			if !sleepWithContext(ctx, errorBackoff) {
				return nil
			}
			continue
		}
		for _, message := range messages {
			if err := a.processSignalInboundMessage(ctx, message, allowFrom, agentBySender, defaultAgent, opts.Logf); err != nil {
				inboundLogf(opts.Logf, "signal inbound processing error: %v", err)
			}
		}
		if opts.Once {
			return nil
		}
	}
}

func (a *App) processSignalInboundMessage(ctx context.Context, message signal.ReceiveMessage, allowFrom map[string]struct{}, agentBySender map[string]string, defaultAgent string, logf func(format string, args ...interface{})) error {
	sender := normalizeSignalSenderForInbound(message.Sender)
	if sender == "" {
		return nil
	}
	if message.IsSync {
		return nil
	}
	if message.IsGroup {
		inboundLogf(logf, "signal inbound dropped group message sender=%s group_id=%s", sender, strings.TrimSpace(message.GroupID))
		return nil
	}
	if _, ok := allowFrom[sender]; !ok {
		inboundLogf(logf, "signal inbound dropped non-allowlisted sender=%s", sender)
		return nil
	}

	body := strings.TrimSpace(message.Text)
	if body == "" {
		return nil
	}

	agentID := defaultAgent
	if mappedAgent, ok := agentBySender[sender]; ok {
		agentID = mappedAgent
	}
	if strings.TrimSpace(agentID) == "" {
		agentID = DefaultAgentID
	}
	sessionID := signalSessionIDForSender(sender)

	inboundLogf(logf, "signal inbound accepted sender=%s agent=%s session=%s", sender, agentID, sessionID)
	response, err := a.PromptForSession(ctx, agentID, sessionID, body)
	if err != nil {
		return fmt.Errorf("prompt sender=%s agent=%s session=%s: %w", sender, agentID, sessionID, err)
	}
	response = strings.TrimSpace(response)
	if response == "" {
		inboundLogf(logf, "signal inbound produced empty response sender=%s", sender)
		return nil
	}

	if _, err := a.MCPSignalSend(ctx, response, sender, agentID, sessionID); err != nil {
		return fmt.Errorf("send response sender=%s agent=%s session=%s: %w", sender, agentID, sessionID, err)
	}
	inboundLogf(logf, "signal inbound replied sender=%s agent=%s", sender, agentID)
	return nil
}

func normalizeInboundAllowList(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := normalizeSignalSenderForInbound(value)
		if normalized == "" {
			continue
		}
		out[normalized] = struct{}{}
	}
	return out
}

func normalizeInboundAgentBySender(values map[string]string) map[string]string {
	out := make(map[string]string, len(values))
	for rawSender, rawAgent := range values {
		sender := normalizeSignalSenderForInbound(rawSender)
		agent := strings.TrimSpace(rawAgent)
		if sender == "" || agent == "" {
			continue
		}
		out[sender] = agent
	}
	return out
}

func normalizeSignalSenderForInbound(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	trimmed = strings.ReplaceAll(trimmed, " ", "")
	trimmed = strings.ReplaceAll(trimmed, "-", "")
	trimmed = strings.ReplaceAll(trimmed, "(", "")
	trimmed = strings.ReplaceAll(trimmed, ")", "")
	if !strings.HasPrefix(trimmed, "+") {
		return ""
	}
	for _, ch := range trimmed[1:] {
		if ch < '0' || ch > '9' {
			return ""
		}
	}
	return trimmed
}

func signalSessionIDForSender(sender string) string {
	trimmed := strings.TrimPrefix(strings.TrimSpace(sender), "+")
	if trimmed == "" {
		return "signal-unknown"
	}
	var b strings.Builder
	b.Grow(len(trimmed) + len("signal-"))
	b.WriteString("signal-")
	for _, ch := range trimmed {
		if ch >= '0' && ch <= '9' {
			b.WriteRune(ch)
			continue
		}
		b.WriteRune('-')
	}
	return b.String()
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func inboundLogf(logf func(format string, args ...interface{}), format string, args ...interface{}) {
	if logf == nil {
		return
	}
	logf(format, args...)
}
