package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveWorkspaceUsesDefaultsAndPerAgentOverrides(t *testing.T) {
	t.Parallel()

	stateRoot := t.TempDir()
	absoluteOverride := filepath.Join(t.TempDir(), "absolute-agent")

	mgr := NewLocalManager(Settings{
		StateRoot:        stateRoot,
		DefaultWorkspace: ".",
		AgentWorkspaces: map[string]string{
			"alpha": ".",
			"beta":  "custom/{agentId}",
			"gamma": absoluteOverride,
		},
	})

	defaultPath, err := mgr.ResolveWorkspace("")
	if err != nil {
		t.Fatalf("resolve default workspace: %v", err)
	}
	if defaultPath != filepath.Join(stateRoot, "workspace") {
		t.Fatalf("unexpected default workspace path %q", defaultPath)
	}

	alphaPath, err := mgr.ResolveWorkspace("alpha")
	if err != nil {
		t.Fatalf("resolve alpha workspace: %v", err)
	}
	if alphaPath != filepath.Join(stateRoot, "workspace-alpha") {
		t.Fatalf("unexpected alpha workspace path %q", alphaPath)
	}

	betaPath, err := mgr.ResolveWorkspace("beta")
	if err != nil {
		t.Fatalf("resolve beta workspace: %v", err)
	}
	if betaPath != filepath.Join(stateRoot, "custom", "beta") {
		t.Fatalf("unexpected beta workspace path %q", betaPath)
	}

	gammaPath, err := mgr.ResolveWorkspace("gamma")
	if err != nil {
		t.Fatalf("resolve gamma workspace: %v", err)
	}
	if gammaPath != absoluteOverride {
		t.Fatalf("unexpected gamma workspace path %q", gammaPath)
	}
}

func TestResolveWorkspaceAppliesAgentOverrideForTrimmedAgentID(t *testing.T) {
	t.Parallel()

	stateRoot := t.TempDir()
	mgr := NewLocalManager(Settings{
		StateRoot:        stateRoot,
		DefaultWorkspace: ".",
		AgentWorkspaces: map[string]string{
			"alpha": "custom/{agentId}",
		},
	})

	path, err := mgr.ResolveWorkspace(" alpha ")
	if err != nil {
		t.Fatalf("resolve trimmed agent workspace: %v", err)
	}
	if path != filepath.Join(stateRoot, "custom", "alpha") {
		t.Fatalf("unexpected trimmed agent workspace path %q", path)
	}
}

func TestEnsureWorkspaceCreatesWorkspaceAndBootstrapFiles(t *testing.T) {
	t.Parallel()

	stateRoot := t.TempDir()
	mgr := NewLocalManager(Settings{
		StateRoot:        stateRoot,
		DefaultWorkspace: ".",
	})

	info, err := mgr.EnsureWorkspace(context.Background(), "", true)
	if err != nil {
		t.Fatalf("ensure workspace: %v", err)
	}
	if !info.Created {
		t.Fatalf("expected workspace to be newly created")
	}

	workspacePath := filepath.Join(stateRoot, "workspace")
	if info.Path != workspacePath {
		t.Fatalf("unexpected workspace path %q", info.Path)
	}
	if _, err := os.Stat(workspacePath); err != nil {
		t.Fatalf("workspace path should exist: %v", err)
	}

	for _, name := range []string{
		"AGENTS.md",
		"SOUL.md",
		"TOOLS.md",
		"IDENTITY.md",
		"USER.md",
		"HEARTBEAT.md",
		"WELCOME.md",
		"BOOTSTRAP.md",
	} {
		path := filepath.Join(workspacePath, name)
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if strings.HasPrefix(string(content), "---\n") {
			t.Fatalf("expected frontmatter to be stripped in %s", name)
		}
	}
}

func TestEnsureWorkspaceDoesNotOverwriteExistingBootstrapFiles(t *testing.T) {
	t.Parallel()

	stateRoot := t.TempDir()
	workspacePath := filepath.Join(stateRoot, "workspace")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	existing := filepath.Join(workspacePath, "AGENTS.md")
	if err := os.WriteFile(existing, []byte("custom content\n"), 0o644); err != nil {
		t.Fatalf("write existing AGENTS.md: %v", err)
	}

	mgr := NewLocalManager(Settings{
		StateRoot:        stateRoot,
		DefaultWorkspace: ".",
	})

	info, err := mgr.EnsureWorkspace(context.Background(), "", true)
	if err != nil {
		t.Fatalf("ensure workspace: %v", err)
	}
	if info.Created {
		t.Fatalf("workspace should not be marked created when pre-existing")
	}

	got, err := os.ReadFile(existing)
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	if string(got) != "custom content\n" {
		t.Fatalf("AGENTS.md should not be overwritten, got %q", string(got))
	}
}

func TestLoadBootstrapFilesReturnsStructuredListWithMissingFlags(t *testing.T) {
	t.Parallel()

	stateRoot := t.TempDir()
	mgr := NewLocalManager(Settings{
		StateRoot:        stateRoot,
		DefaultWorkspace: ".",
	})

	info, err := mgr.EnsureWorkspace(context.Background(), "", false)
	if err != nil {
		t.Fatalf("ensure workspace: %v", err)
	}

	if err := os.WriteFile(filepath.Join(info.Path, "AGENTS.md"), []byte("agents\n"), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(info.Path, "MEMORY.md"), []byte("memory\n"), 0o644); err != nil {
		t.Fatalf("write MEMORY.md: %v", err)
	}

	files, err := mgr.LoadBootstrapFiles(context.Background(), "", "main")
	if err != nil {
		t.Fatalf("load bootstrap files: %v", err)
	}

	byName := map[string]BootstrapFile{}
	for _, file := range files {
		byName[file.Name] = file
	}

	if byName["AGENTS.md"].Missing {
		t.Fatalf("AGENTS.md should not be marked missing")
	}
	if !byName["SOUL.md"].Missing {
		t.Fatalf("SOUL.md should be marked missing")
	}
	if byName["MEMORY.md"].Missing {
		t.Fatalf("MEMORY.md should not be marked missing")
	}
	if _, ok := byName["memory.md"]; ok {
		t.Fatalf("memory.md should not be included when missing")
	}
}

func TestLoadBootstrapFilesExcludesWelcomeFileFromPromptContext(t *testing.T) {
	t.Parallel()

	stateRoot := t.TempDir()
	mgr := NewLocalManager(Settings{
		StateRoot:        stateRoot,
		DefaultWorkspace: ".",
	})

	info, err := mgr.EnsureWorkspace(context.Background(), "", false)
	if err != nil {
		t.Fatalf("ensure workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(info.Path, "WELCOME.md"), []byte("welcome\n"), 0o644); err != nil {
		t.Fatalf("write WELCOME.md: %v", err)
	}

	files, err := mgr.LoadBootstrapFiles(context.Background(), "", "main")
	if err != nil {
		t.Fatalf("load bootstrap files: %v", err)
	}
	for _, file := range files {
		if file.Name == "WELCOME.md" {
			t.Fatalf("WELCOME.md should not be included in prompt bootstrap files")
		}
	}
}

func TestLoadBootstrapFilesAppliesSubagentAllowlist(t *testing.T) {
	t.Parallel()

	stateRoot := t.TempDir()
	mgr := NewLocalManager(Settings{
		StateRoot:        stateRoot,
		DefaultWorkspace: ".",
	})

	info, err := mgr.EnsureWorkspace(context.Background(), "", false)
	if err != nil {
		t.Fatalf("ensure workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(info.Path, "AGENTS.md"), []byte("agents\n"), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(info.Path, "TOOLS.md"), []byte("tools\n"), 0o644); err != nil {
		t.Fatalf("write TOOLS.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(info.Path, "USER.md"), []byte("user\n"), 0o644); err != nil {
		t.Fatalf("write USER.md: %v", err)
	}

	files, err := mgr.LoadBootstrapFiles(context.Background(), "", "subagent:worker-1")
	if err != nil {
		t.Fatalf("load bootstrap files: %v", err)
	}

	for _, file := range files {
		if file.Name == "USER.md" {
			t.Fatalf("USER.md should be excluded by subagent allowlist")
		}
	}
	byName := map[string]BootstrapFile{}
	for _, file := range files {
		byName[file.Name] = file
	}
	if _, ok := byName["MEMORY.md"]; ok {
		t.Fatalf("MEMORY.md should only be included for subagent sessions when present")
	}
	if _, ok := byName["memory.md"]; ok {
		t.Fatalf("memory.md should only be included for subagent sessions when present")
	}
}
