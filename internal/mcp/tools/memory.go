package tools

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/dgriffin831/localclaw/internal/mcp/protocol"
	"github.com/dgriffin831/localclaw/internal/memory"
	"github.com/dgriffin831/localclaw/internal/runtime"
)

const (
	ToolLocalclawMemorySearch = "localclaw_memory_search"
	ToolLocalclawMemoryGet    = "localclaw_memory_get"
	ToolLocalclawMemoryGrep   = "localclaw_memory_grep"
)

type MemorySearchRequest struct {
	AgentID    string
	SessionID  string
	SessionKey string
	Query      string
	MaxResults int
	MinScore   float64
}

type MemoryGetRequest struct {
	AgentID   string
	SessionID string
	Path      string
	FromLine  int
	Lines     int
}

type MemoryGrepRequest struct {
	AgentID       string
	SessionID     string
	Query         string
	Mode          string
	CaseSensitive bool
	Word          bool
	MaxMatches    int
	ContextLines  int
	PathGlob      []string
	Source        string
}

type MemoryBackend interface {
	Search(ctx context.Context, req MemorySearchRequest) ([]memory.SearchResult, error)
	Get(ctx context.Context, req MemoryGetRequest) (memory.GetResult, error)
	Grep(ctx context.Context, req MemoryGrepRequest) (memory.GrepResult, error)
}

type MemorySearchTool struct {
	backend MemoryBackend
}

type MemoryGetTool struct {
	backend MemoryBackend
}

type MemoryGrepTool struct {
	backend MemoryBackend
}

func NewMemorySearchTool(backend MemoryBackend) MemorySearchTool {
	return MemorySearchTool{backend: backend}
}

func NewMemoryGetTool(backend MemoryBackend) MemoryGetTool {
	return MemoryGetTool{backend: backend}
}

func NewMemoryGrepTool(backend MemoryBackend) MemoryGrepTool {
	return MemoryGrepTool{backend: backend}
}

func MemorySearchDefinition() protocol.Tool {
	return memorySearchDefinition(ToolLocalclawMemorySearch)
}

func memorySearchDefinition(name string) protocol.Tool {
	return protocol.Tool{
		Name:        name,
		Description: "Search indexed localclaw memory chunks using keyword ranking (FTS/LIKE fallback)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query":       map[string]interface{}{"type": "string"},
				"max_results": map[string]interface{}{"type": "integer"},
				"min_score":   map[string]interface{}{"type": "number"},
				"agent_id":    map[string]interface{}{"type": "string"},
				"session_id":  map[string]interface{}{"type": "string"},
				"session_key": map[string]interface{}{"type": "string"},
			},
			"required": []string{"query"},
		},
	}
}

func MemoryGetDefinition() protocol.Tool {
	return memoryGetDefinition(ToolLocalclawMemoryGet)
}

func memoryGetDefinition(name string) protocol.Tool {
	return protocol.Tool{
		Name:        name,
		Description: "Read a markdown memory file slice from the local index scope",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":       map[string]interface{}{"type": "string"},
				"from_line":  map[string]interface{}{"type": "integer"},
				"lines":      map[string]interface{}{"type": "integer"},
				"agent_id":   map[string]interface{}{"type": "string"},
				"session_id": map[string]interface{}{"type": "string"},
			},
			"required": []string{"path"},
		},
	}
}

func MemoryGrepDefinition() protocol.Tool {
	return memoryGrepDefinition(ToolLocalclawMemoryGrep)
}

func memoryGrepDefinition(name string) protocol.Tool {
	return protocol.Tool{
		Name:        name,
		Description: "Find exact literals or regex matches across allowed memory/session files",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query":          map[string]interface{}{"type": "string"},
				"mode":           map[string]interface{}{"type": "string", "enum": []string{"literal", "regex"}},
				"case_sensitive": map[string]interface{}{"type": "boolean"},
				"word":           map[string]interface{}{"type": "boolean"},
				"max_matches":    map[string]interface{}{"type": "integer"},
				"context_lines":  map[string]interface{}{"type": "integer"},
				"path_glob": map[string]interface{}{
					"anyOf": []map[string]interface{}{
						{"type": "string"},
						{"type": "array", "items": map[string]interface{}{"type": "string"}},
					},
				},
				"source":     map[string]interface{}{"type": "string", "enum": []string{"memory", "sessions", "all"}},
				"agent_id":   map[string]interface{}{"type": "string"},
				"session_id": map[string]interface{}{"type": "string"},
			},
			"required": []string{"query"},
		},
	}
}

func (t MemorySearchTool) Call(ctx context.Context, args map[string]interface{}) protocol.CallToolResult {
	query, err := requiredStringArg(args, "query")
	if err != nil {
		return errorResult(err)
	}
	maxResults, err := optionalIntArg(args, "max_results")
	if err != nil {
		return errorResult(err)
	}
	minScore, err := optionalFloatArg(args, "min_score")
	if err != nil {
		return errorResult(err)
	}
	agentID, err := optionalStringArg(args, "agent_id")
	if err != nil {
		return errorResult(err)
	}
	sessionID, err := optionalStringArg(args, "session_id")
	if err != nil {
		return errorResult(err)
	}
	sessionKey, err := optionalStringArg(args, "session_key")
	if err != nil {
		return errorResult(err)
	}

	results, runErr := t.backend.Search(ctx, MemorySearchRequest{
		AgentID:    agentID,
		SessionID:  sessionID,
		SessionKey: sessionKey,
		Query:      query,
		MaxResults: maxResults,
		MinScore:   minScore,
	})
	if runErr != nil {
		return errorResult(fmt.Errorf("memory_search failed: %w", runErr))
	}
	return protocol.CallToolResult{
		StructuredContent: map[string]interface{}{
			"ok":      true,
			"count":   len(results),
			"results": results,
		},
	}
}

func (t MemoryGetTool) Call(ctx context.Context, args map[string]interface{}) protocol.CallToolResult {
	path, err := requiredStringArg(args, "path")
	if err != nil {
		return errorResult(err)
	}
	fromLine, err := optionalIntArg(args, "from_line")
	if err != nil {
		return errorResult(err)
	}
	lines, err := optionalIntArg(args, "lines")
	if err != nil {
		return errorResult(err)
	}
	agentID, err := optionalStringArg(args, "agent_id")
	if err != nil {
		return errorResult(err)
	}
	sessionID, err := optionalStringArg(args, "session_id")
	if err != nil {
		return errorResult(err)
	}

	result, runErr := t.backend.Get(ctx, MemoryGetRequest{
		AgentID:   agentID,
		SessionID: sessionID,
		Path:      path,
		FromLine:  fromLine,
		Lines:     lines,
	})
	if runErr != nil {
		return errorResult(fmt.Errorf("memory_get failed: %w", runErr))
	}
	return protocol.CallToolResult{
		StructuredContent: map[string]interface{}{
			"ok":     true,
			"result": result,
		},
	}
}

func (t MemoryGrepTool) Call(ctx context.Context, args map[string]interface{}) protocol.CallToolResult {
	query, err := requiredStringArg(args, "query")
	if err != nil {
		return errorResult(err)
	}
	mode, err := optionalStringArg(args, "mode")
	if err != nil {
		return errorResult(err)
	}
	caseSensitive, err := optionalBoolArg(args, "case_sensitive")
	if err != nil {
		return errorResult(err)
	}
	word, err := optionalBoolArg(args, "word")
	if err != nil {
		return errorResult(err)
	}
	maxMatches, err := optionalIntArg(args, "max_matches")
	if err != nil {
		return errorResult(err)
	}
	contextLines, err := optionalIntArg(args, "context_lines")
	if err != nil {
		return errorResult(err)
	}
	pathGlob, err := optionalStringListArg(args, "path_glob")
	if err != nil {
		return errorResult(err)
	}
	source, err := optionalStringArg(args, "source")
	if err != nil {
		return errorResult(err)
	}
	agentID, err := optionalStringArg(args, "agent_id")
	if err != nil {
		return errorResult(err)
	}
	sessionID, err := optionalStringArg(args, "session_id")
	if err != nil {
		return errorResult(err)
	}

	result, runErr := t.backend.Grep(ctx, MemoryGrepRequest{
		AgentID:       agentID,
		SessionID:     sessionID,
		Query:         query,
		Mode:          mode,
		CaseSensitive: caseSensitive,
		Word:          word,
		MaxMatches:    maxMatches,
		ContextLines:  contextLines,
		PathGlob:      pathGlob,
		Source:        source,
	})
	if runErr != nil {
		return errorResult(fmt.Errorf("memory_grep failed: %w", runErr))
	}
	return protocol.CallToolResult{
		StructuredContent: map[string]interface{}{
			"ok":      true,
			"count":   result.Count,
			"matches": result.Matches,
		},
	}
}

func errorResult(err error) protocol.CallToolResult {
	return protocol.CallToolResult{
		IsError: true,
		StructuredContent: map[string]interface{}{
			"ok":    false,
			"error": err.Error(),
		},
	}
}

func requiredStringArg(args map[string]interface{}, name string) (string, error) {
	value, ok := args[name]
	if !ok {
		return "", fmt.Errorf("%s is required", name)
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", name)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("%s cannot be blank", name)
	}
	return text, nil
}

func optionalStringArg(args map[string]interface{}, name string) (string, error) {
	value, ok := args[name]
	if !ok {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", name)
	}
	return strings.TrimSpace(text), nil
}

func optionalIntArg(args map[string]interface{}, name string) (int, error) {
	value, ok := args[name]
	if !ok {
		return 0, nil
	}
	switch typed := value.(type) {
	case int:
		return typed, nil
	case int32:
		return int(typed), nil
	case int64:
		return int(typed), nil
	case float64:
		if math.Mod(typed, 1) != 0 {
			return 0, fmt.Errorf("%s must be an integer", name)
		}
		return int(typed), nil
	default:
		return 0, fmt.Errorf("%s must be an integer", name)
	}
}

func optionalFloatArg(args map[string]interface{}, name string) (float64, error) {
	value, ok := args[name]
	if !ok {
		return 0, nil
	}
	switch typed := value.(type) {
	case float64:
		return typed, nil
	case float32:
		return float64(typed), nil
	case int:
		return float64(typed), nil
	default:
		return 0, fmt.Errorf("%s must be a number", name)
	}
}

func optionalBoolArg(args map[string]interface{}, name string) (bool, error) {
	value, ok := args[name]
	if !ok {
		return false, nil
	}
	typed, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("%s must be a boolean", name)
	}
	return typed, nil
}

func optionalStringListArg(args map[string]interface{}, name string) ([]string, error) {
	value, ok := args[name]
	if !ok {
		return nil, nil
	}
	appendText := func(out []string, text string) []string {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			return out
		}
		return append(out, trimmed)
	}

	switch typed := value.(type) {
	case string:
		return appendText(nil, typed), nil
	case []string:
		out := make([]string, 0, len(typed))
		for _, entry := range typed {
			out = appendText(out, entry)
		}
		return out, nil
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, entry := range typed {
			text, ok := entry.(string)
			if !ok {
				return nil, fmt.Errorf("%s must be a string or string array", name)
			}
			out = appendText(out, text)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%s must be a string or string array", name)
	}
}

type RuntimeMemoryBackend struct {
	App *runtime.App
}

func (b RuntimeMemoryBackend) Search(ctx context.Context, req MemorySearchRequest) ([]memory.SearchResult, error) {
	return b.App.MCPMemorySearch(ctx, req.AgentID, req.SessionID, req.Query, memory.SearchOptions{
		MaxResults: req.MaxResults,
		MinScore:   req.MinScore,
		SessionKey: req.SessionKey,
	})
}

func (b RuntimeMemoryBackend) Get(ctx context.Context, req MemoryGetRequest) (memory.GetResult, error) {
	return b.App.MCPMemoryGet(ctx, req.AgentID, req.SessionID, req.Path, memory.GetOptions{FromLine: req.FromLine, Lines: req.Lines})
}

func (b RuntimeMemoryBackend) Grep(ctx context.Context, req MemoryGrepRequest) (memory.GrepResult, error) {
	return b.App.MCPMemoryGrep(ctx, req.AgentID, req.SessionID, req.Query, memory.GrepOptions{
		Mode:          req.Mode,
		CaseSensitive: req.CaseSensitive,
		Word:          req.Word,
		MaxMatches:    req.MaxMatches,
		ContextLines:  req.ContextLines,
		PathGlob:      req.PathGlob,
		Source:        req.Source,
	})
}
