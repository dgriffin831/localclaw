package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/dgriffin831/localclaw/internal/channels/signal"
	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/llm"
)

func TestRunSignalInboundSkipsGroupMessages(t *testing.T) {
	app := newSignalInboundTestApp(t)
	llmClient := &captureRequestLLMClient{}
	app.llm = llmClient
	app.signalReceive = func(ctx context.Context, settings signal.ReceiveSettings) ([]signal.ReceiveMessage, error) {
		return []signal.ReceiveMessage{
			{Sender: "+15550000001", Text: "group hello", IsGroup: true, GroupID: "g-1"},
		}, nil
	}

	sent := 0
	app.signal = stubSignalClient{sendFn: func(ctx context.Context, req signal.SendRequest) (signal.SendResult, error) {
		sent++
		return signal.SendResult{OK: true, Recipient: req.Recipient}, nil
	}}

	if err := app.RunSignalInbound(context.Background(), SignalInboundRunOptions{Once: true}); err != nil {
		t.Fatalf("run signal inbound: %v", err)
	}
	if sent != 0 {
		t.Fatalf("expected no outbound send for group message, got %d", sent)
	}
	if len(llmClient.requests) != 0 {
		t.Fatalf("expected no llm prompt for group message, got %d", len(llmClient.requests))
	}
}

func TestRunSignalInboundSkipsNonAllowlistedSenders(t *testing.T) {
	app := newSignalInboundTestApp(t)
	llmClient := &captureRequestLLMClient{}
	app.llm = llmClient
	app.signalReceive = func(ctx context.Context, settings signal.ReceiveSettings) ([]signal.ReceiveMessage, error) {
		return []signal.ReceiveMessage{
			{Sender: "+15559999999", Text: "hello"},
		}, nil
	}

	sent := 0
	app.signal = stubSignalClient{sendFn: func(ctx context.Context, req signal.SendRequest) (signal.SendResult, error) {
		sent++
		return signal.SendResult{OK: true, Recipient: req.Recipient}, nil
	}}

	if err := app.RunSignalInbound(context.Background(), SignalInboundRunOptions{Once: true}); err != nil {
		t.Fatalf("run signal inbound: %v", err)
	}
	if sent != 0 {
		t.Fatalf("expected no outbound send for non-allowlisted sender, got %d", sent)
	}
	if len(llmClient.requests) != 0 {
		t.Fatalf("expected no llm prompt for non-allowlisted sender, got %d", len(llmClient.requests))
	}
}

func TestRunSignalInboundRoutesSenderToMappedAgent(t *testing.T) {
	app := newSignalInboundTestApp(t)
	app.cfg.Channels.Signal.Inbound.AgentBySender = map[string]string{
		"+15550000001": "agent-ops",
	}
	app.cfg.Channels.Signal.Inbound.DefaultAgent = "default"

	llmClient := &captureRequestLLMClient{streamFn: func(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, <-chan error) {
		events := make(chan llm.StreamEvent, 1)
		errs := make(chan error)
		events <- llm.StreamEvent{Type: llm.StreamEventFinal, Text: "ack from agent"}
		close(events)
		close(errs)
		return events, errs
	}}
	app.llm = llmClient
	app.signalReceive = func(ctx context.Context, settings signal.ReceiveSettings) ([]signal.ReceiveMessage, error) {
		return []signal.ReceiveMessage{
			{Sender: "+15550000001", Text: "route this"},
		}, nil
	}

	var outbound signal.SendRequest
	app.signal = stubSignalClient{sendFn: func(ctx context.Context, req signal.SendRequest) (signal.SendResult, error) {
		outbound = req
		return signal.SendResult{OK: true, Recipient: req.Recipient}, nil
	}}

	if err := app.RunSignalInbound(context.Background(), SignalInboundRunOptions{Once: true}); err != nil {
		t.Fatalf("run signal inbound: %v", err)
	}
	if len(llmClient.requests) != 1 {
		t.Fatalf("expected 1 llm request, got %d", len(llmClient.requests))
	}
	if llmClient.lastRequest.Session.AgentID != "agent-ops" {
		t.Fatalf("expected routed agent agent-ops, got %q", llmClient.lastRequest.Session.AgentID)
	}
	if outbound.Recipient != "+15550000001" {
		t.Fatalf("expected reply recipient +15550000001, got %q", outbound.Recipient)
	}
	if outbound.Text != "ack from agent" {
		t.Fatalf("expected reply text from llm, got %q", outbound.Text)
	}
}

func TestRunSignalInboundSendsReadReceiptForAcceptedDirectMessage(t *testing.T) {
	app := newSignalInboundTestApp(t)
	app.cfg.Channels.Signal.Inbound.SendReadReceipts = true

	llmClient := &captureRequestLLMClient{streamFn: func(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, <-chan error) {
		events := make(chan llm.StreamEvent, 1)
		errs := make(chan error)
		events <- llm.StreamEvent{Type: llm.StreamEventFinal, Text: "ack"}
		close(events)
		close(errs)
		return events, errs
	}}
	app.llm = llmClient
	app.signalReceive = func(ctx context.Context, settings signal.ReceiveSettings) ([]signal.ReceiveMessage, error) {
		return []signal.ReceiveMessage{
			{Sender: "+15550000001", Text: "hello", Timestamp: 1700000000000},
		}, nil
	}

	var receipts []signal.ReceiptRequest
	app.signal = stubSignalClient{
		sendFn: func(ctx context.Context, req signal.SendRequest) (signal.SendResult, error) {
			return signal.SendResult{OK: true, Recipient: req.Recipient}, nil
		},
		sendReceiptFn: func(ctx context.Context, req signal.ReceiptRequest) error {
			receipts = append(receipts, req)
			return nil
		},
	}

	if err := app.RunSignalInbound(context.Background(), SignalInboundRunOptions{Once: true}); err != nil {
		t.Fatalf("run signal inbound: %v", err)
	}
	if len(receipts) != 1 {
		t.Fatalf("expected one receipt, got %d", len(receipts))
	}
	if receipts[0].Recipient != "+15550000001" {
		t.Fatalf("expected receipt recipient +15550000001, got %q", receipts[0].Recipient)
	}
	if receipts[0].TargetTimestamp != 1700000000000 {
		t.Fatalf("expected receipt timestamp 1700000000000, got %d", receipts[0].TargetTimestamp)
	}
}

func TestRunSignalInboundStartsAndStopsTypingDuringReply(t *testing.T) {
	app := newSignalInboundTestApp(t)
	app.cfg.Channels.Signal.Inbound.SendTyping = true
	app.cfg.Channels.Signal.Inbound.TypingIntervalSeconds = 1

	llmClient := &captureRequestLLMClient{streamFn: func(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, <-chan error) {
		events := make(chan llm.StreamEvent, 1)
		errs := make(chan error, 1)
		go func() {
			time.Sleep(50 * time.Millisecond)
			events <- llm.StreamEvent{Type: llm.StreamEventFinal, Text: "done"}
			close(events)
			close(errs)
		}()
		return events, errs
	}}
	app.llm = llmClient
	app.signalReceive = func(ctx context.Context, settings signal.ReceiveSettings) ([]signal.ReceiveMessage, error) {
		return []signal.ReceiveMessage{
			{Sender: "+15550000001", Text: "typing please", Timestamp: 1700000000001},
		}, nil
	}

	var typing []signal.TypingRequest
	app.signal = stubSignalClient{
		sendFn: func(ctx context.Context, req signal.SendRequest) (signal.SendResult, error) {
			return signal.SendResult{OK: true, Recipient: req.Recipient}, nil
		},
		sendTypingFn: func(ctx context.Context, req signal.TypingRequest) error {
			typing = append(typing, req)
			return nil
		},
	}

	if err := app.RunSignalInbound(context.Background(), SignalInboundRunOptions{Once: true}); err != nil {
		t.Fatalf("run signal inbound: %v", err)
	}
	if len(typing) < 2 {
		t.Fatalf("expected at least typing start+stop calls, got %d", len(typing))
	}
	if typing[0].Stop {
		t.Fatalf("expected first typing call to be start, got stop")
	}
	if typing[len(typing)-1].Stop == false {
		t.Fatalf("expected final typing call to be stop")
	}
}

func newSignalInboundTestApp(t *testing.T) *App {
	t.Helper()

	cfg := config.Default()
	cfg.App.Root = t.TempDir()
	cfg.Agents.Defaults.Workspace = "."
	cfg.Agents.List = []config.AgentConfig{{ID: "agent-ops"}}
	cfg.Cron.Enabled = false
	cfg.Heartbeat.Enabled = false
	cfg.Channels.Enabled = []string{"signal"}
	cfg.Channels.Signal.Inbound.Enabled = true
	cfg.Channels.Signal.Inbound.AllowFrom = []string{"+15550000001"}
	cfg.Channels.Signal.Inbound.DefaultAgent = "default"

	app, err := New(cfg)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("run app: %v", err)
	}
	return app
}
