package tools

import (
	"context"
	"errors"
	"testing"
)

type stubChannelsBackend struct {
	slackFn  func(ctx context.Context, req SlackSendRequest) (SlackSendResult, error)
	signalFn func(ctx context.Context, req SignalSendRequest) (SignalSendResult, error)
}

func (s stubChannelsBackend) SlackSend(ctx context.Context, req SlackSendRequest) (SlackSendResult, error) {
	return s.slackFn(ctx, req)
}

func (s stubChannelsBackend) SignalSend(ctx context.Context, req SignalSendRequest) (SignalSendResult, error) {
	return s.signalFn(ctx, req)
}

func TestSlackSendToolRequiresText(t *testing.T) {
	h := NewSlackSendTool(stubChannelsBackend{slackFn: func(ctx context.Context, req SlackSendRequest) (SlackSendResult, error) {
		return SlackSendResult{}, nil
	}})

	res := h.Call(context.Background(), map[string]interface{}{})
	if !res.IsError {
		t.Fatalf("expected validation error")
	}
}

func TestSlackSendToolMapsRequestAndReturnsStructuredResponse(t *testing.T) {
	h := NewSlackSendTool(stubChannelsBackend{slackFn: func(ctx context.Context, req SlackSendRequest) (SlackSendResult, error) {
		if req.Text != "hello" {
			t.Fatalf("unexpected text %q", req.Text)
		}
		if req.Channel != "C123" {
			t.Fatalf("unexpected channel %q", req.Channel)
		}
		if req.ThreadID != "1700000000.000100" {
			t.Fatalf("unexpected thread id %q", req.ThreadID)
		}
		if req.AgentID != "agent-a" || req.SessionID != "session-a" {
			t.Fatalf("unexpected session routing %+v", req)
		}
		return SlackSendResult{OK: true, Channel: "C123", MessageID: "1700000000.000200", ThreadID: "1700000000.000100"}, nil
	}})

	res := h.Call(context.Background(), map[string]interface{}{
		"text":       "hello",
		"channel":    "C123",
		"thread_id":  "1700000000.000100",
		"agent_id":   "agent-a",
		"session_id": "session-a",
	})
	if res.IsError {
		t.Fatalf("expected success, got %+v", res.StructuredContent)
	}
	if got := res.StructuredContent["message_id"]; got != "1700000000.000200" {
		t.Fatalf("unexpected message_id %v", got)
	}
}

func TestSignalSendToolMapsBackendErrors(t *testing.T) {
	h := NewSignalSendTool(stubChannelsBackend{signalFn: func(ctx context.Context, req SignalSendRequest) (SignalSendResult, error) {
		return SignalSendResult{}, errors.New("signal unavailable")
	}})

	res := h.Call(context.Background(), map[string]interface{}{"text": "hello"})
	if !res.IsError {
		t.Fatalf("expected error")
	}
	if got := res.StructuredContent["ok"]; got != false {
		t.Fatalf("expected ok=false, got %v", got)
	}
}

func TestSignalSendToolReturnsStructuredContent(t *testing.T) {
	h := NewSignalSendTool(stubChannelsBackend{signalFn: func(ctx context.Context, req SignalSendRequest) (SignalSendResult, error) {
		if req.Recipient != "+15557654321" {
			t.Fatalf("unexpected recipient %q", req.Recipient)
		}
		return SignalSendResult{OK: true, Recipient: req.Recipient, SentAt: "2026-02-17T15:04:05Z"}, nil
	}})

	res := h.Call(context.Background(), map[string]interface{}{
		"text":      "hello",
		"recipient": "+15557654321",
	})
	if res.IsError {
		t.Fatalf("expected success")
	}
	if got := res.StructuredContent["sent_at"]; got != "2026-02-17T15:04:05Z" {
		t.Fatalf("unexpected sent_at %v", got)
	}
}
