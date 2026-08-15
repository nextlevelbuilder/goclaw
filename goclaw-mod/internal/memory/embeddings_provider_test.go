package memory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOpenAIEmbeddingProviderRestoresResponseOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"index": 1, "embedding": []float32{2, 2, 2}},
				{"index": 0, "embedding": []float32{1, 1, 1}},
			},
		})
	}))
	defer server.Close()

	provider := NewOpenAIEmbeddingProvider("test", "key", server.URL, "model").WithDimensions(3)
	embeddings, err := provider.Embed(context.Background(), []string{"first", "second"})
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if embeddings[0][0] != 1 || embeddings[1][0] != 2 {
		t.Fatalf("embeddings returned in response order: %v", embeddings)
	}
}

func TestOpenAIEmbeddingProviderRejectsWrongDimension(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[1,2]}]}`))
	}))
	defer server.Close()

	provider := NewOpenAIEmbeddingProvider("test", "key", server.URL, "model").WithDimensions(3)
	if _, err := provider.Embed(context.Background(), []string{"text"}); err == nil {
		t.Fatal("Embed() error = nil, want dimension validation error")
	}
}

func TestOpenAIEmbeddingProviderUsesBoundedHTTPClient(t *testing.T) {
	provider := NewOpenAIEmbeddingProvider("test", "key", "http://example.invalid", "model")
	if provider.httpClient == nil || provider.httpClient.Timeout != 60*time.Second {
		t.Fatalf("HTTP timeout = %v, want 60s", provider.httpClient)
	}
}
