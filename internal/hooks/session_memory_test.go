package hooks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakePromptClient struct {
	response string
	err      error
}

func (f fakePromptClient) Prompt(ctx context.Context, input string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.response, nil
}

func writeTranscriptFile(t *testing.T, path string, rows []string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir transcript dir: %v", err)
	}
	payload := strings.Join(rows, "\n") + "\n"
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
}

func TestRunSessionMemorySnapshotCreatesDatedFileFromTranscript(t *testing.T) {
	workspacePath := t.TempDir()
	transcriptPath := filepath.Join(t.TempDir(), "main.jsonl")
	writeTranscriptFile(t, transcriptPath, []string{
		`{"type":"message","role":"user","content":"I fixed the parser bug"}`,
		`{"type":"message","role":"assistant","content":"Great, let's add tests next"}`,
		`{"type":"message","role":"user","content":"Please also improve reset behavior"}`,
	})

	now := time.Date(2026, 2, 15, 18, 22, 30, 0, time.UTC)
	result, err := RunSessionMemorySnapshot(context.Background(), SessionMemorySnapshotRequest{
		AgentID:        "default",
		SessionID:      "main",
		SessionKey:     "default/main",
		Source:         "/reset",
		WorkspacePath:  workspacePath,
		TranscriptPath: transcriptPath,
		Now: func() time.Time {
			return now
		},
	})
	if err != nil {
		t.Fatalf("run session memory snapshot: %v", err)
	}

	expectedPrefix := filepath.Join(workspacePath, "memory", "2026-02-15-")
	if !strings.HasPrefix(result.Path, expectedPrefix) {
		t.Fatalf("expected snapshot path to start with %q, got %q", expectedPrefix, result.Path)
	}
	if result.Slug == "" {
		t.Fatalf("expected snapshot slug to be set")
	}

	content, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	body := string(content)
	for _, want := range []string{
		"sessionKey: default/main",
		"sessionID: main",
		"source: /reset",
		"I fixed the parser bug",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected snapshot to contain %q", want)
		}
	}
}

func TestRunSessionMemorySnapshotUsesLLMSlugAndSummaryWhenAvailable(t *testing.T) {
	workspacePath := t.TempDir()
	transcriptPath := filepath.Join(t.TempDir(), "main.jsonl")
	writeTranscriptFile(t, transcriptPath, []string{
		`{"type":"message","role":"user","content":"Track parser regressions"}`,
	})

	now := time.Date(2026, 2, 15, 20, 1, 0, 0, time.UTC)
	result, err := RunSessionMemorySnapshot(context.Background(), SessionMemorySnapshotRequest{
		AgentID:        "default",
		SessionID:      "main",
		SessionKey:     "default/main",
		Source:         "/new",
		WorkspacePath:  workspacePath,
		TranscriptPath: transcriptPath,
		PromptClient: fakePromptClient{
			response: "slug: parser-regression-fix\nsummary: Captured parser regression notes and the follow-up plan.",
		},
		Now: func() time.Time {
			return now
		},
	})
	if err != nil {
		t.Fatalf("run session memory snapshot: %v", err)
	}

	if result.Slug != "parser-regression-fix" {
		t.Fatalf("expected llm slug to be used, got %q", result.Slug)
	}
	if !strings.HasSuffix(result.Path, "2026-02-15-parser-regression-fix.md") {
		t.Fatalf("unexpected snapshot path %q", result.Path)
	}

	content, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if !strings.Contains(string(content), "Captured parser regression notes and the follow-up plan.") {
		t.Fatalf("expected llm summary in snapshot")
	}
}

func TestRunSessionMemorySnapshotFallsBackWhenLLMUnavailable(t *testing.T) {
	workspacePath := t.TempDir()
	transcriptPath := filepath.Join(t.TempDir(), "main.jsonl")
	writeTranscriptFile(t, transcriptPath, []string{
		`{"type":"message","role":"user","content":"Need a deterministic fallback slug"}`,
	})

	now := time.Date(2026, 2, 15, 20, 2, 0, 0, time.UTC)
	result, err := RunSessionMemorySnapshot(context.Background(), SessionMemorySnapshotRequest{
		AgentID:        "default",
		SessionID:      "main",
		SessionKey:     "default/main",
		Source:         "/reset",
		WorkspacePath:  workspacePath,
		TranscriptPath: transcriptPath,
		PromptClient: fakePromptClient{
			err: fmt.Errorf("llm unavailable"),
		},
		Now: func() time.Time {
			return now
		},
	})
	if err != nil {
		t.Fatalf("run session memory snapshot: %v", err)
	}
	if result.Slug == "" {
		t.Fatalf("expected fallback slug to be populated")
	}
	if _, err := os.Stat(result.Path); err != nil {
		t.Fatalf("expected snapshot file to exist: %v", err)
	}
}
