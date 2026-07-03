package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAIMLAPIProviderSendsAttributionHeaders(t *testing.T) {
	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]string{"content": "ok"},
				"finish_reason": "stop",
			}},
		})
	}))
	t.Cleanup(server.Close)

	provider := NewAIMLAPIProvider("aimlapi", "test-key", server.URL)
	if _, err := provider.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	}); err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	checks := map[string]string{
		"Authorization":                 "Bearer test-key",
		"X-AIMLAPI-Partner-ID":          "nextlevelbuilder",
		"X-AIMLAPI-Integration-Repo":    "nextlevelbuilder/goclaw",
		"X-AIMLAPI-Integration-Version": "1.0.0",
	}
	for header, want := range checks {
		if value := got.Get(header); value != want {
			t.Errorf("%s = %q, want %q", header, value, want)
		}
	}
}

func TestAIMLAPIChatModelsReturnsCopy(t *testing.T) {
	models := AIMLAPIChatModels()
	if len(models) == 0 || models[0] != AIMLAPIDefaultModel {
		t.Fatalf("models = %v, want default model first", models)
	}
	models[0] = "changed"
	if got := AIMLAPIChatModels()[0]; got != AIMLAPIDefaultModel {
		t.Fatalf("catalog mutated through returned slice: %q", got)
	}
}
