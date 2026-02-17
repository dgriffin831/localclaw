package cli

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dgriffin831/localclaw/internal/backup"
	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/runtime"
)

var startBackgroundBackupLoops = StartBackupLoops
var buildBackupManager = newBackupManager

type backupManager interface {
	StateRoot() string
	CreateBackup(ctx context.Context, sources []backup.Source) (string, error)
	StartAutoSave(ctx context.Context, interval time.Duration, sourcesFn func() ([]backup.Source, error))
	StartAutoClean(ctx context.Context, interval time.Duration)
}

func RunBackupCommand(ctx context.Context, cfg config.Config, app *runtime.App, args []string, stdout, stderr io.Writer) error {
	_ = stderr
	if stdout == nil {
		stdout = os.Stdout
	}
	if len(args) > 0 {
		return errors.New("backup command does not accept positional arguments")
	}
	if app == nil {
		return errors.New("runtime app is required")
	}

	manager, _, err := buildBackupManager(cfg)
	if err != nil {
		return err
	}
	sources, err := backupSources(cfg, manager.StateRoot(), app)
	if err != nil {
		return err
	}

	archivePath, err := manager.CreateBackup(ctx, sources)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "backup created: %s\n", archivePath)
	return nil
}

func StartBackupLoops(ctx context.Context, cfg config.Config, app *runtime.App) {
	if app == nil {
		return
	}
	if !cfg.Backup.AutoSave && !cfg.Backup.AutoClean {
		return
	}

	saveManager, interval, err := buildBackupManager(cfg)
	if err != nil {
		log.Printf("backup: unable to start background loops: %v", err)
		return
	}
	if cfg.Backup.AutoSave {
		sourcesFn := func() ([]backup.Source, error) {
			return backupSources(cfg, saveManager.StateRoot(), app)
		}
		saveManager.StartAutoSave(ctx, interval, sourcesFn)
	}
	if cfg.Backup.AutoClean {
		cleanManager := saveManager
		cleanInterval := interval
		if cfg.Backup.AutoSave {
			// Use a dedicated manager to avoid auto-clean starvation from CreateBackup run locks.
			dedicatedCleanManager, dedicatedInterval, cleanErr := buildBackupManager(cfg)
			if cleanErr != nil {
				log.Printf("backup: unable to start auto-clean loop: %v", cleanErr)
				return
			}
			cleanManager = dedicatedCleanManager
			cleanInterval = dedicatedInterval
		}
		cleanManager.StartAutoClean(ctx, cleanInterval)
	}
}

func newBackupManager(cfg config.Config) (backupManager, time.Duration, error) {
	interval, err := backup.ParseInterval(cfg.Backup.Interval)
	if err != nil {
		return nil, 0, err
	}
	manager, err := backup.NewManager(backup.Settings{
		StateRoot:   cfg.App.Root,
		RetainCount: cfg.Backup.RetainCount,
	})
	if err != nil {
		return nil, 0, err
	}
	return manager, interval, nil
}

type backupWorkspaceResolver interface {
	ResolveWorkspacePath(agentID string) (string, error)
}

func backupSources(cfg config.Config, stateRoot string, resolver backupWorkspaceResolver) ([]backup.Source, error) {
	sources := []backup.Source{
		{
			Path:        filepath.Join(stateRoot, "localclaw.json"),
			ArchivePath: "localclaw.json",
			Optional:    true,
		},
		{
			Path:        filepath.Join(stateRoot, "cron"),
			ArchivePath: "cron",
			Optional:    true,
		},
	}

	defaultWorkspace, err := resolver.ResolveWorkspacePath(runtime.DefaultAgentID)
	if err != nil {
		return nil, fmt.Errorf("resolve default workspace path: %w", err)
	}
	sources = append(sources, backup.Source{
		Path:        defaultWorkspace,
		ArchivePath: "workspace",
		Optional:    true,
	})

	for _, agent := range cfg.Agents.List {
		agentID := strings.TrimSpace(agent.ID)
		if agentID == "" {
			continue
		}
		workspacePath, err := resolver.ResolveWorkspacePath(agentID)
		if err != nil {
			return nil, fmt.Errorf("resolve workspace path for agent %q: %w", agentID, err)
		}
		sources = append(sources, backup.Source{
			Path:        workspacePath,
			ArchivePath: archivePathForAgent(agentID),
			Optional:    true,
		})
	}

	return sources, nil
}

func archivePathForAgent(agentID string) string {
	trimmed := strings.TrimSpace(agentID)
	token := sanitizeArchiveToken(trimmed)
	hash := stableArchiveTokenHash(trimmed)
	return "workspace-" + token + "-" + hash
}

func stableArchiveTokenHash(value string) string {
	sum := sha1.Sum([]byte(value))
	return hex.EncodeToString(sum[:4])
}

func sanitizeArchiveToken(value string) string {
	replacer := strings.NewReplacer("/", "-", "\\", "-", " ", "-")
	out := replacer.Replace(strings.TrimSpace(value))
	if out == "" {
		return "default"
	}
	return out
}
