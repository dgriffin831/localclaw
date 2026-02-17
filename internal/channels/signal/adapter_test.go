package signal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalAdapterSendBuildsGroupCommandUsingDefaultRecipient(t *testing.T) {
	tempDir := t.TempDir()
	argsPath := filepath.Join(tempDir, "args.txt")
	scriptPath := filepath.Join(tempDir, "signal-cli")
	script := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n' \"$@\" > \"" + argsPath + "\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake signal-cli: %v", err)
	}

	adapter := NewLocalAdapter(Settings{
		CLIPath:          scriptPath,
		Account:          "+15551234567",
		DefaultRecipient: "group:engineering-room",
		Timeout:          time.Second,
		Now:              func() time.Time { return time.Date(2026, 2, 17, 12, 0, 0, 0, time.UTC) },
	})

	result, err := adapter.Send(context.Background(), SendRequest{Text: "hello team"})
	if err != nil {
		t.Fatalf("send error: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected result.OK=true")
	}
	if result.Recipient != "group:engineering-room" {
		t.Fatalf("unexpected recipient %q", result.Recipient)
	}
	if result.SentAt != "2026-02-17T12:00:00Z" {
		t.Fatalf("unexpected sent_at %q", result.SentAt)
	}

	data, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	args := splitArgsLines(data)
	want := []string{"-a", "+15551234567", "send", "-m", "hello team", "-g", "engineering-room"}
	if len(args) != len(want) {
		t.Fatalf("unexpected arg count\nwant=%v\ngot=%v", want, args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("unexpected args\nwant=%v\ngot=%v", want, args)
		}
	}
}

func TestLocalAdapterSendUsesExplicitRecipientOverride(t *testing.T) {
	tempDir := t.TempDir()
	argsPath := filepath.Join(tempDir, "args.txt")
	scriptPath := filepath.Join(tempDir, "signal-cli")
	script := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n' \"$@\" > \"" + argsPath + "\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake signal-cli: %v", err)
	}

	adapter := NewLocalAdapter(Settings{
		CLIPath:          scriptPath,
		Account:          "+15551234567",
		DefaultRecipient: "+15550000000",
		Timeout:          time.Second,
	})

	result, err := adapter.Send(context.Background(), SendRequest{
		Text:      "direct ping",
		Recipient: "+15557654321",
	})
	if err != nil {
		t.Fatalf("send error: %v", err)
	}
	if result.Recipient != "+15557654321" {
		t.Fatalf("expected explicit recipient, got %q", result.Recipient)
	}

	data, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	args := splitArgsLines(data)
	if args[len(args)-1] != "+15557654321" {
		t.Fatalf("expected final arg to be explicit recipient, got %v", args)
	}
}

func TestLocalAdapterSendReturnsStderrOnSubprocessFailure(t *testing.T) {
	tempDir := t.TempDir()
	scriptPath := filepath.Join(tempDir, "signal-cli")
	script := "#!/usr/bin/env bash\nset -euo pipefail\necho 'untrusted identity key' >&2\nexit 3\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake signal-cli: %v", err)
	}

	adapter := NewLocalAdapter(Settings{
		CLIPath:          scriptPath,
		Account:          "+15551234567",
		DefaultRecipient: "+15557654321",
		Timeout:          time.Second,
	})

	_, err := adapter.Send(context.Background(), SendRequest{Text: "hello"})
	if err == nil {
		t.Fatalf("expected subprocess failure")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "untrusted identity key") {
		t.Fatalf("expected stderr in error, got %v", err)
	}
}

func TestLocalAdapterSendHonorsTimeout(t *testing.T) {
	tempDir := t.TempDir()
	scriptPath := filepath.Join(tempDir, "signal-cli")
	script := "#!/usr/bin/env bash\nset -euo pipefail\nsleep 2\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake signal-cli: %v", err)
	}

	adapter := NewLocalAdapter(Settings{
		CLIPath:          scriptPath,
		Account:          "+15551234567",
		DefaultRecipient: "+15557654321",
		Timeout:          20 * time.Millisecond,
	})

	_, err := adapter.Send(context.Background(), SendRequest{Text: "hello"})
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(strings.ToLower(err.Error()), "deadline") {
		t.Fatalf("expected deadline exceeded error, got %v", err)
	}
}

func splitArgsLines(data []byte) []string {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}
