package memory

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallVectorModelPrimarySuccess(t *testing.T) {
	body := []byte("primary model")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer server.Close()

	cfg := testModelConfig(t, body)
	cfg.Model.PrimaryURL = server.URL + "/model.gguf"
	result, err := InstallVectorModel(context.Background(), cfg, ModelInstallOptions{Source: ModelSourceAuto})
	if err != nil {
		t.Fatalf("InstallVectorModel: %v", err)
	}
	if result.Source != ModelSourceHuggingFace || !result.Installed {
		t.Fatalf("unexpected result: %+v", result)
	}
	if _, err := os.Stat(cfg.Model.Path); err != nil {
		t.Fatalf("expected installed model: %v", err)
	}
}

func TestInstallVectorModelFallsBackToMirror(t *testing.T) {
	body := []byte("mirror model")
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "blocked", http.StatusForbidden)
	}))
	defer primary.Close()
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer mirror.Close()

	cfg := testModelConfig(t, body)
	cfg.Model.PrimaryURL = primary.URL + "/model.gguf"
	cfg.Model.MirrorURL = mirror.URL + "/model.gguf"
	result, err := InstallVectorModel(context.Background(), cfg, ModelInstallOptions{Source: ModelSourceAuto})
	if err != nil {
		t.Fatalf("InstallVectorModel: %v", err)
	}
	if result.Source != ModelSourceMirror {
		t.Fatalf("expected mirror fallback, got %+v", result)
	}
	if len(result.AttemptedSources) != 2 {
		t.Fatalf("expected primary and mirror attempts, got %+v", result.AttemptedSources)
	}
}

func TestInstallVectorModelSHAMismatchLeavesNoPartialFile(t *testing.T) {
	body := []byte("bad model")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer server.Close()

	cfg := testModelConfig(t, []byte("expected model"))
	cfg.Model.MirrorURL = server.URL + "/model.gguf"
	errPath := cfg.Model.Path
	if _, err := InstallVectorModel(context.Background(), cfg, ModelInstallOptions{Source: ModelSourceMirror}); err == nil {
		t.Fatalf("expected sha mismatch error")
	}
	if _, err := os.Stat(errPath); !os.IsNotExist(err) {
		t.Fatalf("expected no partial model file, stat err=%v", err)
	}
}

func TestInstallVectorModelMirrorSourceOnlyUsesMirror(t *testing.T) {
	body := []byte("mirror only")
	primaryHits := 0
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryHits++
		http.Error(w, "should not be called", http.StatusTeapot)
	}))
	defer primary.Close()
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer mirror.Close()

	cfg := testModelConfig(t, body)
	cfg.Model.PrimaryURL = primary.URL + "/model.gguf"
	cfg.Model.MirrorURL = mirror.URL + "/model.gguf"
	if _, err := InstallVectorModel(context.Background(), cfg, ModelInstallOptions{Source: ModelSourceMirror}); err != nil {
		t.Fatalf("InstallVectorModel: %v", err)
	}
	if primaryHits != 0 {
		t.Fatalf("expected primary not to be called, got %d hits", primaryHits)
	}
}

func testModelConfig(t *testing.T, expected []byte) VectorConfig {
	t.Helper()
	sum := sha256.Sum256(expected)
	return VectorConfig{
		Enabled:    true,
		Provider:   "llama.cpp",
		SearchMode: SearchModeHybrid,
		Model: VectorModelConfig{
			ID:         "test-model",
			Path:       filepath.Join(t.TempDir(), "model.gguf"),
			PrimaryURL: "https://example.invalid/primary.gguf",
			MirrorURL:  "https://example.invalid/mirror.gguf",
			SHA256:     fmt.Sprintf("%x", sum),
		},
		Server: VectorServerConfig{Managed: true, Binary: "llama-server", Host: "127.0.0.1", StartupTimeoutSeconds: 1},
	}
}
