package cron

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const cronStoreVersion = 1

type storeFile struct {
	Version int     `json:"version"`
	Jobs    []Entry `json:"jobs"`
}

func resolveStorePath(stateRoot string) string {
	root := strings.TrimSpace(stateRoot)
	if root == "" {
		return ""
	}
	return filepath.Join(root, "cron", "jobs.json")
}

func loadEntries(storePath string) ([]Entry, error) {
	path := strings.TrimSpace(storePath)
	if path == "" {
		return []Entry{}, nil
	}

	payload, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Entry{}, nil
		}
		return nil, fmt.Errorf("read cron store: %w", err)
	}

	var decoded storeFile
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, fmt.Errorf("parse cron store: %w", err)
	}
	if decoded.Version != 0 && decoded.Version != cronStoreVersion {
		return nil, fmt.Errorf("unsupported cron store version %d", decoded.Version)
	}
	if decoded.Jobs == nil {
		return []Entry{}, nil
	}
	return decoded.Jobs, nil
}

func saveEntries(storePath string, entries []Entry) error {
	path := strings.TrimSpace(storePath)
	if path == "" {
		return nil
	}

	dir := filepath.Dir(filepath.Clean(path))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create cron store dir: %w", err)
	}

	out := make([]Entry, len(entries))
	copy(out, entries)
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})

	encoded, err := json.MarshalIndent(storeFile{Version: cronStoreVersion, Jobs: out}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cron store: %w", err)
	}
	encoded = append(encoded, '\n')

	tmpPath := fmt.Sprintf("%s.%d.%d.tmp", path, os.Getpid(), time.Now().UnixNano())
	if err := os.WriteFile(filepath.Clean(tmpPath), encoded, 0o600); err != nil {
		return fmt.Errorf("write cron store temp file: %w", err)
	}
	if err := os.Rename(filepath.Clean(tmpPath), filepath.Clean(path)); err != nil {
		_ = os.Remove(filepath.Clean(tmpPath))
		return fmt.Errorf("rename cron store temp file: %w", err)
	}
	return nil
}
