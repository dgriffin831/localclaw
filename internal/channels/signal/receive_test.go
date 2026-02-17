package signal

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReceiveBatchParsesDirectAndGroupMessages(t *testing.T) {
	tmp := t.TempDir()
	script := filepath.Join(tmp, "signal-cli")
	body := `#!/usr/bin/env bash
set -euo pipefail
cat <<'JSON'
{"envelope":{"sourceNumber":"+15550000001","sourceName":"Alice","timestamp":1771300140306,"dataMessage":{"message":"hello"}},"account":"+15559990000"}
{"envelope":{"sourceNumber":"+15550000002","sourceName":"Bob","timestamp":1771300140400,"dataMessage":{"message":"group msg","groupInfo":{"groupId":"group-123","groupName":"Team"}}},"account":"+15559990000"}
JSON
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake signal-cli: %v", err)
	}

	messages, err := ReceiveBatch(context.Background(), ReceiveSettings{
		CLIPath:            script,
		Account:            "+15559990000",
		Timeout:            2 * time.Second,
		MaxMessagesPerPoll: 5,
		IgnoreAttachments:  true,
		IgnoreStories:      true,
	})
	if err != nil {
		t.Fatalf("receive batch: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if messages[0].Sender != "+15550000001" || messages[0].Text != "hello" {
		t.Fatalf("unexpected first message %+v", messages[0])
	}
	if !messages[1].IsGroup || messages[1].GroupID != "group-123" {
		t.Fatalf("expected second message to be group message, got %+v", messages[1])
	}
}

func TestReceiveBatchBuildsExpectedSignalCLICommand(t *testing.T) {
	tmp := t.TempDir()
	script := filepath.Join(tmp, "signal-cli")
	argsPath := filepath.Join(tmp, "args.txt")
	body := `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$@" > "` + argsPath + `"
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake signal-cli: %v", err)
	}

	_, err := ReceiveBatch(context.Background(), ReceiveSettings{
		CLIPath:            script,
		Account:            "+15559990000",
		Timeout:            3 * time.Second,
		MaxMessagesPerPoll: 7,
		IgnoreAttachments:  true,
		IgnoreStories:      true,
	})
	if err != nil {
		t.Fatalf("receive batch: %v", err)
	}

	argsRaw, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	args := string(argsRaw)
	for _, token := range []string{
		"-o\njson\n",
		"-a\n+15559990000\n",
		"receive\n",
		"--timeout\n3\n",
		"--max-messages\n7\n",
		"--ignore-attachments\n",
		"--ignore-stories\n",
	} {
		if !containsLiteral(args, token) {
			t.Fatalf("expected token %q in args output, got:\n%s", token, args)
		}
	}
}

func containsLiteral(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
