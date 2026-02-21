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

func (a *App) buildPromptRequest(ctx context.Context, resolution SessionResolution, input string, opts llm.PromptOptions) (llm.Request, error) {
	trimmedInput := strings.TrimSpace(input)
	bootstrapSection := a.buildBootstrapPromptSection(ctx, resolution)
	skillsSection := a.buildSkillsPromptSection(ctx, resolution)
	provider := a.resolveProvider(opts.ProviderOverride)
	modelOverride := strings.TrimSpace(opts.ModelOverride)
	reasoningOverride := strings.TrimSpace(opts.ReasoningOverride)
	if strings.EqualFold(provider, "codex") && reasoningOverride == "" {
		reasoningOverride = strings.TrimSpace(a.cfg.LLM.Codex.ReasoningDefault)
	}
	providerSessionID := a.loadPersistedProviderSessionID(ctx, resolution, provider)
	workspacePath, securityMode, err := a.resolveSecurityRequestContext(resolution.AgentID)
	if err != nil {
		return llm.Request{}, fmt.Errorf("resolve workspace: %w", err)
	}

	var system strings.Builder
	if bootstrapSection != "" {
		system.WriteString(bootstrapSection)
	}

	return llm.Request{
		Input:         trimmedInput,
		SystemContext: strings.TrimSpace(system.String()),
		SkillPrompt:   strings.TrimSpace(skillsSection),
		Session: llm.SessionMetadata{
			AgentID:           resolution.AgentID,
			SessionID:         resolution.SessionID,
			SessionKey:        resolution.SessionKey,
			Provider:          provider,
			ProviderSessionID: providerSessionID,
			WorkspacePath:     workspacePath,
			SecurityMode:      securityMode,
		},
		Options: llm.PromptOptions{
			ModelOverride:     modelOverride,
			ReasoningOverride: reasoningOverride,
		},
	}, nil
}

func (a *App) resolveSecurityRequestContext(agentID string) (workspacePath string, securityMode string, err error) {
	resolvedWorkspacePath, resolveErr := a.ResolveWorkspacePath(agentID)
	if resolveErr != nil {
		return "", "", resolveErr
	}
	return resolvedWorkspacePath, strings.ToLower(strings.TrimSpace(a.cfg.Security.Mode)), nil
}

func (a *App) loadPersistedProviderSessionID(ctx context.Context, resolution SessionResolution, provider string) string {
	if a.sessions == nil {
		return ""
	}
	entry, exists, err := a.sessions.Get(ctx, resolution.AgentID, resolution.SessionID)
	if err != nil || !exists {
		return ""
	}
	return session.GetProviderSessionID(entry, provider)
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
	return ResolveMemoryConfig(a.cfg, agentID)
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
