package memory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestHTTPEmbedderParsesOpenAIEmbeddings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{{"embedding": []float64{3, 4}}},
		})
	}))
	defer server.Close()

	vector, err := NewHTTPEmbedder(server.URL).Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vector) != 2 || vector[0] <= 0 || vector[1] <= 0 {
		t.Fatalf("unexpected vector: %+v", vector)
	}
}

func TestLlamaServerArgsUseLoopbackHostAndEmbeddings(t *testing.T) {
	args := llamaServerArgs("/tmp/model.gguf", "127.0.0.1", 12345)
	for _, want := range []string{"-m", "/tmp/model.gguf", "--embeddings", "--host", "127.0.0.1", "--port", "12345"} {
		if !slices.Contains(args, want) {
			t.Fatalf("expected args to contain %q, got %v", want, args)
		}
	}
}
