package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

// Client is the Slack channel boundary.
type Client interface {
	Send(ctx context.Context, req SendRequest) (SendResult, error)
}

type SendRequest struct {
	Text     string
	Channel  string
	ThreadID string
}

type SendResult struct {
	OK        bool   `json:"ok"`
	Channel   string `json:"channel"`
	MessageID string `json:"message_id"`
	ThreadID  string `json:"thread_id,omitempty"`
}

type Settings struct {
	TokenEnv       string
	DefaultChannel string
	APIBaseURL     string
	Timeout        time.Duration
	HTTPClient     *http.Client
	LookupEnv      func(string) string
}

type LocalAdapter struct {
	tokenEnv       string
	defaultChannel string
	apiBaseURL     string
	timeout        time.Duration
	httpClient     *http.Client
	lookupEnv      func(string) string
}

type postMessageRequest struct {
	Channel  string `json:"channel"`
	Text     string `json:"text"`
	ThreadTS string `json:"thread_ts,omitempty"`
}

type postMessageResponse struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error"`
	Channel string `json:"channel"`
	TS      string `json:"ts"`
	Message struct {
		ThreadTS string `json:"thread_ts"`
	} `json:"message"`
}

func NewLocalAdapter(settings Settings) *LocalAdapter {
	tokenEnv := strings.TrimSpace(settings.TokenEnv)
	if tokenEnv == "" {
		tokenEnv = "SLACK_BOT_TOKEN"
	}
	baseURL := strings.TrimSpace(settings.APIBaseURL)
	if baseURL == "" {
		baseURL = "https://slack.com/api"
	}
	timeout := settings.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	httpClient := settings.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	lookupEnv := settings.LookupEnv
	if lookupEnv == nil {
		lookupEnv = os.Getenv
	}

	return &LocalAdapter{
		tokenEnv:       tokenEnv,
		defaultChannel: strings.TrimSpace(settings.DefaultChannel),
		apiBaseURL:     strings.TrimRight(baseURL, "/"),
		timeout:        timeout,
		httpClient:     httpClient,
		lookupEnv:      lookupEnv,
	}
}

func (a *LocalAdapter) Send(ctx context.Context, req SendRequest) (SendResult, error) {
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return SendResult{}, errors.New("text is required")
	}

	channel := strings.TrimSpace(req.Channel)
	if channel == "" {
		channel = a.defaultChannel
	}
	if channel == "" {
		return SendResult{}, errors.New("channel is required")
	}

	token := strings.TrimSpace(a.lookupEnv(a.tokenEnv))
	if token == "" {
		return SendResult{}, fmt.Errorf("slack token env %q is empty", a.tokenEnv)
	}

	payload := postMessageRequest{
		Channel:  channel,
		Text:     text,
		ThreadTS: strings.TrimSpace(req.ThreadID),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return SendResult{}, fmt.Errorf("encode slack request: %w", err)
	}

	runCtx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	endpoint := a.apiEndpoint("chat.postMessage")
	httpReq, err := http.NewRequestWithContext(runCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return SendResult{}, fmt.Errorf("build slack request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		if runCtx.Err() != nil {
			return SendResult{}, fmt.Errorf("slack request failed: %w", runCtx.Err())
		}
		return SendResult{}, fmt.Errorf("slack request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return SendResult{}, fmt.Errorf("read slack response: %w", err)
	}
	sanitizedBody := redactSecret(strings.TrimSpace(string(respBody)), token)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if sanitizedBody == "" {
			sanitizedBody = http.StatusText(resp.StatusCode)
		}
		return SendResult{}, fmt.Errorf("slack api HTTP %d: %s", resp.StatusCode, sanitizedBody)
	}

	var decoded postMessageResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return SendResult{}, fmt.Errorf("decode slack response: %w", err)
	}
	if !decoded.OK {
		code := strings.TrimSpace(decoded.Error)
		if code == "" {
			code = "unknown_error"
		}
		return SendResult{}, fmt.Errorf("slack api error: %s", code)
	}

	messageID := strings.TrimSpace(decoded.TS)
	if messageID == "" {
		return SendResult{}, errors.New("slack api response missing message timestamp")
	}

	threadID := strings.TrimSpace(decoded.Message.ThreadTS)
	if threadID == "" {
		threadID = strings.TrimSpace(req.ThreadID)
	}
	outChannel := strings.TrimSpace(decoded.Channel)
	if outChannel == "" {
		outChannel = channel
	}

	return SendResult{
		OK:        true,
		Channel:   outChannel,
		MessageID: messageID,
		ThreadID:  threadID,
	}, nil
}

func (a *LocalAdapter) apiEndpoint(method string) string {
	cleanMethod := strings.TrimLeft(path.Clean("/"+method), "/")
	return a.apiBaseURL + "/" + cleanMethod
}

func redactSecret(value, secret string) string {
	if strings.TrimSpace(secret) == "" {
		return value
	}
	return strings.ReplaceAll(value, secret, "[redacted]")
}
