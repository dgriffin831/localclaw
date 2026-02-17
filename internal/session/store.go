package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var ErrLockTimeout = errors.New("session store lock timeout")

type Settings struct {
	StateRoot         string
	StorePath         string
	KnownAgentIDs     []string
	LockTimeout       time.Duration
	LockStaleAfter    time.Duration
	LockRetryInterval time.Duration
	Now               func() time.Time
	Sleep             func(time.Duration)
}

type Store struct {
	settings Settings
}

type sessionsFile struct {
	Sessions map[string]SessionEntry `json:"sessions"`
}

func NewStore(settings Settings) *Store {
	if strings.TrimSpace(settings.StorePath) == "" {
		settings.StorePath = filepath.Join("agents", "{agentId}", "sessions", "sessions.json")
	}
	if settings.LockTimeout <= 0 {
		settings.LockTimeout = 5 * time.Second
	}
	if settings.LockStaleAfter <= 0 {
		settings.LockStaleAfter = 15 * time.Second
	}
	if settings.LockRetryInterval <= 0 {
		settings.LockRetryInterval = 25 * time.Millisecond
	}
	if settings.Now == nil {
		settings.Now = time.Now
	}
	if settings.Sleep == nil {
		settings.Sleep = time.Sleep
	}

	return &Store{settings: settings}
}

func (s *Store) Init(ctx context.Context) error {
	agentIDs := make([]string, 0, len(s.settings.KnownAgentIDs)+1)
	agentIDs = append(agentIDs, "default")
	for _, agentID := range s.settings.KnownAgentIDs {
		normalized := normalizeAgentID(agentID)
		if normalized == "default" {
			continue
		}
		agentIDs = append(agentIDs, normalized)
	}

	seen := map[string]struct{}{}
	for _, agentID := range agentIDs {
		if _, ok := seen[agentID]; ok {
			continue
		}
		seen[agentID] = struct{}{}

		if err := s.ensureStoreFile(ctx, agentID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ResolveSessionsPath(agentID string) (string, error) {
	normalized := normalizeAgentID(agentID)
	resolved := strings.ReplaceAll(strings.TrimSpace(s.settings.StorePath), "{agentId}", sanitizeAgentPathToken(normalized))
	if strings.TrimSpace(resolved) == "" {
		return "", errors.New("session store path is empty")
	}

	resolvedPath, err := expandAndCleanPath(resolved)
	if err != nil {
		return "", fmt.Errorf("resolve session store path: %w", err)
	}

	stateRoot, err := expandAndCleanPath(strings.TrimSpace(s.settings.StateRoot))
	if err != nil {
		return "", fmt.Errorf("resolve state root: %w", err)
	}

	if !filepath.IsAbs(resolvedPath) {
		resolvedPath = filepath.Join(stateRoot, resolvedPath)
	}
	return filepath.Clean(resolvedPath), nil
}

func (s *Store) ResolveSessionsDir(agentID string) (string, error) {
	path, err := s.ResolveSessionsPath(agentID)
	if err != nil {
		return "", err
	}
	return filepath.Dir(path), nil
}

func (s *Store) ResolveTranscriptPath(agentID, sessionID string) (string, error) {
	dir, err := s.ResolveSessionsDir(agentID)
	if err != nil {
		return "", err
	}
	normalizedSessionID := sanitizePathToken(strings.TrimSpace(sessionID))
	if normalizedSessionID == "" {
		return "", errors.New("session id is required")
	}
	return filepath.Join(dir, normalizedSessionID+".jsonl"), nil
}

func (s *Store) Load(ctx context.Context, agentID string) (map[string]SessionEntry, error) {
	path, err := s.ResolveSessionsPath(agentID)
	if err != nil {
		return nil, err
	}
	if err := s.ensureStoreFile(ctx, agentID); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read session store: %w", err)
	}

	decoded := sessionsFile{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &decoded); err != nil {
			return nil, fmt.Errorf("parse session store: %w", err)
		}
	}
	if decoded.Sessions == nil {
		decoded.Sessions = map[string]SessionEntry{}
	}
	return decoded.Sessions, nil
}

func (s *Store) Save(ctx context.Context, agentID string, sessions map[string]SessionEntry) error {
	path, err := s.ResolveSessionsPath(agentID)
	if err != nil {
		return err
	}
	if err := s.ensureStoreFile(ctx, agentID); err != nil {
		return err
	}

	release, err := s.acquireFileLock(ctx, path)
	if err != nil {
		return err
	}
	defer release()

	return s.writeSessions(path, sessions)
}

func (s *Store) Get(ctx context.Context, agentID, sessionID string) (SessionEntry, bool, error) {
	sessions, err := s.Load(ctx, agentID)
	if err != nil {
		return SessionEntry{}, false, err
	}
	normalizedID := strings.TrimSpace(sessionID)
	entry, ok := sessions[normalizedID]
	return entry, ok, nil
}

func (s *Store) Update(ctx context.Context, agentID, sessionID string, updateFn func(*SessionEntry) error) (SessionEntry, error) {
	if updateFn == nil {
		return SessionEntry{}, errors.New("update function is required")
	}
	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return SessionEntry{}, errors.New("session id is required")
	}

	path, err := s.ResolveSessionsPath(agentID)
	if err != nil {
		return SessionEntry{}, err
	}
	if err := s.ensureStoreFile(ctx, agentID); err != nil {
		return SessionEntry{}, err
	}

	release, err := s.acquireFileLock(ctx, path)
	if err != nil {
		return SessionEntry{}, err
	}
	defer release()

	sessions, err := s.readSessionsFile(path)
	if err != nil {
		return SessionEntry{}, err
	}

	now := s.settings.Now().UTC().Format(time.RFC3339Nano)
	normalizedAgentID := normalizeAgentID(agentID)
	entry, exists := sessions[normalizedSessionID]
	if !exists {
		entry = SessionEntry{
			ID:        normalizedSessionID,
			AgentID:   normalizedAgentID,
			Origin:    OriginUnknown,
			CreatedAt: now,
			UpdatedAt: now,
		}
	}
	if strings.TrimSpace(entry.ID) == "" {
		entry.ID = normalizedSessionID
	}
	if strings.TrimSpace(entry.AgentID) == "" {
		entry.AgentID = normalizedAgentID
	}
	if strings.TrimSpace(entry.CreatedAt) == "" {
		entry.CreatedAt = now
	}
	createdAt := entry.CreatedAt
	entry.UpdatedAt = now

	if transcriptPath, transcriptErr := s.ResolveTranscriptPath(agentID, normalizedSessionID); transcriptErr == nil {
		entry.TranscriptPath = transcriptPath
	}

	if err := updateFn(&entry); err != nil {
		return SessionEntry{}, err
	}

	entry.ID = normalizedSessionID
	entry.AgentID = normalizedAgentID
	entry.CreatedAt = createdAt
	entry.UpdatedAt = now

	sessions[normalizedSessionID] = entry
	if err := s.writeSessions(path, sessions); err != nil {
		return SessionEntry{}, err
	}
	return entry, nil
}

func (s *Store) Delete(ctx context.Context, agentID, sessionID string) (bool, error) {
	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return false, errors.New("session id is required")
	}

	path, err := s.ResolveSessionsPath(agentID)
	if err != nil {
		return false, err
	}
	if err := s.ensureStoreFile(ctx, agentID); err != nil {
		return false, err
	}

	release, err := s.acquireFileLock(ctx, path)
	if err != nil {
		return false, err
	}
	defer release()

	sessions, err := s.readSessionsFile(path)
	if err != nil {
		return false, err
	}
	if _, exists := sessions[normalizedSessionID]; !exists {
		return false, nil
	}

	delete(sessions, normalizedSessionID)
	if err := s.writeSessions(path, sessions); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) ensureStoreFile(ctx context.Context, agentID string) error {
	path, err := s.ResolveSessionsPath(agentID)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir session store dir: %w", err)
	}

	if _, err := os.Stat(path); err == nil {
		hardenFilePerms(path, 0o600)
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat session store: %w", err)
	}

	empty := sessionsFile{Sessions: map[string]SessionEntry{}}
	buf, err := json.MarshalIndent(empty, "", "  ")
	if err != nil {
		return fmt.Errorf("encode empty session store: %w", err)
	}
	buf = append(buf, '\n')
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		return fmt.Errorf("create session store: %w", err)
	}
	hardenFilePerms(path, 0o600)
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return nil
}

func (s *Store) readSessionsFile(path string) (map[string]SessionEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]SessionEntry{}, nil
		}
		return nil, fmt.Errorf("read session store: %w", err)
	}
	decoded := sessionsFile{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &decoded); err != nil {
			return nil, fmt.Errorf("parse session store: %w", err)
		}
	}
	if decoded.Sessions == nil {
		decoded.Sessions = map[string]SessionEntry{}
	}
	return decoded.Sessions, nil
}

func (s *Store) writeSessions(path string, sessions map[string]SessionEntry) error {
	payload := sessionsFile{Sessions: sessions}
	buf, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("encode session store: %w", err)
	}
	buf = append(buf, '\n')

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o600); err != nil {
		return fmt.Errorf("write session store temp file: %w", err)
	}
	hardenFilePerms(tmp, 0o600)
	if err := replaceSessionStoreFile(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace session store: %w", err)
	}
	hardenFilePerms(path, 0o600)
	return nil
}

func replaceSessionStoreFile(tmpPath, targetPath string) error {
	if runtime.GOOS == "windows" {
		if err := os.Remove(targetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return os.Rename(tmpPath, targetPath)
}

func (s *Store) acquireFileLock(ctx context.Context, filePath string) (func(), error) {
	lockPath := lockFilePath(filePath)
	start := s.settings.Now()

	for {
		handle, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			_, writeErr := io.WriteString(handle, strconv.FormatInt(s.settings.Now().UnixNano(), 10))
			closeErr := handle.Close()
			if writeErr != nil {
				_ = os.Remove(lockPath)
				return nil, fmt.Errorf("write lock file: %w", writeErr)
			}
			if closeErr != nil {
				_ = os.Remove(lockPath)
				return nil, fmt.Errorf("close lock file: %w", closeErr)
			}
			hardenFilePerms(lockPath, 0o600)
			return func() {
				_ = os.Remove(lockPath)
			}, nil
		}

		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("create lock file: %w", err)
		}

		stale, staleErr := s.isStaleLock(lockPath)
		if staleErr != nil {
			return nil, staleErr
		}
		if stale {
			removeErr := os.Remove(lockPath)
			if removeErr == nil || errors.Is(removeErr, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("remove stale lock: %w", removeErr)
		}

		if s.settings.Now().Sub(start) >= s.settings.LockTimeout {
			return nil, ErrLockTimeout
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		s.settings.Sleep(s.settings.LockRetryInterval)
	}
}

func (s *Store) isStaleLock(path string) (bool, error) {
	createdAt, hasCreatedAt, err := readLockCreatedAt(path)
	if err != nil {
		return false, err
	}
	if hasCreatedAt {
		return s.settings.Now().Sub(createdAt) > s.settings.LockStaleAfter, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("stat lock file: %w", err)
	}
	return s.settings.Now().Sub(info.ModTime()) > s.settings.LockStaleAfter, nil
}

func readLockCreatedAt(path string) (time.Time, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, fmt.Errorf("read lock file: %w", err)
	}

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return time.Time{}, false, nil
	}
	if unixNanos, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return time.Unix(0, unixNanos), true, nil
	}
	if timestamp, err := time.Parse(time.RFC3339Nano, trimmed); err == nil {
		return timestamp, true, nil
	}
	return time.Time{}, false, nil
}

func lockFilePath(filePath string) string {
	return filePath + ".lock"
}

func normalizeAgentID(agentID string) string {
	trimmed := strings.TrimSpace(agentID)
	if trimmed == "" {
		return "default"
	}
	return trimmed
}

func sanitizePathToken(value string) string {
	replacer := strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-")
	return strings.TrimSpace(replacer.Replace(value))
}

func sanitizeAgentPathToken(agentID string) string {
	normalized := sanitizePathToken(agentID)
	switch normalized {
	case "", ".", "..":
		return "default"
	default:
		return normalized
	}
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

func hardenFilePerms(path string, mode os.FileMode) {
	if runtime.GOOS == "windows" {
		return
	}
	_ = os.Chmod(path, mode)
}
