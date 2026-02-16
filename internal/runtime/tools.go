package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/llm"
	"github.com/dgriffin831/localclaw/internal/memory"
	"github.com/dgriffin831/localclaw/internal/session"
	"github.com/dgriffin831/localclaw/internal/skills"
	"github.com/dgriffin831/localclaw/internal/workspace"
)

const memoryRecallPolicyPrompt = `System policy:
- Memory recall is mandatory before finalizing an answer when tools are available.
- First call memory_search with the user's intent and review the top matches.
- If details are incomplete, call memory_get for exact lines from relevant files.
- If a tool fails or returns no relevant data, continue with best effort and explicitly note that memory recall was unavailable or empty.`

type ToolExecutionRequest struct {
	AgentID   string
	SessionID string
	Name      string
	Class     llm.ToolClass
	Args      map[string]interface{}
}

type ToolExecutionResult struct {
	Tool  string                 `json:"tool"`
	Class llm.ToolClass          `json:"class,omitempty"`
	OK    bool                   `json:"ok"`
	Data  map[string]interface{} `json:"data,omitempty"`
	Error string                 `json:"error,omitempty"`
}

type DelegatedToolExecutor interface {
	ExecuteDelegatedTool(ctx context.Context, req ToolExecutionRequest) ToolExecutionResult
}

func (a *App) SetDelegatedToolExecutor(executor DelegatedToolExecutor) {
	a.delegated = executor
}

// ToolDefinitions returns runtime tools available for the current agent policy.
func (a *App) ToolDefinitions(agentID string) []skills.ToolDefinition {
	if !a.toolsEnabled(agentID) || a.tools == nil {
		return nil
	}
	defs := a.tools.List()
	filtered := make([]skills.ToolDefinition, 0, len(defs))
	for _, tool := range defs {
		if allowed, _ := a.evaluateToolPolicy(agentID, tool.Name, llm.ToolClassLocal); !allowed {
			continue
		}
		filtered = append(filtered, tool)
	}
	return filtered
}

// ExecuteTool invokes one registered runtime tool and degrades gracefully on failures.
func (a *App) ExecuteTool(ctx context.Context, req ToolExecutionRequest) ToolExecutionResult {
	toolName := normalizeToolName(req.Name)
	toolClass := req.Class
	result := ToolExecutionResult{
		Tool:  toolName,
		Class: toolClass,
		OK:    false,
	}

	if toolName == "" {
		result.Error = "tool name is required"
		return result
	}

	if toolClass == llm.ToolClassUnspecified {
		if a.tools != nil {
			if _, ok := a.tools.Get(toolName); ok {
				toolClass = llm.ToolClassLocal
			}
		}
		if toolClass == llm.ToolClassUnspecified {
			toolClass = llm.ToolClassLocal
		}
		result.Class = toolClass
	}

	if toolClass == llm.ToolClassDelegated {
		allowed, reason := a.evaluateToolPolicy(req.AgentID, toolName, llm.ToolClassDelegated)
		if !allowed {
			result.Error = fmt.Sprintf("tool policy blocked: %s", reason)
			return result
		}
		if a.delegated == nil {
			result.Error = "delegated tool execution is not configured"
			return result
		}
		delegatedResult := a.delegated.ExecuteDelegatedTool(ctx, req)
		if delegatedResult.Tool == "" {
			delegatedResult.Tool = toolName
		}
		if delegatedResult.Class == llm.ToolClassUnspecified {
			delegatedResult.Class = llm.ToolClassDelegated
		}
		return delegatedResult
	}

	if !a.toolsEnabled(req.AgentID) {
		result.Error = "runtime tools are disabled"
		return result
	}
	if a.tools == nil {
		result.Error = "tool registry unavailable"
		return result
	}
	if _, ok := a.tools.Get(toolName); !ok {
		result.Error = fmt.Sprintf("unknown tool %q", toolName)
		return result
	}
	allowed, reason := a.evaluateToolPolicy(req.AgentID, toolName, llm.ToolClassLocal)
	if !allowed {
		result.Error = fmt.Sprintf("tool policy blocked: %s", reason)
		return result
	}

	switch toolName {
	case skills.ToolMemorySearch:
		out := a.executeMemorySearchTool(ctx, req)
		out.Class = llm.ToolClassLocal
		return out
	case skills.ToolMemoryGet:
		out := a.executeMemoryGetTool(ctx, req)
		out.Class = llm.ToolClassLocal
		return out
	default:
		result.Error = fmt.Sprintf("unsupported tool %q", toolName)
		return result
	}
}

func (a *App) buildPromptInput(ctx context.Context, agentID, sessionID, input string) string {
	resolution := ResolveSession(agentID, sessionID)
	req := a.buildPromptRequest(ctx, resolution, input)
	return llm.ComposePromptFallback(req)
}

func (a *App) buildPromptRequest(ctx context.Context, resolution SessionResolution, input string) llm.Request {
	trimmedInput := strings.TrimSpace(input)
	bootstrapSection := a.buildBootstrapPromptSection(ctx, resolution)
	skillsSection := a.buildSkillsPromptSection(ctx, resolution)

	toolLines := make([]string, 0, 8)
	toolDefs := make([]llm.ToolDefinition, 0, 8)
	if a.toolsEnabled(resolution.AgentID) {
		for _, tool := range a.ToolDefinitions(resolution.AgentID) {
			paramParts := make([]string, 0, len(tool.Parameters))
			params := make([]llm.ToolParameter, 0, len(tool.Parameters))
			for _, param := range tool.Parameters {
				suffix := " optional"
				if param.Required {
					suffix = " required"
				}
				paramParts = append(paramParts, fmt.Sprintf("%s:%s (%s)", param.Name, param.Type, suffix))
				params = append(params, llm.ToolParameter{
					Name:        param.Name,
					Type:        param.Type,
					Required:    param.Required,
					Description: param.Description,
				})
			}
			toolLines = append(toolLines, fmt.Sprintf("- %s: %s | args: %s", tool.Name, tool.Description, strings.Join(paramParts, ", ")))
			toolDefs = append(toolDefs, llm.ToolDefinition{
				Name:        tool.Name,
				Description: tool.Description,
				Class:       llm.ToolClassLocal,
				Parameters:  params,
			})
		}
	}

	var system strings.Builder
	if bootstrapSection != "" {
		system.WriteString(bootstrapSection)
	}
	if len(toolLines) > 0 {
		if system.Len() > 0 {
			system.WriteString("\n\n")
		}
		system.WriteString(memoryRecallPolicyPrompt)
		system.WriteString("\n\nAvailable tools:\n")
		system.WriteString(strings.Join(toolLines, "\n"))
		system.WriteString("\n\nCurrent session_key: ")
		system.WriteString(resolution.SessionKey)
	}

	return llm.Request{
		Input:           trimmedInput,
		SystemContext:   strings.TrimSpace(system.String()),
		SkillPrompt:     strings.TrimSpace(skillsSection),
		ToolDefinitions: toolDefs,
		Session: llm.SessionMetadata{
			AgentID:    resolution.AgentID,
			SessionID:  resolution.SessionID,
			SessionKey: resolution.SessionKey,
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
	skillsCfg := a.resolveSkillsConfig(resolution.AgentID)
	snapshot, err := a.skills.Snapshot(ctx, skills.SnapshotRequest{
		WorkspacePath: workspacePath,
		Enabled:       skillsCfg.Enabled,
		Disabled:      skillsCfg.Disabled,
	})
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

func (a *App) executeMemorySearchTool(ctx context.Context, req ToolExecutionRequest) ToolExecutionResult {
	result := ToolExecutionResult{Tool: skills.ToolMemorySearch, OK: false}

	query, err := stringArg(req.Args, "query", true)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	maxResults, err := intArg(req.Args, "max_results")
	if err != nil {
		result.Error = err.Error()
		return result
	}
	minScore, err := floatArg(req.Args, "min_score")
	if err != nil {
		result.Error = err.Error()
		return result
	}
	sessionKey, err := stringArg(req.Args, "session_key", false)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if sessionKey == "" {
		sessionKey = ResolveSession(req.AgentID, req.SessionID).SessionKey
	}

	searchCfg := a.resolveMemorySearchConfig(req.AgentID)
	manager, cleanup, toolErr := a.newMemoryToolManager(ctx, req.AgentID, searchCfg)
	if toolErr != nil {
		result.Error = toolErr.Error()
		return result
	}
	defer cleanup()

	if searchCfg.Sync.OnSearch {
		if _, err := manager.Sync(ctx, false); err != nil {
			result.Error = fmt.Sprintf("memory_search sync failed: %v", err)
			return result
		}
	}

	opts := memory.SearchOptions{
		MaxResults: maxResults,
		MinScore:   minScore,
		SessionKey: sessionKey,
	}
	if opts.MaxResults <= 0 {
		opts.MaxResults = searchCfg.Query.MaxResults
	}
	if opts.MaxResults <= 0 {
		opts.MaxResults = 8
	}
	if !argPresent(req.Args, "min_score") {
		opts.MinScore = searchCfg.Query.MinScore
	}

	results, err := manager.Search(ctx, query, opts)
	if err != nil {
		result.Error = fmt.Sprintf("memory_search failed: %v", err)
		return result
	}

	result.OK = true
	result.Data = map[string]interface{}{
		"results": results,
		"count":   len(results),
	}
	return result
}

func (a *App) executeMemoryGetTool(ctx context.Context, req ToolExecutionRequest) ToolExecutionResult {
	result := ToolExecutionResult{Tool: skills.ToolMemoryGet, OK: false}

	path, err := stringArg(req.Args, "path", true)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	fromLine, err := intArg(req.Args, "from_line")
	if err != nil {
		result.Error = err.Error()
		return result
	}
	lines, err := intArg(req.Args, "lines")
	if err != nil {
		result.Error = err.Error()
		return result
	}

	searchCfg := a.resolveMemorySearchConfig(req.AgentID)
	manager, cleanup, toolErr := a.newMemoryToolManager(ctx, req.AgentID, searchCfg)
	if toolErr != nil {
		result.Error = toolErr.Error()
		return result
	}
	defer cleanup()

	out, err := manager.Get(ctx, path, memory.GetOptions{FromLine: fromLine, Lines: lines})
	if err != nil {
		result.Error = fmt.Sprintf("memory_get failed: %v", err)
		return result
	}

	result.OK = true
	result.Data = map[string]interface{}{
		"result": out,
	}
	return result
}

func (a *App) newMemoryToolManager(ctx context.Context, agentID string, searchCfg config.MemorySearchConfig) (*memory.SQLiteIndexManager, func(), error) {
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

	storePath, err := resolveStorePath(a.cfg.State.Root, searchCfg.Store.Path, resolution.AgentID)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve memory store path: %w", err)
	}

	sourceSet := normalizeSources(searchCfg.Sources)
	allowMemorySource := sourceSet["memory"]
	extraPaths := append([]string{}, searchCfg.ExtraPaths...)
	if !allowMemorySource {
		extraPaths = nil
	}

	manager := memory.NewSQLiteIndexManager(memory.IndexManagerConfig{
		DBPath:               storePath,
		WorkspaceRoot:        workspacePath,
		SessionsRoot:         sessionsRoot,
		Sources:              searchCfg.Sources,
		ExtraPaths:           extraPaths,
		ChunkTokens:          searchCfg.Chunking.Tokens,
		ChunkOverlap:         searchCfg.Chunking.Overlap,
		Provider:             searchCfg.Provider,
		Model:                searchCfg.Model,
		Fallback:             searchCfg.Fallback,
		Local:                memory.LocalEmbeddingConfig{ModelPath: searchCfg.Local.ModelPath, ModelCacheDir: searchCfg.Local.ModelCacheDir},
		EnableFTS:            true,
		EnableVector:         searchCfg.Store.Vector.Enabled,
		EnableEmbeddingCache: searchCfg.Cache.Enabled,
		EmbeddingCacheMax:    searchCfg.Cache.MaxEntries,
		HybridEnabled:        searchCfg.Query.Hybrid.Enabled,
		VectorWeight:         searchCfg.Query.Hybrid.VectorWeight,
		KeywordWeight:        searchCfg.Query.Hybrid.KeywordWeight,
		CandidateMultiplier:  searchCfg.Query.Hybrid.CandidateMultiplier,
		SessionDeltaBytes:    searchCfg.Sync.Sessions.DeltaBytes,
		SessionDeltaMessages: searchCfg.Sync.Sessions.DeltaMessages,
	})
	if err := manager.Open(ctx); err != nil {
		return nil, nil, fmt.Errorf("open memory index: %w", err)
	}

	return manager, func() { _ = manager.Close() }, nil
}

func (a *App) toolsEnabled(agentID string) bool {
	return a.resolveMemorySearchConfig(agentID).Enabled
}

func (a *App) resolveMemorySearchConfig(agentID string) config.MemorySearchConfig {
	resolved := a.cfg.Agents.Defaults.MemorySearch
	normalizedAgentID := ResolveAgentID(agentID)
	for _, agent := range a.cfg.Agents.List {
		if ResolveAgentID(agent.ID) != normalizedAgentID {
			continue
		}
		override := agent.MemorySearch
		if !hasMemorySearchOverride(override) {
			break
		}
		resolved = mergeMemorySearchConfig(resolved, override)
		break
	}
	return resolved
}

func (a *App) resolveToolsConfig(agentID string) config.ToolsConfig {
	resolved := a.cfg.Tools
	if hasToolsOverride(a.cfg.Agents.Defaults.Tools) {
		resolved = mergeToolsConfig(resolved, a.cfg.Agents.Defaults.Tools)
	}

	normalizedAgentID := ResolveAgentID(agentID)
	for _, agent := range a.cfg.Agents.List {
		if ResolveAgentID(agent.ID) != normalizedAgentID {
			continue
		}
		if hasToolsOverride(agent.Tools) {
			resolved = mergeToolsConfig(resolved, agent.Tools)
		}
		break
	}
	return resolved
}

func hasToolsOverride(cfg config.ToolsConfig) bool {
	return len(cfg.Allow) > 0 ||
		len(cfg.Deny) > 0 ||
		cfg.Delegated.Enabled ||
		len(cfg.Delegated.Allow) > 0 ||
		len(cfg.Delegated.Deny) > 0
}

func mergeToolsConfig(base, override config.ToolsConfig) config.ToolsConfig {
	merged := base
	if len(override.Allow) > 0 || len(override.Deny) > 0 {
		merged.Allow = append([]string{}, override.Allow...)
		merged.Deny = append([]string{}, override.Deny...)
	}
	if override.Delegated.Enabled {
		merged.Delegated.Enabled = true
	}
	if len(override.Delegated.Allow) > 0 || len(override.Delegated.Deny) > 0 {
		merged.Delegated.Allow = append([]string{}, override.Delegated.Allow...)
		merged.Delegated.Deny = append([]string{}, override.Delegated.Deny...)
	}
	return merged
}

func (a *App) resolveSkillsConfig(agentID string) config.SkillsConfig {
	resolved := a.cfg.Skills
	if hasSkillsOverride(a.cfg.Agents.Defaults.Skills) {
		resolved = mergeSkillsConfig(resolved, a.cfg.Agents.Defaults.Skills)
	}

	normalizedAgentID := ResolveAgentID(agentID)
	for _, agent := range a.cfg.Agents.List {
		if ResolveAgentID(agent.ID) != normalizedAgentID {
			continue
		}
		if hasSkillsOverride(agent.Skills) {
			resolved = mergeSkillsConfig(resolved, agent.Skills)
		}
		break
	}
	return resolved
}

func hasSkillsOverride(cfg config.SkillsConfig) bool {
	return len(cfg.Enabled) > 0 || len(cfg.Disabled) > 0
}

func mergeSkillsConfig(base, override config.SkillsConfig) config.SkillsConfig {
	merged := base
	if len(override.Enabled) > 0 || len(override.Disabled) > 0 {
		merged.Enabled = append([]string{}, override.Enabled...)
		merged.Disabled = append([]string{}, override.Disabled...)
	}
	return merged
}

func (a *App) evaluateToolPolicy(agentID string, toolName string, class llm.ToolClass) (bool, string) {
	name := normalizeToolName(toolName)
	if name == "" {
		return false, "tool name is required"
	}

	policy := a.resolveToolsConfig(agentID)
	if class == llm.ToolClassDelegated {
		if !policy.Delegated.Enabled {
			return false, "delegated tools are disabled"
		}
		if matchToolList(policy.Delegated.Deny, name) {
			return false, fmt.Sprintf("delegated tool %q denied by policy", name)
		}
		if len(policy.Delegated.Allow) == 0 {
			return false, fmt.Sprintf("delegated tool %q is not allowlisted", name)
		}
		if !matchToolList(policy.Delegated.Allow, name) {
			return false, fmt.Sprintf("delegated tool %q is not allowlisted", name)
		}
		return true, ""
	}

	if matchToolList(policy.Deny, name) {
		return false, fmt.Sprintf("tool %q denied by policy", name)
	}
	if len(policy.Allow) > 0 && !matchToolList(policy.Allow, name) {
		return false, fmt.Sprintf("tool %q is not allowlisted", name)
	}
	return true, ""
}

func hasMemorySearchOverride(cfg config.MemorySearchConfig) bool {
	return cfg.Enabled ||
		len(cfg.Sources) > 0 ||
		len(cfg.ExtraPaths) > 0 ||
		strings.TrimSpace(cfg.Provider) != "" ||
		strings.TrimSpace(cfg.Fallback) != "" ||
		strings.TrimSpace(cfg.Model) != "" ||
		strings.TrimSpace(cfg.Store.Path) != "" ||
		cfg.Store.Vector.Enabled ||
		cfg.Chunking.Tokens > 0 ||
		cfg.Chunking.Overlap > 0 ||
		cfg.Query.MaxResults > 0 ||
		cfg.Query.MinScore > 0 ||
		cfg.Query.Hybrid.Enabled ||
		cfg.Query.Hybrid.VectorWeight > 0 ||
		cfg.Query.Hybrid.KeywordWeight > 0 ||
		cfg.Query.Hybrid.CandidateMultiplier > 0 ||
		cfg.Sync.OnSessionStart ||
		cfg.Sync.OnSearch ||
		cfg.Sync.Watch ||
		cfg.Sync.WatchDebounceMs > 0 ||
		cfg.Sync.IntervalMinutes > 0 ||
		cfg.Sync.Sessions.DeltaBytes > 0 ||
		cfg.Sync.Sessions.DeltaMessages > 0 ||
		cfg.Cache.Enabled ||
		cfg.Cache.MaxEntries > 0 ||
		strings.TrimSpace(cfg.Local.ModelPath) != "" ||
		strings.TrimSpace(cfg.Local.ModelCacheDir) != ""
}

func mergeMemorySearchConfig(base, override config.MemorySearchConfig) config.MemorySearchConfig {
	merged := base
	if override.Enabled {
		merged.Enabled = true
	}
	if len(override.Sources) > 0 {
		merged.Sources = append([]string{}, override.Sources...)
	}
	if len(override.ExtraPaths) > 0 {
		merged.ExtraPaths = append([]string{}, override.ExtraPaths...)
	}
	if strings.TrimSpace(override.Provider) != "" {
		merged.Provider = override.Provider
	}
	if strings.TrimSpace(override.Fallback) != "" {
		merged.Fallback = override.Fallback
	}
	if strings.TrimSpace(override.Model) != "" {
		merged.Model = override.Model
	}
	if strings.TrimSpace(override.Store.Path) != "" {
		merged.Store.Path = override.Store.Path
	}
	if override.Store.Vector.Enabled {
		merged.Store.Vector.Enabled = true
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
	if override.Query.Hybrid.Enabled {
		merged.Query.Hybrid.Enabled = true
	}
	if override.Query.Hybrid.VectorWeight > 0 {
		merged.Query.Hybrid.VectorWeight = override.Query.Hybrid.VectorWeight
	}
	if override.Query.Hybrid.KeywordWeight > 0 {
		merged.Query.Hybrid.KeywordWeight = override.Query.Hybrid.KeywordWeight
	}
	if override.Query.Hybrid.CandidateMultiplier > 0 {
		merged.Query.Hybrid.CandidateMultiplier = override.Query.Hybrid.CandidateMultiplier
	}
	if override.Sync.OnSessionStart {
		merged.Sync.OnSessionStart = true
	}
	if override.Sync.OnSearch {
		merged.Sync.OnSearch = true
	}
	if override.Sync.Watch {
		merged.Sync.Watch = true
	}
	if override.Sync.WatchDebounceMs > 0 {
		merged.Sync.WatchDebounceMs = override.Sync.WatchDebounceMs
	}
	if override.Sync.IntervalMinutes > 0 {
		merged.Sync.IntervalMinutes = override.Sync.IntervalMinutes
	}
	if override.Sync.Sessions.DeltaBytes > 0 {
		merged.Sync.Sessions.DeltaBytes = override.Sync.Sessions.DeltaBytes
	}
	if override.Sync.Sessions.DeltaMessages > 0 {
		merged.Sync.Sessions.DeltaMessages = override.Sync.Sessions.DeltaMessages
	}
	if override.Cache.Enabled {
		merged.Cache.Enabled = true
	}
	if override.Cache.MaxEntries > 0 {
		merged.Cache.MaxEntries = override.Cache.MaxEntries
	}
	if strings.TrimSpace(override.Local.ModelPath) != "" {
		merged.Local.ModelPath = override.Local.ModelPath
	}
	if strings.TrimSpace(override.Local.ModelCacheDir) != "" {
		merged.Local.ModelCacheDir = override.Local.ModelCacheDir
	}
	return merged
}

func argPresent(args map[string]interface{}, name string) bool {
	if args == nil {
		return false
	}
	_, ok := args[name]
	return ok
}

func stringArg(args map[string]interface{}, name string, required bool) (string, error) {
	if args == nil {
		if required {
			return "", fmt.Errorf("%s is required", name)
		}
		return "", nil
	}
	raw, ok := args[name]
	if !ok || raw == nil {
		if required {
			return "", fmt.Errorf("%s is required", name)
		}
		return "", nil
	}
	out, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", name)
	}
	out = strings.TrimSpace(out)
	if out == "" && required {
		return "", fmt.Errorf("%s is required", name)
	}
	return out, nil
}

func intArg(args map[string]interface{}, name string) (int, error) {
	if args == nil {
		return 0, nil
	}
	raw, ok := args[name]
	if !ok || raw == nil {
		return 0, nil
	}
	switch v := raw.(type) {
	case int:
		return v, nil
	case int32:
		return int(v), nil
	case int64:
		return int(v), nil
	case float32:
		if float32(int(v)) != v {
			return 0, fmt.Errorf("%s must be an integer", name)
		}
		return int(v), nil
	case float64:
		if float64(int(v)) != v {
			return 0, fmt.Errorf("%s must be an integer", name)
		}
		return int(v), nil
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0, fmt.Errorf("%s must be an integer", name)
		}
		return n, nil
	default:
		return 0, fmt.Errorf("%s must be an integer", name)
	}
}

func floatArg(args map[string]interface{}, name string) (float64, error) {
	if args == nil {
		return 0, nil
	}
	raw, ok := args[name]
	if !ok || raw == nil {
		return 0, nil
	}
	switch v := raw.(type) {
	case int:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case float32:
		return float64(v), nil
	case float64:
		return v, nil
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, fmt.Errorf("%s must be a number", name)
		}
		return n, nil
	default:
		return 0, fmt.Errorf("%s must be a number", name)
	}
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

func matchToolList(values []string, toolName string) bool {
	normalizedTool := normalizeToolName(toolName)
	if normalizedTool == "" {
		return false
	}
	for _, raw := range values {
		pattern := normalizeToolName(raw)
		if pattern == "" {
			continue
		}
		if pattern == "*" || pattern == normalizedTool {
			return true
		}
	}
	return false
}

func normalizeToolName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func resolveStorePath(stateRoot string, storePattern string, agentID string) (string, error) {
	pattern := strings.TrimSpace(storePattern)
	if pattern == "" {
		return "", errors.New("memorySearch.store.path is required")
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
