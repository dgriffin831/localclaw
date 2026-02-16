package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Registry tracks locally available skills.
type Registry interface {
	Load(ctx context.Context) error
	List(ctx context.Context) ([]string, error)
	Snapshot(ctx context.Context, req SnapshotRequest) (Snapshot, error)
}

const (
	ToolMemorySearch = "memory_search"
	ToolMemoryGet    = "memory_get"
)

type ToolParameter struct {
	Name        string
	Type        string
	Required    bool
	Description string
}

type ToolDefinition struct {
	Name        string
	Description string
	Parameters  []ToolParameter
}

type Skill struct {
	Name                   string
	Description            string
	Path                   string
	UserInvocable          bool
	DisableModelInvocation bool
}

type Snapshot struct {
	Skills []Skill
}

type SnapshotRequest struct {
	WorkspacePath string
	Enabled       []string
	Disabled      []string
}

type LocalRegistrySettings struct {
	AgentIDs         []string
	ResolveWorkspace func(agentID string) (string, error)
}

// ToolRegistry tracks callable local runtime tools.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]ToolDefinition
}

type LocalRegistry struct {
	settings LocalRegistrySettings

	mu     sync.RWMutex
	loaded map[string][]Skill
}

func NewLocalRegistry(settings ...LocalRegistrySettings) *LocalRegistry {
	cfg := LocalRegistrySettings{}
	if len(settings) > 0 {
		cfg = settings[0]
	}
	return &LocalRegistry{
		settings: cfg,
		loaded:   map[string][]Skill{},
	}
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: map[string]ToolDefinition{},
	}
}

func DefaultToolRegistry() *ToolRegistry {
	registry := NewToolRegistry()
	for _, tool := range DefaultMemoryTools() {
		registry.Register(tool)
	}
	return registry
}

func DefaultMemoryTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        ToolMemorySearch,
			Description: "Search indexed memory snippets relevant to the current request.",
			Parameters: []ToolParameter{
				{Name: "query", Type: "string", Required: true, Description: "Search query text."},
				{Name: "max_results", Type: "integer", Required: false, Description: "Optional result cap."},
				{Name: "min_score", Type: "number", Required: false, Description: "Optional minimum score filter."},
				{Name: "session_key", Type: "string", Required: false, Description: "Optional session key context."},
			},
		},
		{
			Name:        ToolMemoryGet,
			Description: "Read a memory markdown file within allowed memory scope.",
			Parameters: []ToolParameter{
				{Name: "path", Type: "string", Required: true, Description: "Workspace-relative markdown path."},
				{Name: "from_line", Type: "integer", Required: false, Description: "1-based starting line."},
				{Name: "lines", Type: "integer", Required: false, Description: "Optional line count."},
			},
		},
	}
}

func (r *ToolRegistry) Register(tool ToolDefinition) {
	name := strings.TrimSpace(tool.Name)
	if name == "" {
		return
	}
	tool.Name = name
	r.mu.Lock()
	r.tools[name] = tool
	r.mu.Unlock()
}

func (r *ToolRegistry) Get(name string) (ToolDefinition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tool, ok := r.tools[strings.TrimSpace(name)]
	return tool, ok
}

func (r *ToolRegistry) List() []ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ToolDefinition, 0, len(r.tools))
	for _, tool := range r.tools {
		out = append(out, tool)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

func (r *LocalRegistry) Load(ctx context.Context) error {
	if r.settings.ResolveWorkspace == nil {
		return nil
	}

	agentIDs := make([]string, 0, len(r.settings.AgentIDs)+1)
	agentIDs = append(agentIDs, "default")
	agentIDs = append(agentIDs, r.settings.AgentIDs...)
	seen := map[string]struct{}{}

	for _, agentID := range agentIDs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		normalizedAgentID := strings.TrimSpace(agentID)
		if normalizedAgentID == "" {
			normalizedAgentID = "default"
		}
		if _, ok := seen[normalizedAgentID]; ok {
			continue
		}
		seen[normalizedAgentID] = struct{}{}

		workspacePath, err := r.settings.ResolveWorkspace(normalizedAgentID)
		if err != nil {
			continue
		}
		snapshot, err := discoverSkills(workspacePath, nil, nil)
		if err != nil {
			return err
		}
		r.mu.Lock()
		r.loaded[workspacePath] = append([]Skill{}, snapshot.Skills...)
		r.mu.Unlock()
	}
	return nil
}

func (r *LocalRegistry) List(ctx context.Context) ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := map[string]struct{}{}
	for _, skillSet := range r.loaded {
		for _, skill := range skillSet {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
			names[skill.Name] = struct{}{}
		}
	}

	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

func (r *LocalRegistry) Snapshot(ctx context.Context, req SnapshotRequest) (Snapshot, error) {
	select {
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	default:
	}

	workspacePath := strings.TrimSpace(req.WorkspacePath)
	if workspacePath == "" {
		return Snapshot{}, fmt.Errorf("workspace path is required")
	}

	snapshot, err := discoverSkills(workspacePath, req.Enabled, req.Disabled)
	if err != nil {
		return Snapshot{}, err
	}

	r.mu.Lock()
	r.loaded[workspacePath] = append([]Skill{}, snapshot.Skills...)
	r.mu.Unlock()

	return snapshot, nil
}

func RenderSnapshotPrompt(snapshot Snapshot) string {
	modelSkills := make([]Skill, 0, len(snapshot.Skills))
	for _, skill := range snapshot.Skills {
		if skill.DisableModelInvocation {
			continue
		}
		modelSkills = append(modelSkills, skill)
	}
	if len(modelSkills) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("Available localclaw skills (workspace-managed):\n")
	for _, skill := range modelSkills {
		description := strings.TrimSpace(skill.Description)
		if description == "" {
			description = "No description provided."
		}
		description = truncate(description, 180)
		line := fmt.Sprintf("- %s: %s", skill.Name, description)
		if !skill.UserInvocable {
			line += " [user-invocable=false]"
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("When a skill is relevant, read the skill's SKILL.md before executing it.")
	return strings.TrimSpace(b.String())
}

func discoverSkills(workspacePath string, enabled []string, disabled []string) (Snapshot, error) {
	skillsDir := filepath.Join(workspacePath, "skills")
	info, err := os.Stat(skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return Snapshot{}, nil
		}
		return Snapshot{}, fmt.Errorf("stat skills directory: %w", err)
	}
	if !info.IsDir() {
		return Snapshot{}, nil
	}

	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read skills directory: %w", err)
	}

	found := make([]Skill, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillPath := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
		if _, err := os.Stat(skillPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return Snapshot{}, fmt.Errorf("stat skill file: %w", err)
		}

		skill, err := loadSkillFile(skillPath, entry.Name())
		if err != nil {
			return Snapshot{}, err
		}
		found = append(found, skill)
	}

	eligible := filterEligibleSkills(found, enabled, disabled)
	sort.Slice(eligible, func(i, j int) bool {
		return eligible[i].Name < eligible[j].Name
	})
	return Snapshot{Skills: eligible}, nil
}

func loadSkillFile(path string, fallbackName string) (Skill, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, fmt.Errorf("read skill file %s: %w", path, err)
	}
	frontmatter, body := parseFrontmatter(string(payload))

	name := strings.TrimSpace(frontmatter["name"])
	if name == "" {
		name = strings.TrimSpace(fallbackName)
	}
	description := strings.TrimSpace(frontmatter["description"])
	if description == "" {
		description = inferDescription(body)
	}
	userInvocable := true
	if raw := strings.TrimSpace(frontmatter["user-invocable"]); raw != "" {
		if parsed, err := strconv.ParseBool(raw); err == nil {
			userInvocable = parsed
		}
	}
	disableModelInvocation := false
	if raw := strings.TrimSpace(frontmatter["disable-model-invocation"]); raw != "" {
		if parsed, err := strconv.ParseBool(raw); err == nil {
			disableModelInvocation = parsed
		}
	}

	return Skill{
		Name:                   name,
		Description:            description,
		Path:                   path,
		UserInvocable:          userInvocable,
		DisableModelInvocation: disableModelInvocation,
	}, nil
}

func parseFrontmatter(content string) (map[string]string, string) {
	out := map[string]string{}
	if !strings.HasPrefix(content, "---\n") {
		return out, content
	}

	rest := strings.TrimPrefix(content, "---\n")
	idx := strings.Index(rest, "\n---\n")
	if idx < 0 {
		return out, content
	}

	front := rest[:idx]
	body := rest[idx+len("\n---\n"):]
	for _, line := range strings.Split(front, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.TrimSpace(parts[1])
		value = strings.Trim(value, `"'`)
		if key == "" {
			continue
		}
		out[key] = value
	}
	return out, body
}

func inferDescription(body string) string {
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		return truncate(trimmed, 180)
	}
	return "No description provided."
}

func filterEligibleSkills(all []Skill, enabled []string, disabled []string) []Skill {
	enabledSet := normalizeNameSet(enabled)
	disabledSet := normalizeNameSet(disabled)

	filtered := make([]Skill, 0, len(all))
	for _, skill := range all {
		name := normalizeSkillName(skill.Name)
		if name == "" {
			continue
		}
		if _, blocked := disabledSet[name]; blocked {
			continue
		}
		if len(enabledSet) > 0 {
			if _, allowed := enabledSet[name]; !allowed {
				continue
			}
		}
		filtered = append(filtered, skill)
	}
	return filtered
}

func normalizeNameSet(values []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, raw := range values {
		name := normalizeSkillName(raw)
		if name == "" {
			continue
		}
		out[name] = struct{}{}
	}
	return out
}

func normalizeSkillName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func truncate(value string, max int) string {
	trimmed := strings.TrimSpace(value)
	if max <= 0 || len(trimmed) <= max {
		return trimmed
	}
	if max <= 3 {
		return trimmed[:max]
	}
	return trimmed[:max-3] + "..."
}
