package hooks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/dgriffin831/localclaw/internal/session"
)

const (
	defaultRecentTurns = 12
	maxSlugLength      = 64
)

type SessionMemoryPromptClient interface {
	Prompt(ctx context.Context, input string) (string, error)
}

type SessionMemorySnapshotRequest struct {
	AgentID        string
	SessionID      string
	SessionKey     string
	Source         string
	WorkspacePath  string
	TranscriptPath string
	RecentTurns    int
	PromptClient   SessionMemoryPromptClient
	Now            func() time.Time
}

type SessionMemorySnapshotResult struct {
	Path    string
	Slug    string
	Summary string
}

func RunSessionMemorySnapshot(ctx context.Context, req SessionMemorySnapshotRequest) (SessionMemorySnapshotResult, error) {
	workspacePath := strings.TrimSpace(req.WorkspacePath)
	if workspacePath == "" {
		return SessionMemorySnapshotResult{}, fmt.Errorf("workspace path is required")
	}

	nowFn := req.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	capturedAt := nowFn().UTC()

	recentLines := loadRecentTranscriptLines(req.TranscriptPath, req.RecentTurns)
	fallbackSummary := buildFallbackSummary(recentLines)
	fallbackSlug := deterministicSlug(recentLines, capturedAt)

	summary := fallbackSummary
	slug := fallbackSlug
	if req.PromptClient != nil && len(recentLines) > 0 {
		if llmSlug, llmSummary, ok := generateLLMSnapshot(ctx, req.PromptClient, recentLines); ok {
			slug = llmSlug
			summary = llmSummary
		}
	}

	memoryDir := filepath.Join(workspacePath, "memory")
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		return SessionMemorySnapshotResult{}, fmt.Errorf("mkdir memory dir: %w", err)
	}

	filename := fmt.Sprintf("%s-%s.md", capturedAt.Format("2006-01-02"), slug)
	snapshotPath := filepath.Join(memoryDir, filename)
	snapshotPath = nextAvailableSnapshotPath(snapshotPath)
	content := renderSnapshotMarkdown(req, capturedAt, summary, recentLines)
	if err := os.WriteFile(snapshotPath, []byte(content), 0o644); err != nil {
		return SessionMemorySnapshotResult{}, fmt.Errorf("write session memory snapshot: %w", err)
	}

	return SessionMemorySnapshotResult{
		Path:    snapshotPath,
		Slug:    slug,
		Summary: summary,
	}, nil
}

func nextAvailableSnapshotPath(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	base := strings.TrimSuffix(path, filepath.Ext(path))
	ext := filepath.Ext(path)
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d%s", base, i, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func loadRecentTranscriptLines(transcriptPath string, recentTurns int) []string {
	trimmedPath := strings.TrimSpace(transcriptPath)
	if trimmedPath == "" {
		return nil
	}
	normalized, err := session.ReadNormalizedTranscript(trimmedPath)
	if err != nil {
		return nil
	}
	lines := splitNonEmptyLines(normalized)
	if len(lines) == 0 {
		return nil
	}
	limit := recentTurns
	if limit <= 0 {
		limit = defaultRecentTurns
	}
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines
}

func splitNonEmptyLines(input string) []string {
	rawLines := strings.Split(input, "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lines = append(lines, trimmed)
	}
	return lines
}

func buildFallbackSummary(recentLines []string) string {
	if len(recentLines) == 0 {
		return "Session reset snapshot created without transcript messages."
	}
	maxLines := len(recentLines)
	if maxLines > 6 {
		maxLines = 6
	}
	selected := recentLines[len(recentLines)-maxLines:]
	parts := make([]string, 0, len(selected))
	for _, line := range selected {
		parts = append(parts, truncateForSummary(line, 180))
	}
	return strings.Join(parts, "\n")
}

func truncateForSummary(text string, max int) string {
	if max <= 0 {
		max = 180
	}
	trimmed := strings.TrimSpace(text)
	if len(trimmed) <= max {
		return trimmed
	}
	if max <= 3 {
		return trimmed[:max]
	}
	return trimmed[:max-3] + "..."
}

func deterministicSlug(recentLines []string, capturedAt time.Time) string {
	seed := strings.Join(recentLines, " ")
	seed = strings.ToLower(seed)
	if idx := strings.Index(seed, ":"); idx > -1 {
		seed = seed[idx+1:]
	}
	re := regexp.MustCompile(`[^a-z0-9]+`)
	cleaned := re.ReplaceAllString(seed, " ")
	tokens := strings.Fields(cleaned)
	if len(tokens) == 0 {
		return "session-" + capturedAt.Format("150405")
	}
	if len(tokens) > 8 {
		tokens = tokens[:8]
	}
	slug := strings.Join(tokens, "-")
	if len(slug) > maxSlugLength {
		slug = slug[:maxSlugLength]
		slug = strings.Trim(slug, "-")
	}
	if slug == "" {
		return "session-" + capturedAt.Format("150405")
	}
	return slug
}

func generateLLMSnapshot(ctx context.Context, client SessionMemoryPromptClient, recentLines []string) (string, string, bool) {
	prompt := buildLLMSnapshotPrompt(recentLines)
	response, err := client.Prompt(ctx, prompt)
	if err != nil {
		return "", "", false
	}
	llmSlug, llmSummary := parseLLMSnapshotResponse(response)
	if llmSlug == "" || llmSummary == "" {
		return "", "", false
	}
	return llmSlug, llmSummary, true
}

func buildLLMSnapshotPrompt(recentLines []string) string {
	var b strings.Builder
	b.WriteString("Generate a short memory snapshot title slug and summary from this conversation excerpt.\n")
	b.WriteString("Respond with exactly two lines:\n")
	b.WriteString("slug: <kebab-case slug>\n")
	b.WriteString("summary: <concise summary>\n\n")
	b.WriteString("Conversation excerpt:\n")
	for _, line := range recentLines {
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func parseLLMSnapshotResponse(response string) (string, string) {
	lines := splitNonEmptyLines(response)
	if len(lines) == 0 {
		return "", ""
	}
	var slug, summary string
	for _, line := range lines {
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "slug:"):
			slug = sanitizeSlug(strings.TrimSpace(line[len("slug:"):]))
		case strings.HasPrefix(lower, "summary:"):
			summary = strings.TrimSpace(line[len("summary:"):])
		}
	}
	return slug, summary
}

func sanitizeSlug(value string) string {
	re := regexp.MustCompile(`[^a-z0-9]+`)
	slug := strings.ToLower(strings.TrimSpace(value))
	slug = re.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > maxSlugLength {
		slug = strings.Trim(slug[:maxSlugLength], "-")
	}
	return slug
}

func renderSnapshotMarkdown(req SessionMemorySnapshotRequest, capturedAt time.Time, summary string, recentLines []string) string {
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "session-reset"
	}
	transcriptPath := strings.TrimSpace(req.TranscriptPath)
	if transcriptPath == "" {
		transcriptPath = "(none)"
	}
	agentID := strings.TrimSpace(req.AgentID)
	sessionID := strings.TrimSpace(req.SessionID)
	sessionKey := strings.TrimSpace(req.SessionKey)

	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("sessionKey: %s\n", sessionKey))
	b.WriteString(fmt.Sprintf("sessionID: %s\n", sessionID))
	b.WriteString(fmt.Sprintf("agentID: %s\n", agentID))
	b.WriteString(fmt.Sprintf("source: %s\n", source))
	b.WriteString(fmt.Sprintf("timestamp: %s\n", capturedAt.Format(time.RFC3339Nano)))
	b.WriteString(fmt.Sprintf("transcript: %s\n", transcriptPath))
	b.WriteString("---\n\n")
	b.WriteString("# Session Memory Snapshot\n\n")
	b.WriteString("## Summary\n\n")
	b.WriteString(summary)
	b.WriteString("\n\n")
	if len(recentLines) > 0 {
		b.WriteString("## Recent Transcript\n\n")
		for _, line := range recentLines {
			b.WriteString("- ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	return b.String()
}
