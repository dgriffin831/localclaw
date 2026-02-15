package session

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TranscriptMessage is one normalized transcript append input.
type TranscriptMessage struct {
	Role    string
	Content string
}

type TranscriptWriterSettings struct {
	Events *TranscriptEventBus
	Now    func() time.Time
}

type TranscriptWriter struct {
	events *TranscriptEventBus
	now    func() time.Time
}

func NewTranscriptWriter(settings TranscriptWriterSettings) *TranscriptWriter {
	now := settings.Now
	if now == nil {
		now = time.Now
	}
	return &TranscriptWriter{events: settings.Events, now: now}
}

func (w *TranscriptWriter) AppendMessage(ctx context.Context, transcriptPath string, msg TranscriptMessage) error {
	if strings.TrimSpace(transcriptPath) == "" {
		return errors.New("transcript path is required")
	}
	if strings.TrimSpace(msg.Content) == "" {
		return errors.New("transcript content is required")
	}
	if err := os.MkdirAll(filepath.Dir(transcriptPath), 0o700); err != nil {
		return err
	}

	record := map[string]interface{}{
		"type":      "message",
		"role":      strings.TrimSpace(msg.Role),
		"content":   msg.Content,
		"createdAt": w.now().UTC().Format(time.RFC3339Nano),
	}
	if strings.TrimSpace(msg.Role) == "" {
		delete(record, "role")
	}

	payload, err := json.Marshal(record)
	if err != nil {
		return err
	}
	line := append(payload, '\n')

	f, err := os.OpenFile(transcriptPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	written := 0
	written, err = f.Write(line)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}

	if w.events != nil {
		_ = w.events.Publish(ctx, TranscriptUpdate{
			TranscriptPath: transcriptPath,
			DeltaBytes:     written,
			DeltaMessages:  1,
			OccurredAt:     w.now().UTC(),
		})
	}
	return nil
}

// NormalizeJSONLTranscript parses JSONL transcript rows into normalized searchable text.
func NormalizeJSONLTranscript(data []byte) string {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	lines := make([]string, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		normalized := normalizeTranscriptLine(line)
		if normalized == "" {
			continue
		}
		lines = append(lines, normalized)
	}
	return strings.Join(lines, "\n")
}

func normalizeTranscriptLine(line string) string {
	var row interface{}
	if err := json.Unmarshal([]byte(line), &row); err != nil {
		return ""
	}
	obj, ok := row.(map[string]interface{})
	if !ok {
		return ""
	}
	role := firstNonEmptyString(
		stringField(obj, "role"),
		nestedStringField(obj, "message", "role"),
	)
	text := firstNonEmptyString(
		extractTextField(obj["content"]),
		extractTextField(obj["text"]),
		extractTextField(obj["message"]),
		nestedStringField(obj, "message", "content"),
		nestedStringField(obj, "delta", "text"),
	)
	if text == "" {
		return ""
	}
	if role != "" {
		return role + ": " + text
	}
	return text
}

func ReadNormalizedTranscript(path string) (string, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return NormalizeJSONLTranscript(payload), nil
}

func extractTextField(v interface{}) string {
	switch typed := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case map[string]interface{}:
		return firstNonEmptyString(
			stringField(typed, "text"),
			stringField(typed, "content"),
			extractTextField(typed["content"]),
		)
	case []interface{}:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			text := extractTextField(item)
			if text != "" {
				parts = append(parts, text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, " "))
	default:
		return ""
	}
}

func stringField(obj map[string]interface{}, key string) string {
	if obj == nil {
		return ""
	}
	raw, ok := obj[key]
	if !ok {
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func nestedStringField(obj map[string]interface{}, outer string, inner string) string {
	raw, ok := obj[outer]
	if !ok {
		return ""
	}
	nested, ok := raw.(map[string]interface{})
	if !ok {
		return ""
	}
	return stringField(nested, inner)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
