package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestImportLegacyMemoryJSONCreatesMemoryAndMarker(t *testing.T) {
	workspace := t.TempDir()
	legacyPath := filepath.Join(t.TempDir(), "legacy-memory.json")
	if err := os.WriteFile(legacyPath, []byte(`{"project":"alpha","notes":["first","second"]}`), 0o600); err != nil {
		t.Fatalf("write legacy json: %v", err)
	}

	result, err := ImportLegacyMemoryJSON(context.Background(), LegacyImportRequest{
		WorkspacePath: workspace,
		LegacyPath:    legacyPath,
		Now: func() time.Time {
			return time.Date(2026, 2, 15, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("import legacy memory: %v", err)
	}
	if !result.Imported {
		t.Fatalf("expected import to run")
	}

	memoryPath := filepath.Join(workspace, "MEMORY.md")
	body, err := os.ReadFile(memoryPath)
	if err != nil {
		t.Fatalf("read MEMORY.md: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "Legacy Memory Import") {
		t.Fatalf("expected legacy import heading in MEMORY.md")
	}
	if !strings.Contains(text, "\"project\": \"alpha\"") {
		t.Fatalf("expected legacy JSON payload in MEMORY.md")
	}

	markerPath := filepath.Join(workspace, legacyImportMarkerFilename)
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("expected marker file at %q: %v", markerPath, err)
	}
}

func TestImportLegacyMemoryJSONIsIdempotent(t *testing.T) {
	workspace := t.TempDir()
	legacyPath := filepath.Join(t.TempDir(), "legacy-memory.json")
	if err := os.WriteFile(legacyPath, []byte(`{"topic":"alpha"}`), 0o600); err != nil {
		t.Fatalf("write legacy json: %v", err)
	}

	if _, err := ImportLegacyMemoryJSON(context.Background(), LegacyImportRequest{
		WorkspacePath: workspace,
		LegacyPath:    legacyPath,
	}); err != nil {
		t.Fatalf("initial import: %v", err)
	}

	first, err := os.ReadFile(filepath.Join(workspace, "MEMORY.md"))
	if err != nil {
		t.Fatalf("read MEMORY.md first pass: %v", err)
	}

	if err := os.WriteFile(legacyPath, []byte(`{"topic":"beta"}`), 0o600); err != nil {
		t.Fatalf("rewrite legacy json: %v", err)
	}

	result, err := ImportLegacyMemoryJSON(context.Background(), LegacyImportRequest{
		WorkspacePath: workspace,
		LegacyPath:    legacyPath,
	})
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if result.Imported {
		t.Fatalf("expected marker to prevent re-import")
	}

	second, err := os.ReadFile(filepath.Join(workspace, "MEMORY.md"))
	if err != nil {
		t.Fatalf("read MEMORY.md second pass: %v", err)
	}
	if string(second) != string(first) {
		t.Fatalf("expected MEMORY.md unchanged on second import")
	}
}

func TestImportLegacyMemoryJSONInvalidPayloadDoesNotCreateMarker(t *testing.T) {
	workspace := t.TempDir()
	legacyPath := filepath.Join(t.TempDir(), "legacy-memory.json")
	if err := os.WriteFile(legacyPath, []byte(`{`), 0o600); err != nil {
		t.Fatalf("write invalid legacy json: %v", err)
	}

	if _, err := ImportLegacyMemoryJSON(context.Background(), LegacyImportRequest{
		WorkspacePath: workspace,
		LegacyPath:    legacyPath,
	}); err == nil {
		t.Fatalf("expected parse error")
	}

	if _, err := os.Stat(filepath.Join(workspace, legacyImportMarkerFilename)); !os.IsNotExist(err) {
		t.Fatalf("expected no marker on failed import, got err=%v", err)
	}
}

func TestImportLegacyMemoryJSONResolvesRelativePathFromWorkspace(t *testing.T) {
	workspace := t.TempDir()
	legacyRel := filepath.Join(".localclaw", "memory.json")
	legacyPath := filepath.Join(workspace, legacyRel)
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	if err := os.WriteFile(legacyPath, []byte(`{"topic":"workspace-relative"}`), 0o600); err != nil {
		t.Fatalf("write legacy json: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	otherDir := t.TempDir()
	if err := os.Chdir(otherDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})

	result, err := ImportLegacyMemoryJSON(context.Background(), LegacyImportRequest{
		WorkspacePath: workspace,
		LegacyPath:    legacyRel,
	})
	if err != nil {
		t.Fatalf("import legacy memory: %v", err)
	}
	if !result.Imported {
		t.Fatalf("expected workspace-relative import to run")
	}

	body, err := os.ReadFile(filepath.Join(workspace, "MEMORY.md"))
	if err != nil {
		t.Fatalf("read MEMORY.md: %v", err)
	}
	if !strings.Contains(string(body), "workspace-relative") {
		t.Fatalf("expected imported workspace-relative payload in MEMORY.md")
	}
}

func TestImportLegacyMemoryJSONExpandsTildePath(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	legacyRel := filepath.Join(".localclaw", "legacy-memory.json")
	legacyPath := filepath.Join(home, legacyRel)
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	if err := os.WriteFile(legacyPath, []byte(`{"topic":"home-path"}`), 0o600); err != nil {
		t.Fatalf("write legacy json: %v", err)
	}

	result, err := ImportLegacyMemoryJSON(context.Background(), LegacyImportRequest{
		WorkspacePath: workspace,
		LegacyPath:    "~/" + filepath.ToSlash(legacyRel),
	})
	if err != nil {
		t.Fatalf("import legacy memory: %v", err)
	}
	if !result.Imported {
		t.Fatalf("expected tilde path import to run")
	}

	body, err := os.ReadFile(filepath.Join(workspace, "MEMORY.md"))
	if err != nil {
		t.Fatalf("read MEMORY.md: %v", err)
	}
	if !strings.Contains(string(body), "home-path") {
		t.Fatalf("expected imported home payload in MEMORY.md")
	}
}
