package workspace

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Manager controls local workspace operations.
type Manager interface {
	Init(ctx context.Context) error
	ResolveWorkspace(agentID string) (string, error)
	EnsureWorkspace(ctx context.Context, agentID string, ensureBootstrap bool) (WorkspaceInfo, error)
	LoadBootstrapFiles(ctx context.Context, agentID, sessionKey string) ([]BootstrapFile, error)
	Root() string
}

type Settings struct {
	StateRoot        string
	DefaultWorkspace string
	AgentWorkspaces  map[string]string
}

type WorkspaceInfo struct {
	AgentID          string
	Path             string
	Created          bool
	BootstrapCreated []string
}

type BootstrapFile struct {
	Name    string
	Path    string
	Content string
	Missing bool
}

type LocalManager struct {
	settings Settings
	root     string
}

//go:embed templates/*.md
var workspaceTemplates embed.FS

var bootstrapTemplateOrder = []string{
	"AGENTS.md",
	"SOUL.md",
	"TOOLS.md",
	"IDENTITY.md",
	"USER.md",
	"HEARTBEAT.md",
}

var subagentBootstrapAllowlist = map[string]struct{}{
	"AGENTS.md":    {},
	"TOOLS.md":     {},
	"IDENTITY.md":  {},
	"BOOTSTRAP.md": {},
	"MEMORY.md":    {},
	"memory.md":    {},
}

func NewLocalManager(settings Settings) *LocalManager {
	normalizedAgentWorkspaces := make(map[string]string, len(settings.AgentWorkspaces))
	for agentID, workspacePath := range settings.AgentWorkspaces {
		normalizedID := normalizeAgentID(agentID)
		normalizedAgentWorkspaces[normalizedID] = workspacePath
	}
	if normalizedAgentWorkspaces == nil {
		normalizedAgentWorkspaces = map[string]string{}
	}
	settings.AgentWorkspaces = normalizedAgentWorkspaces
	return &LocalManager{
		root:     settings.DefaultWorkspace,
		settings: settings,
	}
}

func (m *LocalManager) Init(ctx context.Context) error {
	if _, err := m.EnsureWorkspace(ctx, "", true); err != nil {
		return err
	}
	for agentID := range m.settings.AgentWorkspaces {
		if _, err := m.EnsureWorkspace(ctx, agentID, true); err != nil {
			return err
		}
	}
	return nil
}

func (m *LocalManager) ResolveWorkspace(agentID string) (string, error) {
	normalizedAgentID := normalizeAgentID(agentID)
	configured := strings.TrimSpace(m.settings.DefaultWorkspace)
	if agentConfigured, ok := m.settings.AgentWorkspaces[normalizedAgentID]; ok && strings.TrimSpace(agentConfigured) != "" {
		configured = strings.TrimSpace(agentConfigured)
	}
	if configured == "" {
		return "", errors.New("workspace path is not configured")
	}

	stateRoot, err := expandAndCleanPath(strings.TrimSpace(m.settings.StateRoot))
	if err != nil {
		return "", fmt.Errorf("resolve state root: %w", err)
	}

	if configured == "." {
		if normalizedAgentID == "default" {
			return filepath.Join(stateRoot, "workspace"), nil
		}
		return filepath.Join(stateRoot, "workspace-"+sanitizePathToken(normalizedAgentID)), nil
	}

	resolved := strings.ReplaceAll(configured, "{agentId}", normalizedAgentID)
	resolved, err = expandAndCleanPath(resolved)
	if err != nil {
		return "", fmt.Errorf("resolve workspace: %w", err)
	}
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(stateRoot, resolved)
	}
	return filepath.Clean(resolved), nil
}

func (m *LocalManager) EnsureWorkspace(ctx context.Context, agentID string, ensureBootstrap bool) (WorkspaceInfo, error) {
	workspacePath, err := m.ResolveWorkspace(agentID)
	if err != nil {
		return WorkspaceInfo{}, err
	}

	info := WorkspaceInfo{
		AgentID: normalizeAgentID(agentID),
		Path:    workspacePath,
	}

	stat, err := os.Stat(workspacePath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return WorkspaceInfo{}, fmt.Errorf("stat workspace: %w", err)
		}
		info.Created = true
		if err := os.MkdirAll(workspacePath, 0o755); err != nil {
			return WorkspaceInfo{}, fmt.Errorf("mkdir workspace: %w", err)
		}
	} else if !stat.IsDir() {
		return WorkspaceInfo{}, fmt.Errorf("workspace path %q is not a directory", workspacePath)
	}

	if !ensureBootstrap {
		return info, nil
	}

	files := append([]string{}, bootstrapTemplateOrder...)
	if info.Created {
		files = append(files, "BOOTSTRAP.md")
	}
	for _, fileName := range files {
		targetPath := filepath.Join(workspacePath, fileName)
		if _, err := os.Stat(targetPath); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return WorkspaceInfo{}, fmt.Errorf("stat bootstrap file %s: %w", fileName, err)
		}

		templateBytes, err := workspaceTemplates.ReadFile(filepath.Join("templates", fileName))
		if err != nil {
			return WorkspaceInfo{}, fmt.Errorf("read template %s: %w", fileName, err)
		}
		content := stripFrontmatter(string(templateBytes))
		if err := os.WriteFile(targetPath, []byte(content), 0o644); err != nil {
			return WorkspaceInfo{}, fmt.Errorf("write bootstrap file %s: %w", fileName, err)
		}
		info.BootstrapCreated = append(info.BootstrapCreated, fileName)
	}

	return info, nil
}

func (m *LocalManager) LoadBootstrapFiles(ctx context.Context, agentID, sessionKey string) ([]BootstrapFile, error) {
	workspacePath, err := m.ResolveWorkspace(agentID)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(workspacePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat workspace: %w", err)
	}

	names := append([]string{}, bootstrapTemplateOrder...)
	names = append(names, "BOOTSTRAP.md")
	if isSubagentSession(sessionKey) {
		filtered := make([]string, 0, len(names))
		for _, name := range names {
			if _, ok := subagentBootstrapAllowlist[name]; ok {
				filtered = append(filtered, name)
			}
		}
		names = filtered
	}

	files := make([]BootstrapFile, 0, len(names)+2)
	for _, name := range names {
		file, err := loadBootstrapFile(filepath.Join(workspacePath, name), name)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}

	for _, name := range []string{"MEMORY.md", "memory.md"} {
		path := filepath.Join(workspacePath, name)
		if _, err := os.Stat(path); err == nil {
			file, err := loadBootstrapFile(path, name)
			if err != nil {
				return nil, err
			}
			files = append(files, file)
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("stat bootstrap file %s: %w", name, err)
		}
	}

	return files, nil
}

func (m *LocalManager) Root() string {
	root, err := m.ResolveWorkspace("")
	if err != nil {
		return m.root
	}
	return root
}

func loadBootstrapFile(path string, name string) (BootstrapFile, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return BootstrapFile{Name: name, Path: path, Missing: true}, nil
		}
		return BootstrapFile{}, fmt.Errorf("read bootstrap file %s: %w", name, err)
	}
	return BootstrapFile{Name: name, Path: path, Content: string(content)}, nil
}

func normalizeAgentID(agentID string) string {
	trimmed := strings.TrimSpace(agentID)
	if trimmed == "" {
		return "default"
	}
	return trimmed
}

func sanitizePathToken(value string) string {
	replacer := strings.NewReplacer("/", "-", "\\", "-", " ", "-")
	return replacer.Replace(value)
}

func isSubagentSession(sessionKey string) bool {
	key := strings.ToLower(strings.TrimSpace(sessionKey))
	return strings.HasPrefix(key, "subagent:") || strings.Contains(key, "/subagent/") || strings.Contains(key, "-subagent-")
}

func expandAndCleanPath(path string) (string, error) {
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

func stripFrontmatter(content string) string {
	if !strings.HasPrefix(content, "---\n") {
		return content
	}

	rest := strings.TrimPrefix(content, "---\n")
	idx := strings.Index(rest, "\n---\n")
	if idx < 0 {
		return content
	}
	return rest[idx+len("\n---\n"):]
}
