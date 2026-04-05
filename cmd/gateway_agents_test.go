package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func captureEmbeddingRequest(t *testing.T, es *store.EmbeddingSettings) map[string]any {
	t.Helper()

	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2]}]}`))
	}))
	defer server.Close()

	provider := &store.LLMProviderData{
		Name:         "embedding-provider",
		ProviderType: store.ProviderOpenAICompat,
		APIKey:       "test-key",
		APIBase:      server.URL,
		Enabled:      true,
	}

	ep := buildEmbeddingProvider(provider, es, nil, nil)
	if ep == nil {
		t.Fatal("buildEmbeddingProvider() = nil, want provider")
	}
	if _, err := ep.Embed(context.Background(), []string{"hello"}); err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	return requestBody
}

func TestBuildEmbeddingProviderDefaultsTo1536Dimensions(t *testing.T) {
	requestBody := captureEmbeddingRequest(t, nil)
	if got := requestBody["dimensions"]; got != float64(1536) {
		t.Fatalf("dimensions = %v, want 1536", got)
	}
}

func TestBuildEmbeddingProviderIgnoresIncompatibleStoredDimensions(t *testing.T) {
	requestBody := captureEmbeddingRequest(t, &store.EmbeddingSettings{
		Enabled:    true,
		Model:      "voyage-4-nano",
		Dimensions: 2048,
	})
	if got := requestBody["dimensions"]; got != float64(1536) {
		t.Fatalf("dimensions = %v, want fallback 1536", got)
	}
}
