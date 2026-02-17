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

	"github.com/dgriffin831/localclaw/internal/channels/signal"
	"github.com/dgriffin831/localclaw/internal/channels/slack"
	"github.com/dgriffin831/localclaw/internal/cron"
	"github.com/dgriffin831/localclaw/internal/memory"
	"github.com/dgriffin831/localclaw/internal/session"
	"github.com/dgriffin831/localclaw/internal/skills"
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

type MCPSlackSendResult struct {
	OK        bool   `json:"ok"`
	Channel   string `json:"channel"`
	MessageID string `json:"message_id"`
	ThreadID  string `json:"thread_id,omitempty"`
}

type MCPSignalSendResult struct {
	OK        bool   `json:"ok"`
	Recipient string `json:"recipient"`
	SentAt    string `json:"sent_at"`
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

func (a *App) MCPSlackSend(ctx context.Context, text, channel, threadID, agentID, sessionID string) (MCPSlackSendResult, error) {
	if !a.channelEnabled("slack") || a.slack == nil {
		return MCPSlackSendResult{}, disabledChannelError("slack")
	}

	delivery, err := a.slack.Send(ctx, slack.SendRequest{
		Text:     text,
		Channel:  channel,
		ThreadID: threadID,
	})
	if err != nil {
		return MCPSlackSendResult{}, err
	}

	result := MCPSlackSendResult{
		OK:        delivery.OK,
		Channel:   delivery.Channel,
		MessageID: delivery.MessageID,
		ThreadID:  delivery.ThreadID,
	}

	if shouldPersistDelivery(agentID, sessionID) {
		if err := a.persistChannelDelivery(ctx, session.OriginSlack, agentID, sessionID, result.Channel, result.ThreadID, result.MessageID); err != nil {
			return result, fmt.Errorf("slack delivered but failed to persist session metadata (channel=%q message_id=%q thread_id=%q): %w", result.Channel, result.MessageID, result.ThreadID, err)
		}
	}

	return result, nil
}

func (a *App) MCPSignalSend(ctx context.Context, text, recipient, agentID, sessionID string) (MCPSignalSendResult, error) {
	if !a.channelEnabled("signal") || a.signal == nil {
		return MCPSignalSendResult{}, disabledChannelError("signal")
	}

	delivery, err := a.signal.Send(ctx, signal.SendRequest{
		Text:      text,
		Recipient: recipient,
	})
	if err != nil {
		return MCPSignalSendResult{}, err
	}

	result := MCPSignalSendResult{
		OK:        delivery.OK,
		Recipient: delivery.Recipient,
		SentAt:    delivery.SentAt,
	}

	if shouldPersistDelivery(agentID, sessionID) {
		if err := a.persistChannelDelivery(ctx, session.OriginSignal, agentID, sessionID, "signal", "", ""); err != nil {
			return result, fmt.Errorf("signal delivered but failed to persist session metadata (recipient=%q sent_at=%q): %w", result.Recipient, result.SentAt, err)
		}
	}

	return result, nil
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

func (a *App) MCPSessionDelete(ctx context.Context, agentID, sessionID string) (bool, error) {
	resolution := ResolveSession(agentID, sessionID)
	removedSession, err := a.sessions.Delete(ctx, resolution.AgentID, resolution.SessionID)
	if err != nil {
		return false, err
	}

	removedTranscript := false
	transcriptPath, err := a.ResolveTranscriptPath(resolution.AgentID, resolution.SessionID)
	if err != nil {
		return false, err
	}
	if err := os.Remove(transcriptPath); err == nil {
		removedTranscript = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("remove transcript: %w", err)
	}

	removed := removedSession || removedTranscript
	if removed {
		a.clearSkillPromptSnapshot(resolution.SessionKey)
	}
	return removed, nil
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

func (a *App) channelEnabled(name string) bool {
	if a == nil {
		return false
	}
	if len(a.enabledChannels) == 0 {
		return false
	}
	_, ok := a.enabledChannels[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

func disabledChannelError(name string) error {
	trimmed := strings.ToLower(strings.TrimSpace(name))
	if trimmed == "" {
		trimmed = "unknown"
	}
	return fmt.Errorf("channel %q is disabled", trimmed)
}

func shouldPersistDelivery(agentID, sessionID string) bool {
	return strings.TrimSpace(agentID) != "" || strings.TrimSpace(sessionID) != ""
}

func (a *App) persistChannelDelivery(ctx context.Context, origin session.Origin, agentID, sessionID, channel, threadID, messageID string) error {
	if a.sessions == nil {
		return errors.New("session store is unavailable")
	}
	resolution := ResolveSession(agentID, sessionID)
	_, err := a.sessions.Update(ctx, resolution.AgentID, resolution.SessionID, func(entry *session.SessionEntry) error {
		if entry.Origin == "" || entry.Origin == session.OriginUnknown {
			entry.Origin = origin
		}
		if value := strings.TrimSpace(channel); value != "" {
			entry.Delivery.Channel = value
		}
		if value := strings.TrimSpace(threadID); value != "" {
			entry.Delivery.ThreadID = value
		}
		if value := strings.TrimSpace(messageID); value != "" {
			entry.Delivery.MessageID = value
		}
		return nil
	})
	return err
}
