package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLocalAdapterSendUsesDefaultChannelAndReturnsDeliveryMetadata(t *testing.T) {
	t.Setenv("TEST_SLACK_TOKEN", "xoxb-test-token")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat.postMessage" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer xoxb-test-token" {
			t.Fatalf("unexpected authorization header %q", got)
		}
		defer r.Body.Close()

		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload["channel"] != "C12345" {
			t.Fatalf("expected default channel C12345, got %v", payload["channel"])
		}
		if payload["text"] != "hello from test" {
			t.Fatalf("unexpected text payload %v", payload["text"])
		}
		if payload["thread_ts"] != "1700000000.000100" {
			t.Fatalf("unexpected thread_ts %v", payload["thread_ts"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"channel":"C12345","ts":"1700000000.000200","message":{"thread_ts":"1700000000.000100"}}`))
	}))
	defer srv.Close()

	adapter := NewLocalAdapter(Settings{
		TokenEnv:       "TEST_SLACK_TOKEN",
		DefaultChannel: "C12345",
		APIBaseURL:     srv.URL,
		Timeout:        time.Second,
	})

	result, err := adapter.Send(context.Background(), SendRequest{
		Text:     "hello from test",
		ThreadID: "1700000000.000100",
	})
	if err != nil {
		t.Fatalf("send error: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected result.OK=true")
	}
	if result.Channel != "C12345" {
		t.Fatalf("expected channel C12345, got %q", result.Channel)
	}
	if result.MessageID != "1700000000.000200" {
		t.Fatalf("expected message id ts, got %q", result.MessageID)
	}
	if result.ThreadID != "1700000000.000100" {
		t.Fatalf("expected thread id from response, got %q", result.ThreadID)
	}
}

func TestLocalAdapterSendReturnsSlackAPIErrorWhenResponseNotOK(t *testing.T) {
	t.Setenv("TEST_SLACK_TOKEN", "xoxb-test-token")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":false,"error":"channel_not_found"}`))
	}))
	defer srv.Close()

	adapter := NewLocalAdapter(Settings{
		TokenEnv:       "TEST_SLACK_TOKEN",
		DefaultChannel: "C404",
		APIBaseURL:     srv.URL,
		Timeout:        time.Second,
	})

	_, err := adapter.Send(context.Background(), SendRequest{Text: "hello"})
	if err == nil {
		t.Fatalf("expected slack api error")
	}
	if !strings.Contains(err.Error(), "channel_not_found") {
		t.Fatalf("expected slack error code in error, got %v", err)
	}
}

func TestLocalAdapterSendReturnsHTTPErrorForNonSuccessStatus(t *testing.T) {
	t.Setenv("TEST_SLACK_TOKEN", "xoxb-test-token")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer srv.Close()

	adapter := NewLocalAdapter(Settings{
		TokenEnv:       "TEST_SLACK_TOKEN",
		DefaultChannel: "CERR",
		APIBaseURL:     srv.URL,
		Timeout:        time.Second,
	})

	_, err := adapter.Send(context.Background(), SendRequest{Text: "hello"})
	if err == nil {
		t.Fatalf("expected non-2xx status error")
	}
	if !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("expected HTTP status in error, got %v", err)
	}
}

func TestLocalAdapterSendHonorsTimeout(t *testing.T) {
	t.Setenv("TEST_SLACK_TOKEN", "xoxb-test-token")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(120 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"channel":"C123","ts":"1700000000.000200"}`))
	}))
	defer srv.Close()

	adapter := NewLocalAdapter(Settings{
		TokenEnv:       "TEST_SLACK_TOKEN",
		DefaultChannel: "C123",
		APIBaseURL:     srv.URL,
		Timeout:        20 * time.Millisecond,
	})

	_, err := adapter.Send(context.Background(), SendRequest{Text: "hello"})
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "deadline") {
		t.Fatalf("expected deadline error, got %v", err)
	}
}
