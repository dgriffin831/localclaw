package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dgriffin831/localclaw/internal/session"
)

const defaultMemoryFlushPrompt = "Write durable notes from recent context to memory/YYYY-MM-DD.md in the workspace memory directory. Keep it concise and actionable."

type FlushSettings struct {
	Enabled                   bool
	CompactionThresholdTokens int
	TriggerWindowTokens       int
	Prompt                    string
	Timeout                   time.Duration
}

type FlushRunRequest struct {
	AgentID           string
	SessionID         string
	SessionKey        string
	WorkspacePath     string
	WorkspaceWritable bool
	Settings          FlushSettings
}

type flushSessionStore interface {
	Update(ctx context.Context, agentID, sessionID string, updateFn func(*session.SessionEntry) error) (session.SessionEntry, error)
}

type flushPromptClient interface {
	Prompt(ctx context.Context, input string) (string, error)
}

func ShouldRunMemoryFlush(settings FlushSettings, entry session.SessionEntry, workspaceWritable bool) bool {
	cfg := normalizeFlushSettings(settings)
	if !cfg.Enabled || !workspaceWritable {
		return false
	}
	if cfg.CompactionThresholdTokens <= 0 {
		return false
	}
	start := cfg.CompactionThresholdTokens - cfg.TriggerWindowTokens
	if start < 0 {
		start = 0
	}
	if entry.TotalTokens < start {
		return false
	}
	if strings.TrimSpace(entry.MemoryFlushAt) != "" && entry.MemoryFlushCompactionCount >= entry.CompactionCount {
		return false
	}
	return true
}

func MaybeRunMemoryFlush(ctx context.Context, req FlushRunRequest, sessions flushSessionStore, llm flushPromptClient) (bool, error) {
	if sessions == nil {
		return false, fmt.Errorf("session store is required")
	}
	if llm == nil {
		return false, fmt.Errorf("prompt client is required")
	}
	settings := normalizeFlushSettings(req.Settings)

	var current session.SessionEntry
	_, err := sessions.Update(ctx, req.AgentID, req.SessionID, func(entry *session.SessionEntry) error {
		if strings.TrimSpace(req.SessionKey) != "" {
			entry.Key = req.SessionKey
		}
		current = *entry
		return nil
	})
	if err != nil {
		return false, err
	}

	if !ShouldRunMemoryFlush(settings, current, req.WorkspaceWritable) {
		return false, nil
	}

	flushCtx := ctx
	cancel := func() {}
	if settings.Timeout > 0 {
		flushCtx, cancel = context.WithTimeout(ctx, settings.Timeout)
	}
	defer cancel()

	if _, err := llm.Prompt(flushCtx, buildMemoryFlushPrompt(settings.Prompt, req.WorkspacePath)); err != nil {
		return false, err
	}

	flushedAt := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = sessions.Update(ctx, req.AgentID, req.SessionID, func(entry *session.SessionEntry) error {
		entry.MemoryFlushAt = flushedAt
		entry.MemoryFlushCompactionCount = entry.CompactionCount
		return nil
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

func IsWorkspaceWritable(workspacePath string) bool {
	root := strings.TrimSpace(workspacePath)
	if root == "" {
		return false
	}
	memoryDir := filepath.Join(root, "memory")
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		return false
	}
	file, err := os.CreateTemp(memoryDir, ".localclaw-flush-probe-*.tmp")
	if err != nil {
		return false
	}
	name := file.Name()
	_ = file.Close()
	_ = os.Remove(name)
	return true
}

func EstimateTokensFromText(text string) int {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0
	}
	estimated := len(trimmed) / 4
	if estimated < 1 {
		return 1
	}
	return estimated
}

func normalizeFlushSettings(settings FlushSettings) FlushSettings {
	if settings.TriggerWindowTokens < 0 {
		settings.TriggerWindowTokens = 0
	}
	if settings.CompactionThresholdTokens < 0 {
		settings.CompactionThresholdTokens = 0
	}
	if strings.TrimSpace(settings.Prompt) == "" {
		settings.Prompt = defaultMemoryFlushPrompt
	}
	return settings
}

func buildMemoryFlushPrompt(prompt string, workspacePath string) string {
	p := strings.TrimSpace(prompt)
	if p == "" {
		p = defaultMemoryFlushPrompt
	}
	if strings.TrimSpace(workspacePath) == "" {
		return p
	}
	return fmt.Sprintf("%s\nWorkspace: %s", p, workspacePath)
}
