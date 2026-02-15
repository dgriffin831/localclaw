package skills

import (
	"context"
	"sort"
	"strings"
	"sync"
)

// Registry tracks locally available skills.
type Registry interface {
	Load(ctx context.Context) error
	List(ctx context.Context) ([]string, error)
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

// ToolRegistry tracks callable local runtime tools.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]ToolDefinition
}

type LocalRegistry struct{}

func NewLocalRegistry() *LocalRegistry {
	return &LocalRegistry{}
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
	return nil
}

func (r *LocalRegistry) List(ctx context.Context) ([]string, error) {
	return []string{}, nil
}
