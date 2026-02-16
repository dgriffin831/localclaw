package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dgriffin831/localclaw/internal/cron"
	"github.com/dgriffin831/localclaw/internal/memory"
	"github.com/dgriffin831/localclaw/internal/session"
	"github.com/dgriffin831/localclaw/internal/skills"
	"github.com/dgriffin831/localclaw/internal/workspace"
)

var ErrMCPNotFound = errors.New("not found")

const (
	transcriptScanBufferBytes = 64 * 1024
	transcriptScanMaxBytes    = 1024 * 1024
	transcriptItemMaxChars    = 16 * 1024
)

type MCPWorkspaceStatus struct {
	AgentID       string `json:"agentId"`
	WorkspacePath string `json:"workspacePath"`
	Exists        bool   `json:"exists"`
}

type MCPSessionsList struct {
	Sessions []session.SessionEntry `json:"sessions"`
	Total    int                    `json:"total"`
}

type MCPHistoryItem struct {
	Role      string `json:"role,omitempty"`
	Content   string `json:"content"`
	CreatedAt string `json:"createdAt,omitempty"`
}

type MCPSessionsHistory struct {
	Items []MCPHistoryItem `json:"items"`
	Total int              `json:"total"`
}

func (a *App) MCPMemorySearch(ctx context.Context, agentID, sessionID, query string, opts memory.SearchOptions) ([]memory.SearchResult, error) {
	resolvedAgentID := ResolveAgentID(agentID)
	resolvedSession := ResolveSession(resolvedAgentID, sessionID)
	if enabled, reason := a.memoryToolEnabled(resolvedAgentID, skills.ToolMemorySearch); !enabled {
		return nil, errors.New(reason)
	}
	memoryCfg := a.resolveMemoryConfig(resolvedAgentID)
	if opts.SessionKey == "" {
		opts.SessionKey = resolvedSession.SessionKey
	}
	if opts.MaxResults <= 0 {
		opts.MaxResults = memoryCfg.Query.MaxResults
	}

	manager, cleanup, err := a.newMemoryToolManager(ctx, resolvedAgentID, memoryCfg)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	if memoryCfg.Sync.OnSearch {
		if _, err := manager.Sync(ctx, false); err != nil {
			return nil, fmt.Errorf("memory_search sync failed: %w", err)
		}
	}
	return manager.Search(ctx, query, opts)
}

func (a *App) MCPMemoryGet(ctx context.Context, agentID, _ string, path string, opts memory.GetOptions) (memory.GetResult, error) {
	resolvedAgentID := ResolveAgentID(agentID)
	if enabled, reason := a.memoryToolEnabled(resolvedAgentID, skills.ToolMemoryGet); !enabled {
		return memory.GetResult{}, errors.New(reason)
	}
	memoryCfg := a.resolveMemoryConfig(resolvedAgentID)

	manager, cleanup, err := a.newMemoryToolManager(ctx, resolvedAgentID, memoryCfg)
	if err != nil {
		return memory.GetResult{}, err
	}
	defer cleanup()

	return manager.Get(ctx, path, opts)
}

func (a *App) MCPMemoryGrep(ctx context.Context, agentID, sessionID, query string, opts memory.GrepOptions) (memory.GrepResult, error) {
	_ = sessionID
	resolvedAgentID := ResolveAgentID(agentID)
	if enabled, reason := a.memoryToolEnabled(resolvedAgentID, skills.ToolMemoryGrep); !enabled {
		return memory.GrepResult{}, errors.New(reason)
	}
	memoryCfg := a.resolveMemoryConfig(resolvedAgentID)

	manager, cleanup, err := a.newMemoryToolManager(ctx, resolvedAgentID, memoryCfg)
	if err != nil {
		return memory.GrepResult{}, err
	}
	defer cleanup()

	return manager.Grep(ctx, query, opts)
}

func (a *App) MCPWorkspaceStatus(ctx context.Context, agentID string) (MCPWorkspaceStatus, error) {
	resolvedAgentID := ResolveAgentID(agentID)
	workspacePath, err := a.ResolveWorkspacePath(resolvedAgentID)
	if err != nil {
		return MCPWorkspaceStatus{}, err
	}
	_, statErr := os.Stat(workspacePath)
	exists := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return MCPWorkspaceStatus{}, fmt.Errorf("stat workspace: %w", statErr)
	}
	return MCPWorkspaceStatus{AgentID: resolvedAgentID, WorkspacePath: workspacePath, Exists: exists}, nil
}

func (a *App) MCPWorkspaceBootstrapContext(ctx context.Context, agentID, sessionID string) (string, []workspace.BootstrapFile, error) {
	resolution := ResolveSession(agentID, sessionID)
	workspacePath, err := a.ResolveWorkspacePath(resolution.AgentID)
	if err != nil {
		return "", nil, err
	}
	files, err := a.workspace.LoadBootstrapFiles(ctx, resolution.AgentID, resolution.SessionKey)
	if err != nil {
		return "", nil, err
	}
	return workspacePath, files, nil
}

func (a *App) MCPCronList(ctx context.Context) ([]cron.Entry, error) {
	return a.cron.List(ctx)
}

func (a *App) MCPCronAdd(ctx context.Context, id, schedule, command string) (cron.Entry, error) {
	return a.cron.Add(ctx, cron.AddRequest{ID: id, Schedule: schedule, Command: command})
}

func (a *App) MCPCronRemove(ctx context.Context, id string) (bool, error) {
	return a.cron.Remove(ctx, id)
}

func (a *App) MCPCronRun(ctx context.Context, id string) (cron.RunResult, error) {
	return a.cron.Run(ctx, id)
}

func (a *App) MCPSessionsList(ctx context.Context, agentID string, limit, offset int) (MCPSessionsList, error) {
	resolvedAgentID := ResolveAgentID(agentID)
	sessionsMap, err := a.sessions.Load(ctx, resolvedAgentID)
	if err != nil {
		return MCPSessionsList{}, err
	}
	ordered := make([]session.SessionEntry, 0, len(sessionsMap))
	for _, entry := range sessionsMap {
		ordered = append(ordered, entry)
	}
	sort.Slice(ordered, func(i, j int) bool {
		left := ordered[i].UpdatedAt
		right := ordered[j].UpdatedAt
		if left == right {
			return ordered[i].ID < ordered[j].ID
		}
		return left > right
	})
	total := len(ordered)
	start := clampRangeStart(offset, total)
	end := clampRangeEnd(start, limit, total)
	return MCPSessionsList{Sessions: ordered[start:end], Total: total}, nil
}

func (a *App) MCPSessionStatus(ctx context.Context, agentID, sessionID string) (session.SessionEntry, error) {
	resolution := ResolveSession(agentID, sessionID)
	entry, exists, err := a.sessions.Get(ctx, resolution.AgentID, resolution.SessionID)
	if err != nil {
		return session.SessionEntry{}, err
	}
	if !exists {
		return session.SessionEntry{}, ErrMCPNotFound
	}
	if entry.AgentID != "" && entry.AgentID != resolution.AgentID {
		return session.SessionEntry{}, fmt.Errorf("session %q does not belong to agent %q", resolution.SessionID, resolution.AgentID)
	}
	return entry, nil
}

func (a *App) MCPSessionsHistory(ctx context.Context, agentID, sessionID string, limit, offset int) (MCPSessionsHistory, error) {
	resolution := ResolveSession(agentID, sessionID)
	if _, err := a.MCPSessionStatus(ctx, resolution.AgentID, resolution.SessionID); err != nil {
		return MCPSessionsHistory{}, err
	}
	transcriptPath, err := a.ResolveTranscriptPath(resolution.AgentID, resolution.SessionID)
	if err != nil {
		return MCPSessionsHistory{}, err
	}
	items, err := readTranscriptHistory(transcriptPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return MCPSessionsHistory{Items: []MCPHistoryItem{}, Total: 0}, nil
		}
		return MCPSessionsHistory{}, err
	}
	total := len(items)
	start := clampRangeStart(offset, total)
	end := clampRangeEnd(start, limit, total)
	return MCPSessionsHistory{Items: items[start:end], Total: total}, nil
}

func (a *App) MCPSessionsSend(ctx context.Context, agentID, sessionID, input string) (string, error) {
	resolution := ResolveSession(agentID, sessionID)
	if _, err := a.MCPSessionStatus(ctx, resolution.AgentID, resolution.SessionID); err != nil {
		return "", err
	}
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", errors.New("input is required")
	}
	if err := a.AppendSessionTranscriptMessage(ctx, resolution.AgentID, resolution.SessionID, "user", trimmed); err != nil {
		return "", err
	}
	response, err := a.PromptForSession(ctx, resolution.AgentID, resolution.SessionID, trimmed)
	if err != nil {
		return "", err
	}
	if err := a.AppendSessionTranscriptMessage(ctx, resolution.AgentID, resolution.SessionID, "assistant", response); err != nil {
		return "", err
	}
	_, err = a.sessions.Update(ctx, resolution.AgentID, resolution.SessionID, func(entry *session.SessionEntry) error {
		entry.Key = resolution.SessionKey
		return nil
	})
	if err != nil {
		return "", err
	}
	return response, nil
}

func readTranscriptHistory(path string) ([]MCPHistoryItem, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	items := make([]MCPHistoryItem, 0, 32)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, transcriptScanBufferBytes), transcriptScanMaxBytes)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var row map[string]interface{}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		content := strings.TrimSpace(extractTranscriptText(row["content"]))
		if content == "" {
			content = strings.TrimSpace(extractTranscriptText(row["text"]))
		}
		if content == "" {
			continue
		}
		item := MCPHistoryItem{Content: truncateString(content, transcriptItemMaxChars)}
		if role, ok := row["role"].(string); ok {
			item.Role = strings.TrimSpace(role)
		}
		if createdAt, ok := row["createdAt"].(string); ok {
			item.CreatedAt = strings.TrimSpace(createdAt)
		}
		items = append(items, item)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func truncateString(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	return value[:max]
}

func clampRangeStart(offset, total int) int {
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		return total
	}
	return offset
}

func clampRangeEnd(start, limit, total int) int {
	if limit <= 0 {
		limit = total
	}
	end := start + limit
	if end > total {
		return total
	}
	return end
}

func extractTranscriptText(v interface{}) string {
	switch typed := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case map[string]interface{}:
		text := extractTranscriptText(typed["text"])
		if text != "" {
			return text
		}
		return extractTranscriptText(typed["content"])
	case []interface{}:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			text := extractTranscriptText(item)
			if text != "" {
				parts = append(parts, text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, " "))
	default:
		return ""
	}
}
