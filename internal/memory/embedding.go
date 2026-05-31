package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Embedder interface {
	Embed(ctx context.Context, input string) ([]float32, error)
	Close() error
}

type EmbeddingClient struct {
	baseURL string
	client  *http.Client
}

type ManagedEmbedder struct {
	client *EmbeddingClient
	cmd    *exec.Cmd
	log    *os.File
}

func NewManagedEmbedder(ctx context.Context, cfg VectorConfig) (*ManagedEmbedder, error) {
	if !cfg.Server.Managed {
		return nil, errors.New("only managed llama-server embeddings are supported")
	}
	modelPath, err := ResolveUserPath(cfg.Model.Path)
	if err != nil {
		return nil, err
	}
	if !VectorModelStatus(cfg).Verified {
		return nil, errors.New("vector model is missing or sha256 verification failed")
	}
	binary := strings.TrimSpace(cfg.Server.Binary)
	if binary == "" {
		return nil, errors.New("llama-server binary is required")
	}
	host := strings.TrimSpace(cfg.Server.Host)
	if host == "" {
		host = "127.0.0.1"
	}
	port := cfg.Server.Port
	if port == 0 {
		port, err = freeLoopbackPort(host)
		if err != nil {
			return nil, err
		}
	}

	args := llamaServerArgs(modelPath, host, port)
	cmd := exec.CommandContext(ctx, binary, args...)
	logFile, err := os.CreateTemp("", "localclaw-llama-server-*.log")
	if err != nil {
		return nil, err
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		_ = os.Remove(logFile.Name())
		return nil, err
	}

	baseURL := fmt.Sprintf("http://%s:%d", host, port)
	timeout := time.Duration(cfg.Server.StartupTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	embedder := &ManagedEmbedder{
		client: &EmbeddingClient{baseURL: baseURL, client: &http.Client{Timeout: 60 * time.Second}},
		cmd:    cmd,
		log:    logFile,
	}
	if err := embedder.waitReady(ctx, timeout); err != nil {
		_ = embedder.Close()
		return nil, err
	}
	return embedder, nil
}

func llamaServerArgs(modelPath, host string, port int) []string {
	return []string{
		"-m", modelPath,
		"--embeddings",
		"--host", host,
		"--port", fmt.Sprintf("%d", port),
	}
}

func NewHTTPEmbedder(baseURL string) *EmbeddingClient {
	return &EmbeddingClient{baseURL: strings.TrimRight(baseURL, "/"), client: &http.Client{Timeout: 60 * time.Second}}
}

func (e *EmbeddingClient) Embed(ctx context.Context, input string) ([]float32, error) {
	vector, err := e.embedOpenAI(ctx, input)
	if err == nil {
		return normalizeVector(vector), nil
	}
	vector, fallbackErr := e.embedLlama(ctx, input)
	if fallbackErr == nil {
		return normalizeVector(vector), nil
	}
	return nil, fmt.Errorf("embedding request failed: %v; fallback: %w", err, fallbackErr)
}

func (e *EmbeddingClient) Close() error {
	return nil
}

func (m *ManagedEmbedder) Embed(ctx context.Context, input string) ([]float32, error) {
	return m.client.Embed(ctx, input)
}

func (m *ManagedEmbedder) Close() error {
	if m.cmd == nil || m.cmd.Process == nil {
		return nil
	}
	_ = m.cmd.Process.Kill()
	_, err := m.cmd.Process.Wait()
	if m.log != nil {
		name := m.log.Name()
		_ = m.log.Close()
		if filepath.Base(name) != "" {
			_ = os.Remove(name)
		}
	}
	if err != nil && strings.Contains(err.Error(), "waitid: no child processes") {
		return nil
	}
	return nil
}

func (m *ManagedEmbedder) waitReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if _, err := m.Embed(ctx, "localclaw readiness"); err == nil {
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(100 * time.Millisecond)
	}
	if lastErr != nil {
		return fmt.Errorf("llama-server did not become ready: %w", lastErr)
	}
	return errors.New("llama-server did not become ready")
}

func (e *EmbeddingClient) embedOpenAI(ctx context.Context, input string) ([]float32, error) {
	payload := map[string]interface{}{"input": input}
	var response struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := e.postJSON(ctx, "/v1/embeddings", payload, &response); err != nil {
		return nil, err
	}
	if len(response.Data) == 0 {
		return nil, errors.New("embedding response has no data")
	}
	return float64sToFloat32s(response.Data[0].Embedding), nil
}

func (e *EmbeddingClient) embedLlama(ctx context.Context, input string) ([]float32, error) {
	payload := map[string]interface{}{"input": input}
	var response struct {
		Embedding []float64 `json:"embedding"`
		Data      []float64 `json:"data"`
	}
	if err := e.postJSON(ctx, "/embedding", payload, &response); err != nil {
		return nil, err
	}
	if len(response.Embedding) > 0 {
		return float64sToFloat32s(response.Embedding), nil
	}
	if len(response.Data) > 0 {
		return float64sToFloat32s(response.Data), nil
	}
	return nil, errors.New("embedding response has no vector")
}

func (e *EmbeddingClient) postJSON(ctx context.Context, path string, payload interface{}, out interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(e.baseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("status %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func freeLoopbackPort(host string) (int, error) {
	listener, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.New("unexpected listener address type")
	}
	return addr.Port, nil
}

func float64sToFloat32s(values []float64) []float32 {
	out := make([]float32, len(values))
	for i, value := range values {
		out[i] = float32(value)
	}
	return out
}
