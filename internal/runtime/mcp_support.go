package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dgriffin831/localclaw/internal/memory"
)

var ErrMCPNotFound = errors.New("not found")

var workspaceReadAllowlist = map[string]struct{}{
	"AGENTS.md":    {},
	"SOUL.md":      {},
	"TOOLS.md":     {},
	"IDENTITY.md":  {},
	"USER.md":      {},
	"SECURITY.md":  {},
	"HEARTBEAT.md": {},
	"WELCOME.md":   {},
	"BOOTSTRAP.md": {},
	"MEMORY.md":    {},
	"memory.md":    {},
}

type MCPWorkspaceStatus struct {
	AgentID       string `json:"agentId"`
	WorkspacePath string `json:"workspacePath"`
	Exists        bool   `json:"exists"`
}

type MCPWorkspaceFile struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

type MCPWorkspaceReadResult struct {
	Path      string `json:"path"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	Content   string `json:"content"`
}

type MCPMemoryCreateRequest struct {
	AgentID string
	Title   string
	Content string
	Tags    []string
}

type MCPMemoryCreateResult struct {
	Path      string   `json:"path"`
	Title     string   `json:"title"`
	Tags      []string `json:"tags,omitempty"`
	CreatedAt string   `json:"createdAt"`
	Indexed   bool     `json:"indexed"`
	SyncError string   `json:"syncError,omitempty"`
}

func (a *App) MCPMemoryCreate(ctx context.Context, req MCPMemoryCreateRequest) (MCPMemoryCreateResult, error) {
	resolvedAgentID := ResolveAgentID(req.AgentID)
	if enabled, reason := a.memoryToolEnabled(resolvedAgentID, MemoryToolCreate); !enabled {
		return MCPMemoryCreateResult{}, errors.New(reason)
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return MCPMemoryCreateResult{}, errors.New("title is required")
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		return MCPMemoryCreateResult{}, errors.New("content is required")
	}

	memoryCfg := ResolveMemoryConfig(a.cfg, resolvedAgentID)
	manager, cleanup, err := a.newMemoryToolManager(ctx, resolvedAgentID, memoryCfg)
	if err != nil {
		return MCPMemoryCreateResult{}, err
	}
	defer cleanup()

	createdAt := time.Now().UTC().Format(time.RFC3339)
	tags := normalizeTags(req.Tags)
	path, err := a.uniqueMemoryPath(resolvedAgentID, title)
	if err != nil {
		return MCPMemoryCreateResult{}, err
	}
	body := renderMemoryMarkdown(title, content, tags, createdAt)
	writeResult, err := manager.Write(ctx, body, memory.WriteOptions{Path: path, Overwrite: true})
	if err != nil {
		return MCPMemoryCreateResult{}, err
	}
	return MCPMemoryCreateResult{
		Path:      writeResult.Path,
		Title:     title,
		Tags:      tags,
		CreatedAt: createdAt,
		Indexed:   writeResult.Indexed,
		SyncError: writeResult.SyncError,
	}, nil
}

func (a *App) MCPMemorySearch(ctx context.Context, agentID, query string, opts memory.SearchOptions) ([]memory.SearchResult, error) {
	resolvedAgentID := ResolveAgentID(agentID)
	if enabled, reason := a.memoryToolEnabled(resolvedAgentID, MemoryToolSearch); !enabled {
		return nil, errors.New(reason)
	}
	memoryCfg := ResolveMemoryConfig(a.cfg, resolvedAgentID)
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

func (a *App) MCPMemoryGet(ctx context.Context, agentID, path string, opts memory.GetOptions) (memory.GetResult, error) {
	resolvedAgentID := ResolveAgentID(agentID)
	if enabled, reason := a.memoryToolEnabled(resolvedAgentID, MemoryToolGet); !enabled {
		return memory.GetResult{}, errors.New(reason)
	}
	memoryCfg := ResolveMemoryConfig(a.cfg, resolvedAgentID)

	manager, cleanup, err := a.newMemoryToolManager(ctx, resolvedAgentID, memoryCfg)
	if err != nil {
		return memory.GetResult{}, err
	}
	defer cleanup()

	return manager.Get(ctx, path, opts)
}

func (a *App) MCPMemoryGrep(ctx context.Context, agentID, query string, opts memory.GrepOptions) (memory.GrepResult, error) {
	resolvedAgentID := ResolveAgentID(agentID)
	if enabled, reason := a.memoryToolEnabled(resolvedAgentID, MemoryToolGrep); !enabled {
		return memory.GrepResult{}, errors.New(reason)
	}
	memoryCfg := ResolveMemoryConfig(a.cfg, resolvedAgentID)

	manager, cleanup, err := a.newMemoryToolManager(ctx, resolvedAgentID, memoryCfg)
	if err != nil {
		return memory.GrepResult{}, err
	}
	defer cleanup()

	return manager.Grep(ctx, query, opts)
}

func (a *App) MCPWorkspaceStatus(ctx context.Context, agentID string) (MCPWorkspaceStatus, error) {
	_ = ctx
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

func (a *App) MCPWorkspaceList(ctx context.Context, agentID string) ([]MCPWorkspaceFile, error) {
	_ = ctx
	workspacePath, err := a.ResolveWorkspacePath(agentID)
	if err != nil {
		return nil, err
	}
	files := []MCPWorkspaceFile{}
	for allowed := range workspaceReadAllowlist {
		path := filepath.Join(workspacePath, allowed)
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			files = append(files, MCPWorkspaceFile{Path: filepath.ToSlash(allowed), Bytes: info.Size()})
			continue
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("stat workspace file %s: %w", allowed, err)
		}
	}
	memoryRoot := filepath.Join(workspacePath, "memory")
	if err := filepath.WalkDir(memoryRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(entry.Name())) != ".md" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(workspacePath, path)
		if err != nil {
			return err
		}
		files = append(files, MCPWorkspaceFile{Path: filepath.ToSlash(rel), Bytes: info.Size()})
		return nil
	}); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("walk workspace memory files: %w", err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func (a *App) MCPWorkspaceRead(ctx context.Context, agentID, path string, opts memory.GetOptions) (MCPWorkspaceReadResult, error) {
	_ = ctx
	workspacePath, err := a.ResolveWorkspacePath(agentID)
	if err != nil {
		return MCPWorkspaceReadResult{}, err
	}
	rel, abs, err := resolveWorkspaceReadPath(workspacePath, path)
	if err != nil {
		return MCPWorkspaceReadResult{}, err
	}
	result, err := memory.ReadMarkdownFile(abs, rel, "workspace", opts)
	if err != nil {
		return MCPWorkspaceReadResult{}, err
	}
	return MCPWorkspaceReadResult{
		Path:      result.Path,
		StartLine: result.StartLine,
		EndLine:   result.EndLine,
		Content:   result.Content,
	}, nil
}

func (a *App) uniqueMemoryPath(agentID, title string) (string, error) {
	workspacePath, err := a.ResolveWorkspacePath(agentID)
	if err != nil {
		return "", err
	}
	slug := slugify(title)
	if slug == "" {
		slug = "memory"
	}
	date := time.Now().UTC().Format("2006-01-02")
	for i := 0; i < 1000; i++ {
		name := fmt.Sprintf("%s-%s.md", date, slug)
		if i > 0 {
			name = fmt.Sprintf("%s-%s-%d.md", date, slug, i+1)
		}
		rel := filepath.ToSlash(filepath.Join("memory", name))
		if _, err := os.Stat(filepath.Join(workspacePath, filepath.FromSlash(rel))); errors.Is(err, os.ErrNotExist) {
			return rel, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", errors.New("unable to allocate unique memory path")
}

func resolveWorkspaceReadPath(workspacePath, rawPath string) (string, string, error) {
	rel, err := memory.NormalizeRelativePath(rawPath)
	if err != nil {
		return "", "", memory.ErrMemoryPathOutOfScope
	}
	if !workspaceReadPathAllowed(rel) {
		return "", "", memory.ErrMemoryPathOutOfScope
	}
	root, err := filepath.Abs(filepath.Clean(workspacePath))
	if err != nil {
		return "", "", err
	}
	abs := filepath.Join(root, filepath.FromSlash(rel))
	safeRel, err := memory.SafeRelativePath(root, abs)
	if err != nil {
		return "", "", memory.ErrMemoryPathOutOfScope
	}
	if !workspaceReadPathAllowed(safeRel) {
		return "", "", memory.ErrMemoryPathOutOfScope
	}
	return safeRel, abs, nil
}

func workspaceReadPathAllowed(rel string) bool {
	normalized := filepath.ToSlash(strings.TrimSpace(rel))
	if _, ok := workspaceReadAllowlist[normalized]; ok {
		return true
	}
	lower := strings.ToLower(normalized)
	return strings.HasPrefix(lower, "memory/") && strings.HasSuffix(lower, ".md")
}

func renderMemoryMarkdown(title, content string, tags []string, createdAt string) string {
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(strings.TrimSpace(title))
	b.WriteString("\n\n")
	b.WriteString("Created: ")
	b.WriteString(createdAt)
	b.WriteString("\n")
	if len(tags) > 0 {
		b.WriteString("Tags: ")
		b.WriteString(strings.Join(tags, ", "))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(strings.TrimSpace(content))
	b.WriteString("\n")
	return b.String()
}

func normalizeTags(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, raw := range values {
		tag := strings.ToLower(strings.TrimSpace(raw))
		tag = strings.Trim(tag, "#,")
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}

func slugify(value string) string {
	lower := strings.ToLower(strings.TrimSpace(value))
	re := regexp.MustCompile(`[^a-z0-9]+`)
	slug := re.ReplaceAllString(lower, "-")
	return strings.Trim(slug, "-")
}
