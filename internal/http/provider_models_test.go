package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestProvidersHandlerListProviderModelsChatGPTOAuthIncludesReasoningMetadata(t *testing.T) {
	token := setupProvidersAdminToken(t)
	providerStore := newMockProviderStore()
	provider := &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		Name:         "openai-codex",
		ProviderType: store.ProviderChatGPTOAuth,
		Enabled:      true,
		Settings: json.RawMessage(`{
			"reasoning_defaults": {"effort": "high"}
		}`),
	}
	if err := providerStore.CreateProvider(t.Context(), provider); err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}

	handler := NewProvidersHandler(providerStore, newMockSecretsStore(), nil, "")
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/providers/"+provider.ID.String()+"/models", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}

	var result ProviderModelsResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(result.Models) == 0 {
		t.Fatal("models = empty, want hardcoded ChatGPT OAuth list")
	}
	if result.ReasoningDefaults == nil {
		t.Fatal("reasoning_defaults = nil, want provider defaults")
	}
	if result.ReasoningDefaults.Effort != "high" {
		t.Fatalf("reasoning_defaults.effort = %q, want high", result.ReasoningDefaults.Effort)
	}

	var found bool
	for _, model := range result.Models {
		if model.ID != "gpt-5.4" {
			continue
		}
		found = true
		if model.Reasoning == nil {
			t.Fatal("gpt-5.4 reasoning = nil, want capability metadata")
		}
		if model.Reasoning.DefaultEffort != "none" {
			t.Fatalf("gpt-5.4 default_effort = %q, want none", model.Reasoning.DefaultEffort)
		}
		if got := model.Reasoning.Levels; len(got) != 5 || got[4] != "xhigh" {
			t.Fatalf("gpt-5.4 levels = %#v, want none..xhigh", got)
		}
	}
	if !found {
		t.Fatal("gpt-5.4 not found in ChatGPT OAuth model list")
	}
}

func TestProvidersHandlerListProviderModelsOpenAICompatAnnotatesKnownModels(t *testing.T) {
	token := setupProvidersAdminToken(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{
				{"id": "gpt-5.1-codex-max"},
				{"id": "gpt-5.4-experimental"},
			},
		})
	}))
	t.Cleanup(upstream.Close)

	providerStore := newMockProviderStore()
	provider := &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		Name:         "openai",
		ProviderType: store.ProviderOpenAICompat,
		APIBase:      upstream.URL,
		APIKey:       "token",
		Enabled:      true,
	}
	if err := providerStore.CreateProvider(t.Context(), provider); err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}

	handler := NewProvidersHandler(providerStore, newMockSecretsStore(), nil, "")
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/providers/"+provider.ID.String()+"/models", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}

	var result ProviderModelsResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(result.Models) != 2 {
		t.Fatalf("models len = %d, want 2", len(result.Models))
	}
	if result.Models[0].Reasoning == nil {
		t.Fatal("known GPT-5.1 Codex Max reasoning = nil, want capability metadata")
	}
	if result.Models[0].Reasoning.DefaultEffort != "none" {
		t.Fatalf("known model default_effort = %q, want none", result.Models[0].Reasoning.DefaultEffort)
	}
	if result.Models[1].Reasoning != nil {
		t.Fatalf("unknown GPT-5 variant reasoning = %#v, want nil", result.Models[1].Reasoning)
	}
	if result.ReasoningDefaults != nil {
		t.Fatalf("reasoning_defaults = %#v, want nil when provider has no saved defaults", result.ReasoningDefaults)
	}
}
