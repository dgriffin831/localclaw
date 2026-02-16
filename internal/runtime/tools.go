package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/llm"
	"github.com/dgriffin831/localclaw/internal/memory"
	"github.com/dgriffin831/localclaw/internal/session"
	"github.com/dgriffin831/localclaw/internal/skills"
	"github.com/dgriffin831/localclaw/internal/workspace"
)

// ToolDefinitions returns runtime tools available for the current agent policy.
func (a *App) ToolDefinitions(agentID string) []skills.ToolDefinition {
	if a.tools == nil {
		return nil
	}
	defs := a.tools.List()
	filtered := make([]skills.ToolDefinition, 0, len(defs))
	for _, tool := range defs {
		if enabled, _ := a.memoryToolEnabled(agentID, tool.Name); !enabled {
			continue
		}
		filtered = append(filtered, tool)
	}
	return filtered
}

func (a *App) buildPromptRequest(ctx context.Context, resolution SessionResolution, input string, opts llm.PromptOptions) llm.Request {
	trimmedInput := strings.TrimSpace(input)
	bootstrapSection := a.buildBootstrapPromptSection(ctx, resolution)
	skillsSection := a.buildSkillsPromptSection(ctx, resolution)
	modelOverride := strings.TrimSpace(opts.ModelOverride)

	var system strings.Builder
	if bootstrapSection != "" {
		system.WriteString(bootstrapSection)
	}

	return llm.Request{
		Input:         trimmedInput,
		SystemContext: strings.TrimSpace(system.String()),
		SkillPrompt:   strings.TrimSpace(skillsSection),
		Session: llm.SessionMetadata{
			AgentID:    resolution.AgentID,
			SessionID:  resolution.SessionID,
			SessionKey: resolution.SessionKey,
		},
		Options: llm.PromptOptions{
			ModelOverride: modelOverride,
		},
	}
}

func (a *App) buildBootstrapPromptSection(ctx context.Context, resolution SessionResolution) string {
	if a.sessions == nil || a.workspace == nil {
		return ""
	}
	shouldInject, err := a.shouldInjectBootstrapContext(ctx, resolution)
	if err != nil || !shouldInject {
		return ""
	}

	files, err := a.workspace.LoadBootstrapFiles(ctx, resolution.AgentID, resolution.SessionKey)
	if err != nil {
		return ""
	}
	rendered := renderBootstrapPromptSection(files)
	if strings.TrimSpace(rendered) == "" {
		return ""
	}
	if err := a.markBootstrapInjected(ctx, resolution); err != nil {
		return ""
	}
	return rendered
}

func (a *App) shouldInjectBootstrapContext(ctx context.Context, resolution SessionResolution) (bool, error) {
	entry, exists, err := a.sessions.Get(ctx, resolution.AgentID, resolution.SessionID)
	if err != nil {
		return false, err
	}
	if !exists {
		return true, nil
	}
	if !entry.BootstrapInjected {
		return true, nil
	}
	return entry.BootstrapCompactionCount < entry.CompactionCount, nil
}

func (a *App) markBootstrapInjected(ctx context.Context, resolution SessionResolution) error {
	_, err := a.sessions.Update(ctx, resolution.AgentID, resolution.SessionID, func(entry *session.SessionEntry) error {
		entry.Key = resolution.SessionKey
		entry.BootstrapInjected = true
		entry.BootstrapCompactionCount = entry.CompactionCount
		return nil
	})
	return err
}

func renderBootstrapPromptSection(files []workspace.BootstrapFile) string {
	if len(files) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("Workspace bootstrap context (load on first message and reload after compaction).")
	b.WriteString("\nUse this as local context, but always follow higher-priority system/developer instructions.\n")

	added := 0
	for _, file := range files {
		if file.Missing {
			continue
		}
		content := strings.TrimSpace(file.Content)
		if content == "" {
			continue
		}
		b.WriteString("\n## ")
		b.WriteString(file.Name)
		b.WriteString("\n")
		b.WriteString(content)
		b.WriteString("\n")
		added++
	}
	if added == 0 {
		return ""
	}
	return strings.TrimSpace(b.String())
}

func (a *App) buildSkillsPromptSection(ctx context.Context, resolution SessionResolution) string {
	if a.skills == nil {
		return ""
	}

	compactionCount := 0
	if a.sessions != nil {
		entry, exists, err := a.sessions.Get(ctx, resolution.AgentID, resolution.SessionID)
		if err == nil && exists {
			compactionCount = entry.CompactionCount
		}
	}

	a.snapshotMu.Lock()
	cached, ok := a.skillPromptSnapshot[resolution.SessionKey]
	a.snapshotMu.Unlock()
	if ok && cached.CompactionCount == compactionCount {
		return cached.Prompt
	}

	workspacePath, err := a.ResolveWorkspacePath(resolution.AgentID)
	if err != nil {
		return ""
	}
	snapshot, err := a.skills.Snapshot(ctx, skills.SnapshotRequest{WorkspacePath: workspacePath})
	if err != nil {
		return ""
	}

	prompt := skills.RenderSnapshotPrompt(snapshot)
	a.snapshotMu.Lock()
	if strings.TrimSpace(prompt) == "" {
		delete(a.skillPromptSnapshot, resolution.SessionKey)
	} else {
		a.skillPromptSnapshot[resolution.SessionKey] = skillsSessionSnapshot{
			CompactionCount: compactionCount,
			Prompt:          prompt,
		}
	}
	a.snapshotMu.Unlock()

	return prompt
}

func (a *App) clearSkillPromptSnapshot(sessionKey string) {
	key := strings.TrimSpace(sessionKey)
	if key == "" {
		return
	}
	a.snapshotMu.Lock()
	delete(a.skillPromptSnapshot, key)
	a.snapshotMu.Unlock()
}

func (a *App) newMemoryToolManager(ctx context.Context, agentID string, memoryCfg config.MemoryConfig) (*memory.SQLiteIndexManager, func(), error) {
	resolution := ResolveSession(agentID, "")
	workspacePath, err := a.ResolveWorkspacePath(resolution.AgentID)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve workspace: %w", err)
	}
	sessionsPath, err := a.ResolveSessionsPath(resolution.AgentID)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve sessions path: %w", err)
	}
	sessionsRoot := filepath.Dir(sessionsPath)

	storePath, err := resolveStorePath(a.cfg.App.Root, memoryCfg.Store.Path, resolution.AgentID)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve memory store path: %w", err)
	}

	sourceSet := normalizeSources(memoryCfg.Sources)
	allowMemorySource := sourceSet["memory"]
	extraPaths := append([]string{}, memoryCfg.ExtraPaths...)
	if !allowMemorySource {
		extraPaths = nil
	}

	manager := memory.NewSQLiteIndexManager(memory.IndexManagerConfig{
		DBPath:               storePath,
		WorkspaceRoot:        workspacePath,
		SessionsRoot:         sessionsRoot,
		Sources:              memoryCfg.Sources,
		ExtraPaths:           extraPaths,
		ChunkTokens:          memoryCfg.Chunking.Tokens,
		ChunkOverlap:         memoryCfg.Chunking.Overlap,
		EnableFTS:            true,
		SessionDeltaBytes:    memoryCfg.Sync.Sessions.DeltaBytes,
		SessionDeltaMessages: memoryCfg.Sync.Sessions.DeltaMessages,
	})
	if err := manager.Open(ctx); err != nil {
		return nil, nil, fmt.Errorf("open memory index: %w", err)
	}

	return manager, func() { _ = manager.Close() }, nil
}

func (a *App) memoryToolEnabled(agentID, toolName string) (bool, string) {
	name := normalizeToolName(toolName)
	if name == "" {
		return false, "tool name is required"
	}
	if name != skills.ToolMemorySearch && name != skills.ToolMemoryGet && name != skills.ToolMemoryGrep {
		return true, ""
	}

	resolvedAgentID := ResolveAgentID(agentID)
	memoryCfg := a.resolveMemoryConfig(resolvedAgentID)
	if !memoryCfg.Enabled {
		return false, fmt.Sprintf("memory tools are disabled for agent %q", resolvedAgentID)
	}

	switch name {
	case skills.ToolMemorySearch:
		if !memoryCfg.Tools.Search {
			return false, fmt.Sprintf("memory_search is disabled for agent %q", resolvedAgentID)
		}
	case skills.ToolMemoryGet:
		if !memoryCfg.Tools.Get {
			return false, fmt.Sprintf("memory_get is disabled for agent %q", resolvedAgentID)
		}
	case skills.ToolMemoryGrep:
		if !memoryCfg.Tools.Grep {
			return false, fmt.Sprintf("memory_grep is disabled for agent %q", resolvedAgentID)
		}
	}

	return true, ""
}

func (a *App) resolveMemoryConfig(agentID string) config.MemoryConfig {
	resolved := a.cfg.Agents.Defaults.Memory
	normalizedAgentID := ResolveAgentID(agentID)
	for _, agent := range a.cfg.Agents.List {
		if ResolveAgentID(agent.ID) != normalizedAgentID {
			continue
		}
		if hasMemoryOverride(agent.Memory) {
			resolved = mergeMemoryConfig(resolved, agent.Memory)
		}
		break
	}
	return resolved
}

func hasMemoryOverride(cfg config.MemoryOverrideConfig) bool {
	return cfg.Enabled != nil ||
		cfg.Tools.Get != nil ||
		cfg.Tools.Search != nil ||
		cfg.Tools.Grep != nil ||
		len(cfg.Sources) > 0 ||
		len(cfg.ExtraPaths) > 0 ||
		strings.TrimSpace(cfg.Store.Path) != "" ||
		cfg.Chunking.Tokens > 0 ||
		cfg.Chunking.Overlap > 0 ||
		cfg.Query.MaxResults > 0 ||
		cfg.Query.MinScore > 0 ||
		cfg.Sync.OnSearch ||
		cfg.Sync.Sessions.DeltaBytes > 0 ||
		cfg.Sync.Sessions.DeltaMessages > 0
}

func mergeMemoryConfig(base config.MemoryConfig, override config.MemoryOverrideConfig) config.MemoryConfig {
	merged := base
	if override.Enabled != nil {
		merged.Enabled = *override.Enabled
	}
	if override.Tools.Get != nil {
		merged.Tools.Get = *override.Tools.Get
	}
	if override.Tools.Search != nil {
		merged.Tools.Search = *override.Tools.Search
	}
	if override.Tools.Grep != nil {
		merged.Tools.Grep = *override.Tools.Grep
	}
	if len(override.Sources) > 0 {
		merged.Sources = append([]string{}, override.Sources...)
	}
	if len(override.ExtraPaths) > 0 {
		merged.ExtraPaths = append([]string{}, override.ExtraPaths...)
	}
	if strings.TrimSpace(override.Store.Path) != "" {
		merged.Store.Path = override.Store.Path
	}
	if override.Chunking.Tokens > 0 {
		merged.Chunking.Tokens = override.Chunking.Tokens
	}
	if override.Chunking.Overlap > 0 {
		merged.Chunking.Overlap = override.Chunking.Overlap
	}
	if override.Query.MaxResults > 0 {
		merged.Query.MaxResults = override.Query.MaxResults
	}
	if override.Query.MinScore > 0 {
		merged.Query.MinScore = override.Query.MinScore
	}
	if override.Sync.OnSearch {
		merged.Sync.OnSearch = true
	}
	if override.Sync.Sessions.DeltaBytes > 0 {
		merged.Sync.Sessions.DeltaBytes = override.Sync.Sessions.DeltaBytes
	}
	if override.Sync.Sessions.DeltaMessages > 0 {
		merged.Sync.Sessions.DeltaMessages = override.Sync.Sessions.DeltaMessages
	}
	return merged
}

func normalizeSources(values []string) map[string]bool {
	out := map[string]bool{}
	for _, raw := range values {
		source := strings.ToLower(strings.TrimSpace(raw))
		if source == "" {
			continue
		}
		out[source] = true
	}
	return out
}

func normalizeToolName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func resolveStorePath(stateRoot string, storePattern string, agentID string) (string, error) {
	pattern := strings.TrimSpace(storePattern)
	if pattern == "" {
		return "", errors.New("memory.store.path is required")
	}

	pattern = strings.ReplaceAll(pattern, "{agentId}", agentID)
	resolved, err := expandPath(pattern)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(resolved) {
		return filepath.Clean(resolved), nil
	}

	root, err := expandPath(stateRoot)
	if err != nil {
		return "", err
	}
	return filepath.Clean(filepath.Join(root, resolved)), nil
}

func expandPath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", errors.New("path is empty")
	}
	if trimmed == "~" || strings.HasPrefix(trimmed, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if trimmed == "~" {
			return filepath.Clean(home), nil
		}
		return filepath.Clean(filepath.Join(home, strings.TrimPrefix(trimmed, "~/"))), nil
	}
	return filepath.Clean(trimmed), nil
}
