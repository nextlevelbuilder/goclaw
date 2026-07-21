package memory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func makeEmbedding(dim int) []float32 {
	vec := make([]float32, dim)
	for i := range vec {
		vec[i] = float32(i)
	}
	return vec
}

func TestOpenAIEmbedding_LocalTruncationWithoutDimensionsRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if _, ok := reqBody["dimensions"]; ok {
			t.Fatalf("request must not include dimensions field: %v", reqBody)
		}

		inputs, ok := reqBody["input"].([]any)
		if !ok || len(inputs) != 1 {
			t.Fatalf("unexpected input payload: %v", reqBody["input"])
		}

		resp := struct {
			Data []struct {
				Embedding []float32 `json:"embedding"`
			} `json:"data"`
		}{
			Data: []struct {
				Embedding []float32 `json:"embedding"`
			}{{Embedding: makeEmbedding(3072)}},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	provider := NewOpenAIEmbeddingProvider("vllm", "test-key", server.URL, "gemma-2b-embeddings")
	provider.WithDimensions(1536)

	embeddings, err := provider.Embed(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if len(embeddings) != 1 {
		t.Fatalf("expected 1 embedding, got %d", len(embeddings))
	}
	if len(embeddings[0]) != 1536 {
		t.Fatalf("expected truncated embedding length 1536, got %d", len(embeddings[0]))
	}
	if embeddings[0][1535] != 1535 {
		t.Fatalf("unexpected truncation result: last value = %v", embeddings[0][1535])
	}
}

func TestOpenAIEmbedding_LocalTruncationErrorsWhenVectorTooShort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := struct {
			Data []struct {
				Embedding []float32 `json:"embedding"`
			} `json:"data"`
		}{
			Data: []struct {
				Embedding []float32 `json:"embedding"`
			}{{Embedding: makeEmbedding(1024)}},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	provider := NewOpenAIEmbeddingProvider("vllm", "test-key", server.URL, "gemma-2b-embeddings")
	provider.WithDimensions(1536)

	_, err := provider.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error for too-short embedding, got nil")
	}
	if !strings.Contains(err.Error(), "returned 1024") {
		t.Fatalf("expected dimension count in error, got %v", err)
	}
	if !strings.Contains(err.Error(), "1536") {
		t.Fatalf("expected target dimension in error, got %v", err)
	}
}
