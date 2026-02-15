package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const legacyImportMarkerFilename = ".localclaw-legacy-memory-import-v1"

// LegacyImportRequest configures one-time import from legacy memory.path JSON.
type LegacyImportRequest struct {
	WorkspacePath string
	LegacyPath    string
	Now           func() time.Time
}

// LegacyImportResult reports the import outcome.
type LegacyImportResult struct {
	Imported   bool
	Skipped    bool
	MarkerPath string
	MemoryPath string
}

// ImportLegacyMemoryJSON imports legacy JSON memory into MEMORY.md once.
func ImportLegacyMemoryJSON(ctx context.Context, req LegacyImportRequest) (LegacyImportResult, error) {
	_ = ctx

	workspacePath := strings.TrimSpace(req.WorkspacePath)
	legacyPath := strings.TrimSpace(req.LegacyPath)
	if workspacePath == "" || legacyPath == "" {
		return LegacyImportResult{Skipped: true}, nil
	}
	now := req.Now
	if now == nil {
		now = time.Now
	}

	workspaceAbs, err := expandPath(workspacePath)
	if err != nil {
		return LegacyImportResult{}, err
	}
	legacyAbs, err := resolveLegacyPath(workspaceAbs, legacyPath)
	if err != nil {
		return LegacyImportResult{}, err
	}
	result := LegacyImportResult{
		MarkerPath: filepath.Join(workspaceAbs, legacyImportMarkerFilename),
		MemoryPath: filepath.Join(workspaceAbs, "MEMORY.md"),
	}

	if _, err := os.Stat(legacyAbs); err != nil {
		if os.IsNotExist(err) {
			result.Skipped = true
			return result, nil
		}
		return LegacyImportResult{}, err
	}
	if _, err := os.Stat(result.MarkerPath); err == nil {
		result.Skipped = true
		return result, nil
	} else if !os.IsNotExist(err) {
		return LegacyImportResult{}, err
	}

	payload, err := os.ReadFile(legacyAbs)
	if err != nil {
		return LegacyImportResult{}, err
	}
	if strings.TrimSpace(string(payload)) == "" {
		if err := writeAtomic(result.MarkerPath, []byte(buildLegacyImportMarker(now(), legacyAbs, "empty"))); err != nil {
			return LegacyImportResult{}, err
		}
		result.Skipped = true
		return result, nil
	}

	var decoded interface{}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return LegacyImportResult{}, fmt.Errorf("parse legacy memory JSON: %w", err)
	}
	pretty, err := json.MarshalIndent(decoded, "", "  ")
	if err != nil {
		return LegacyImportResult{}, err
	}

	existing, err := os.ReadFile(result.MemoryPath)
	if err != nil && !os.IsNotExist(err) {
		return LegacyImportResult{}, err
	}
	section := buildLegacyImportSection(now(), legacyAbs, string(pretty))
	combined := mergeMemoryContent(string(existing), section)
	if err := writeAtomic(result.MemoryPath, []byte(combined)); err != nil {
		return LegacyImportResult{}, err
	}
	if err := writeAtomic(result.MarkerPath, []byte(buildLegacyImportMarker(now(), legacyAbs, "imported"))); err != nil {
		return LegacyImportResult{}, err
	}

	result.Imported = true
	return result, nil
}

func mergeMemoryContent(existing string, section string) string {
	trimmed := strings.TrimRight(existing, "\n")
	if trimmed == "" {
		return section + "\n"
	}
	return trimmed + "\n\n" + section + "\n"
}

func buildLegacyImportSection(at time.Time, source string, payload string) string {
	return fmt.Sprintf(
		"## Legacy Memory Import\n\nImported from `%s` on `%s`.\n\n```json\n%s\n```",
		source,
		at.UTC().Format(time.RFC3339),
		payload,
	)
}

func buildLegacyImportMarker(at time.Time, source string, status string) string {
	return fmt.Sprintf("status=%s\nsource=%s\nat=%s\n", status, source, at.UTC().Format(time.RFC3339))
}

func writeAtomic(path string, payload []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tempPath := fmt.Sprintf("%s.tmp.%d", path, time.Now().UnixNano())
	if err := os.WriteFile(tempPath, payload, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

func resolveLegacyPath(workspaceAbs string, legacyPath string) (string, error) {
	expandedLegacy, err := expandPath(legacyPath)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(expandedLegacy) {
		return filepath.Clean(expandedLegacy), nil
	}

	workspaceCandidate := filepath.Clean(filepath.Join(workspaceAbs, expandedLegacy))
	if _, err := os.Stat(workspaceCandidate); err == nil {
		return workspaceCandidate, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}

	cwdCandidate, err := filepath.Abs(filepath.Clean(expandedLegacy))
	if err != nil {
		return "", err
	}
	return filepath.Clean(cwdCandidate), nil
}

func expandPath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("path is empty")
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
