package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	ModelSourceAuto        = "auto"
	ModelSourceHuggingFace = "huggingface"
	ModelSourceMirror      = "mirror"
)

type ModelInstallOptions struct {
	Source string
	Client *http.Client
}

type ModelInstallResult struct {
	Path             string   `json:"path"`
	Source           string   `json:"source"`
	URL              string   `json:"url"`
	SHA256           string   `json:"sha256"`
	Bytes            int64    `json:"bytes"`
	Installed        bool     `json:"installed"`
	LlamaServerFound bool     `json:"llamaServerFound"`
	AttemptedSources []string `json:"attemptedSources"`
}

type ModelStatus struct {
	Path             string `json:"path"`
	Exists           bool   `json:"exists"`
	SHA256           string `json:"sha256,omitempty"`
	ExpectedSHA256   string `json:"expectedSha256"`
	Verified         bool   `json:"verified"`
	LlamaServerFound bool   `json:"llamaServerFound"`
	PrimaryURL       string `json:"primaryUrl"`
	MirrorURL        string `json:"mirrorUrl"`
}

func InstallVectorModel(ctx context.Context, cfg VectorConfig, opts ModelInstallOptions) (ModelInstallResult, error) {
	source := strings.ToLower(strings.TrimSpace(opts.Source))
	if source == "" {
		source = ModelSourceAuto
	}
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Minute}
	}
	path, err := ResolveUserPath(cfg.Model.Path)
	if err != nil {
		return ModelInstallResult{}, err
	}
	if strings.TrimSpace(cfg.Model.SHA256) == "" {
		return ModelInstallResult{}, errors.New("vector model sha256 is required")
	}

	sources, err := modelDownloadSources(cfg, source)
	if err != nil {
		return ModelInstallResult{}, err
	}

	out := ModelInstallResult{
		Path:             path,
		SHA256:           cfg.Model.SHA256,
		LlamaServerFound: commandAvailable(cfg.Server.Binary),
	}
	for _, candidate := range sources {
		out.AttemptedSources = append(out.AttemptedSources, candidate.Name)
		result, err := downloadAndVerifyModel(ctx, client, path, candidate.URL, candidate.Name, cfg.Model.SHA256)
		if err != nil {
			if source == ModelSourceAuto {
				continue
			}
			return out, err
		}
		result.LlamaServerFound = out.LlamaServerFound
		result.AttemptedSources = out.AttemptedSources
		return result, nil
	}
	return out, fmt.Errorf("download vector model: all sources failed")
}

func VectorModelStatus(cfg VectorConfig) ModelStatus {
	path, err := ResolveUserPath(cfg.Model.Path)
	if err != nil {
		path = cfg.Model.Path
	}
	status := ModelStatus{
		Path:             path,
		ExpectedSHA256:   cfg.Model.SHA256,
		LlamaServerFound: commandAvailable(cfg.Server.Binary),
		PrimaryURL:       cfg.Model.PrimaryURL,
		MirrorURL:        cfg.Model.MirrorURL,
	}
	digest, err := fileSHA256(path)
	if err != nil {
		return status
	}
	status.Exists = true
	status.SHA256 = digest
	status.Verified = strings.EqualFold(digest, cfg.Model.SHA256)
	return status
}

func ResolveUserPath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", errors.New("path is required")
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
	if filepath.IsAbs(trimmed) {
		return filepath.Clean(trimmed), nil
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

type modelSource struct {
	Name string
	URL  string
}

func modelDownloadSources(cfg VectorConfig, source string) ([]modelSource, error) {
	switch source {
	case ModelSourceAuto:
		return []modelSource{
			{Name: ModelSourceHuggingFace, URL: cfg.Model.PrimaryURL},
			{Name: ModelSourceMirror, URL: cfg.Model.MirrorURL},
		}, nil
	case ModelSourceHuggingFace:
		return []modelSource{{Name: ModelSourceHuggingFace, URL: cfg.Model.PrimaryURL}}, nil
	case ModelSourceMirror:
		return []modelSource{{Name: ModelSourceMirror, URL: cfg.Model.MirrorURL}}, nil
	default:
		return nil, fmt.Errorf("unknown model source %q (supported: auto, huggingface, mirror)", source)
	}
}

func downloadAndVerifyModel(ctx context.Context, client *http.Client, path, url, source, expectedSHA string) (ModelInstallResult, error) {
	if strings.TrimSpace(url) == "" {
		return ModelInstallResult{}, fmt.Errorf("%s model url is empty", source)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return ModelInstallResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ModelInstallResult{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return ModelInstallResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ModelInstallResult{}, fmt.Errorf("download %s model: status %s", source, resp.Status)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return ModelInstallResult{}, err
	}
	tmpPath := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(tmp, hasher), resp.Body)
	closeErr := tmp.Close()
	if copyErr != nil {
		return ModelInstallResult{}, copyErr
	}
	if closeErr != nil {
		return ModelInstallResult{}, closeErr
	}

	gotSHA := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(gotSHA, expectedSHA) {
		return ModelInstallResult{}, fmt.Errorf("model sha256 mismatch: got %s want %s", gotSHA, expectedSHA)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return ModelInstallResult{}, err
	}
	removeTmp = false
	return ModelInstallResult{
		Path:      path,
		Source:    source,
		URL:       url,
		SHA256:    gotSHA,
		Bytes:     written,
		Installed: true,
	}, nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func commandAvailable(binary string) bool {
	trimmed := strings.TrimSpace(binary)
	if trimmed == "" {
		return false
	}
	if strings.ContainsAny(trimmed, `/\`) {
		info, err := os.Stat(trimmed)
		return err == nil && !info.IsDir()
	}
	_, err := exec.LookPath(trimmed)
	return err == nil
}
