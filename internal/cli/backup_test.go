package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dgriffin831/localclaw/internal/backup"
	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/runtime"
)

func TestRunBackupCommandCreatesArchive(t *testing.T) {
	cfg := config.Default()
	cfg.App.Root = t.TempDir()
	cfg.Agents.Defaults.Workspace = "."
	cfg.Agents.List = []config.AgentConfig{{ID: "agent-ops"}}
	cfg.Cron.Enabled = false
	cfg.Heartbeat.Enabled = false

	app, err := runtime.New(cfg)
	if err != nil {
		t.Fatalf("runtime.New: %v", err)
	}

	defaultWorkspace, err := app.ResolveWorkspacePath("default")
	if err != nil {
		t.Fatalf("resolve default workspace: %v", err)
	}
	opsWorkspace, err := app.ResolveWorkspacePath("agent-ops")
	if err != nil {
		t.Fatalf("resolve agent workspace: %v", err)
	}
	if err := os.MkdirAll(defaultWorkspace, 0o755); err != nil {
		t.Fatalf("mkdir default workspace: %v", err)
	}
	if err := os.MkdirAll(opsWorkspace, 0o755); err != nil {
		t.Fatalf("mkdir ops workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(defaultWorkspace, "AGENTS.md"), []byte("default"), 0o600); err != nil {
		t.Fatalf("write default workspace file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(opsWorkspace, "AGENTS.md"), []byte("ops"), 0o600); err != nil {
		t.Fatalf("write ops workspace file: %v", err)
	}

	if err := os.WriteFile(filepath.Join(cfg.App.Root, "localclaw.json"), []byte(`{"ok":true}`), 0o600); err != nil {
		t.Fatalf("write localclaw.json: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(cfg.App.Root, "cron"), 0o755); err != nil {
		t.Fatalf("mkdir cron: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfg.App.Root, "cron", "jobs.json"), []byte(`[]`), 0o600); err != nil {
		t.Fatalf("write cron/jobs.json: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := RunBackupCommand(context.Background(), cfg, app, nil, &stdout, &stderr); err != nil {
		t.Fatalf("RunBackupCommand: %v (stderr=%q)", err, stderr.String())
	}

	line := strings.TrimSpace(stdout.String())
	if !strings.HasPrefix(line, "backup created: ") {
		t.Fatalf("expected success output with archive path, got %q", line)
	}
	archivePath := strings.TrimSpace(strings.TrimPrefix(line, "backup created: "))
	if archivePath == "" {
		t.Fatalf("expected non-empty archive path output")
	}
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("expected created archive at %s: %v", archivePath, err)
	}
}

func TestRunBackupCommandRejectsPositionalArgs(t *testing.T) {
	cfg := config.Default()
	cfg.App.Root = t.TempDir()
	app, err := runtime.New(cfg)
	if err != nil {
		t.Fatalf("runtime.New: %v", err)
	}

	err = RunBackupCommand(context.Background(), cfg, app, []string{"extra"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "does not accept positional arguments") {
		t.Fatalf("expected positional-args rejection, got %v", err)
	}
}

func TestStartBackupLoopsUsesDedicatedManagerForAutoClean(t *testing.T) {
	originalBuilder := buildBackupManager
	defer func() { buildBackupManager = originalBuilder }()

	saveManager := &stubBackupManager{stateRoot: "/tmp/state-root-save"}
	cleanManager := &stubBackupManager{stateRoot: "/tmp/state-root-clean"}
	calls := 0
	buildBackupManager = func(cfg config.Config) (backupManager, time.Duration, error) {
		_ = cfg
		calls++
		if calls == 1 {
			return saveManager, time.Second, nil
		}
		return cleanManager, time.Second, nil
	}

	cfg := config.Default()
	cfg.Backup.AutoSave = true
	cfg.Backup.AutoClean = true

	StartBackupLoops(context.Background(), cfg, &runtime.App{})

	if calls != 2 {
		t.Fatalf("expected two backup managers when auto-save and auto-clean are enabled, got %d", calls)
	}
	if saveManager.autoSaveCalls != 1 {
		t.Fatalf("expected auto-save loop to start on save manager, got %d", saveManager.autoSaveCalls)
	}
	if saveManager.autoCleanCalls != 0 {
		t.Fatalf("did not expect auto-clean loop on save manager, got %d", saveManager.autoCleanCalls)
	}
	if cleanManager.autoCleanCalls != 1 {
		t.Fatalf("expected auto-clean loop to start on dedicated clean manager, got %d", cleanManager.autoCleanCalls)
	}
	if cleanManager.autoSaveCalls != 0 {
		t.Fatalf("did not expect auto-save loop on clean manager, got %d", cleanManager.autoSaveCalls)
	}
}

func TestBackupSourcesGeneratesUniqueArchivePathsForCollidingAgentTokens(t *testing.T) {
	cfg := config.Default()
	cfg.Agents.List = []config.AgentConfig{
		{ID: "ops/team"},
		{ID: "ops team"},
	}

	resolver := staticWorkspaceResolver{
		paths: map[string]string{
			"default":  "/tmp/ws-default",
			"ops/team": "/tmp/ws-ops-team",
			"ops team": "/tmp/ws-ops-space",
		},
	}

	sources, err := backupSources(cfg, "/tmp/state-root", resolver)
	if err != nil {
		t.Fatalf("backupSources: %v", err)
	}

	archivePathBySourcePath := map[string]string{}
	for _, source := range sources {
		archivePathBySourcePath[source.Path] = source.ArchivePath
	}

	opsTeamPath := archivePathBySourcePath["/tmp/ws-ops-team"]
	opsSpacePath := archivePathBySourcePath["/tmp/ws-ops-space"]
	if opsTeamPath == "" || opsSpacePath == "" {
		t.Fatalf("expected archive paths for both agent workspaces, got %v", archivePathBySourcePath)
	}
	if opsTeamPath == opsSpacePath {
		t.Fatalf("expected unique archive paths for colliding tokens, got same path %q", opsTeamPath)
	}
	if !strings.HasPrefix(opsTeamPath, "workspace-ops-team-") {
		t.Fatalf("expected normalized workspace archive path prefix for ops/team, got %q", opsTeamPath)
	}
	if !strings.HasPrefix(opsSpacePath, "workspace-ops-team-") {
		t.Fatalf("expected normalized workspace archive path prefix for ops team, got %q", opsSpacePath)
	}
}

func TestArchivePathForAgentStableAcrossCalls(t *testing.T) {
	first := archivePathForAgent("agent/ops")
	second := archivePathForAgent("agent/ops")
	if first != second {
		t.Fatalf("expected stable archive path per agent ID, got %q then %q", first, second)
	}
}

type stubBackupManager struct {
	stateRoot      string
	autoSaveCalls  int
	autoCleanCalls int
}

func (s *stubBackupManager) StateRoot() string {
	return s.stateRoot
}

func (s *stubBackupManager) CreateBackup(ctx context.Context, sources []backup.Source) (string, error) {
	_ = ctx
	_ = sources
	return "/tmp/mock-backup.tar.gz", nil
}

func (s *stubBackupManager) StartAutoSave(ctx context.Context, interval time.Duration, sourcesFn func() ([]backup.Source, error)) {
	_ = ctx
	_ = interval
	_ = sourcesFn
	s.autoSaveCalls++
}

func (s *stubBackupManager) StartAutoClean(ctx context.Context, interval time.Duration) {
	_ = ctx
	_ = interval
	s.autoCleanCalls++
}

type staticWorkspaceResolver struct {
	paths map[string]string
}

func (s staticWorkspaceResolver) ResolveWorkspacePath(agentID string) (string, error) {
	path, ok := s.paths[agentID]
	if !ok {
		return "", os.ErrNotExist
	}
	return path, nil
}

func TestBackupSourcesDefaultEntriesRemainUnchanged(t *testing.T) {
	cfg := config.Default()
	cfg.Agents.List = nil
	resolver := staticWorkspaceResolver{
		paths: map[string]string{
			"default": "/tmp/ws-default",
		},
	}

	sources, err := backupSources(cfg, "/tmp/state-root", resolver)
	if err != nil {
		t.Fatalf("backupSources: %v", err)
	}

	got := []string{}
	for _, source := range sources {
		got = append(got, source.ArchivePath)
	}
	want := []string{"localclaw.json", "cron", "workspace"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected default backup archive paths %v, got %v", want, got)
	}
}
