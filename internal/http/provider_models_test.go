package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ollamaTagsHandler returns an httptest.Handler that serves /api/tags with the
// given response body. Any other path returns 404.
func ollamaTagsHandler(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

// newOllamaProvider creates a ProviderOllama entry pointing at the given apiBase.
func newOllamaProvider(apiBase string) *store.LLMProviderData {
	return &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		Name:         "local-ollama",
		ProviderType: store.ProviderOllama,
		APIBase:      apiBase,
		Enabled:      true,
	}
}

// ollamaModelsRequest fires GET /v1/providers/{id}/models through mux and returns
// the decoded ProviderModelsResponse and the HTTP status code.
func ollamaModelsRequest(t *testing.T, mux *http.ServeMux, providerID uuid.UUID, token string) (ProviderModelsResponse, int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/providers/"+providerID.String()+"/models", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		return ProviderModelsResponse{}, w.Code
	}
	var result ProviderModelsResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	return result, w.Code
}

func providerModelsRequest(t *testing.T, mux *http.ServeMux, providerID uuid.UUID, token string) ProviderModelsResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/providers/"+providerID.String()+"/models", nil)
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
	return result
}

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
		if model.ID != "gpt-5.5" {
			continue
		}
		found = true
		if model.Reasoning == nil {
			t.Fatal("gpt-5.5 reasoning = nil, want capability metadata")
		}
		if model.Reasoning.DefaultEffort != "medium" {
			t.Fatalf("gpt-5.5 default_effort = %q, want medium", model.Reasoning.DefaultEffort)
		}
		if got := model.Reasoning.Levels; len(got) != 5 || got[4] != "xhigh" {
			t.Fatalf("gpt-5.5 levels = %#v, want none..xhigh", got)
		}
	}
	if !found {
		t.Fatal("gpt-5.5 not found in ChatGPT OAuth model list")
	}
}

func TestProvidersHandlerListProviderModelsBailianIncludesQwen37Plus(t *testing.T) {
	token := setupProvidersAdminToken(t)
	providerStore := newMockProviderStore()
	provider := &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		Name:         "bailian-coding",
		ProviderType: store.ProviderBailian,
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

	for _, model := range result.Models {
		if model.ID != "qwen3.7-plus" {
			continue
		}
		if model.Name != "Qwen 3.7 Plus" {
			t.Fatalf("model name = %q, want Qwen 3.7 Plus", model.Name)
		}
		if model.Reasoning != nil {
			t.Fatalf("model reasoning = %#v, want nil for Bailian OpenAI-compatible catalog entry", model.Reasoning)
		}
		return
	}

	t.Fatalf("qwen3.7-plus not found in Bailian model list: %#v", result.Models)
}

func TestProvidersHandlerListProviderModelsMiniMaxIncludesM3AndLegacyModels(t *testing.T) {
	token := setupProvidersAdminToken(t)
	providerStore := newMockProviderStore()
	provider := &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		Name:         "minimax",
		ProviderType: store.ProviderMiniMax,
		APIKey:       "token",
		Enabled:      true,
	}
	if err := providerStore.CreateProvider(t.Context(), provider); err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}

	handler := NewProvidersHandler(providerStore, newMockSecretsStore(), nil, "")
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	result := providerModelsRequest(t, mux, provider.ID, token)
	assertModelIDsInOrder(t, result.Models, []string{
		"MiniMax-M3",
		"MiniMax-M2.7",
		"MiniMax-M2.5",
		"MiniMax-M2.1",
		"MiniMax-M2",
	})
}

func TestProvidersHandlerListProviderModelsZaiUsesLocalCatalog(t *testing.T) {
	token := setupProvidersAdminToken(t)

	for _, providerType := range []string{store.ProviderZai, store.ProviderZaiCoding} {
		t.Run(providerType, func(t *testing.T) {
			providerStore := newMockProviderStore()
			provider := &store.LLMProviderData{
				BaseModel:    store.BaseModel{ID: uuid.New()},
				Name:         providerType,
				ProviderType: providerType,
				APIKey:       "token",
				Enabled:      true,
			}
			if err := providerStore.CreateProvider(t.Context(), provider); err != nil {
				t.Fatalf("CreateProvider() error = %v", err)
			}

			handler := NewProvidersHandler(providerStore, newMockSecretsStore(), nil, "")
			mux := http.NewServeMux()
			handler.RegisterRoutes(mux)

			result := providerModelsRequest(t, mux, provider.ID, token)
			assertModelIDsInOrder(t, result.Models, []string{
				"glm-5.2",
				"glm-5.1",
				"glm-5",
				"glm-4.7",
				"glm-4.5",
			})
		})
	}
}

func assertModelIDsInOrder(t *testing.T, models []ModelInfo, want []string) {
	t.Helper()
	position := 0
	for _, model := range models {
		if position < len(want) && model.ID == want[position] {
			position++
		}
	}
	if position != len(want) {
		got := make([]string, 0, len(models))
		for _, model := range models {
			got = append(got, model.ID)
		}
		t.Fatalf("model IDs = %#v, want sequence %#v", got, want)
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

func TestProvidersHandlerListProviderModelsKimiCodingSendsRequiredUserAgent(t *testing.T) {
	token := setupProvidersAdminToken(t)
	var capturedAuth, capturedUserAgent string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		capturedUserAgent = r.Header.Get("User-Agent")
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{
				{"id": store.KimiCodingDefaultModel},
			},
		})
	}))
	t.Cleanup(upstream.Close)

	providerStore := newMockProviderStore()
	provider := &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		Name:         "kimi-coding",
		ProviderType: store.ProviderKimiCoding,
		APIBase:      upstream.URL,
		APIKey:       "kimi-key",
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
	if capturedAuth != "Bearer kimi-key" {
		t.Fatalf("Authorization = %q, want Bearer kimi-key", capturedAuth)
	}
	if capturedUserAgent != store.KimiCodingRequiredUserAgent {
		t.Fatalf("User-Agent = %q, want %q", capturedUserAgent, store.KimiCodingRequiredUserAgent)
	}
	var result ProviderModelsResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(result.Models) != 1 || result.Models[0].ID != store.KimiCodingDefaultModel {
		t.Fatalf("models = %#v, want %q", result.Models, store.KimiCodingDefaultModel)
	}
}

func TestOpenAIModelsAPIBaseDefaultsKimiCoding(t *testing.T) {
	if got := openAIModelsAPIBase(store.ProviderKimiCoding, ""); got != store.KimiCodingDefaultAPIBase {
		t.Fatalf("Kimi default api base = %q, want %q", got, store.KimiCodingDefaultAPIBase)
	}
	if got := openAIModelsAPIBase(store.ProviderOpenAICompat, ""); got != "https://api.openai.com/v1" {
		t.Fatalf("OpenAI compat default api base = %q", got)
	}
}

func TestProvidersHandlerListProviderModelsAIMLAPIUsesCuratedCatalog(t *testing.T) {
	token := setupProvidersAdminToken(t)
	providerStore := newMockProviderStore()
	provider := &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		Name:         "aimlapi",
		ProviderType: store.ProviderAIMLAPI,
		APIBase:      "http://127.0.0.1:1",
		APIKey:       "aimlapi-key",
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
	want := providers.AIMLAPIChatModels()
	if len(result.Models) != len(want) {
		t.Fatalf("models = %#v, want %d curated models", result.Models, len(want))
	}
	for index, model := range result.Models {
		if model.ID != want[index] {
			t.Errorf("models[%d].ID = %q, want %q", index, model.ID, want[index])
		}
	}
}

// TestProvidersHandlerListProviderModelsOllamaRichMetadata verifies that the
// handler fetches /api/tags from Ollama and maps rich details (family,
// parameter_size, quantization_level) into the display name.
func TestProvidersHandlerListProviderModelsOllamaRichMetadata(t *testing.T) {
	token := setupProvidersAdminToken(t)

	upstream := httptest.NewServer(ollamaTagsHandler(`{
		"models": [
			{
				"name": "gemma4:8b-it-q4_K_M",
				"details": {
					"family": "gemma4",
					"parameter_size": "8.0B",
					"quantization_level": "Q4_K_M"
				}
			},
			{
				"name": "llama3.2:3b",
				"details": {
					"family": "llama",
					"parameter_size": "3.2B",
					"quantization_level": "F16"
				}
			}
		]
	}`))
	t.Cleanup(upstream.Close)

	providerStore := newMockProviderStore()
	provider := newOllamaProvider(upstream.URL)
	if err := providerStore.CreateProvider(t.Context(), provider); err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}

	handler := NewProvidersHandler(providerStore, newMockSecretsStore(), nil, "")
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	result, code := ollamaModelsRequest(t, mux, provider.ID, token)
	if code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", code, http.StatusOK)
	}
	if len(result.Models) != 2 {
		t.Fatalf("models len = %d, want 2", len(result.Models))
	}

	// First model: ID is the raw Ollama name, Name is built from rich details.
	m0 := result.Models[0]
	if m0.ID != "gemma4:8b-it-q4_K_M" {
		t.Errorf("models[0].id = %q, want gemma4:8b-it-q4_K_M", m0.ID)
	}
	if m0.Name != "gemma4 8.0B Q4_K_M" {
		t.Errorf("models[0].name = %q, want \"gemma4 8.0B Q4_K_M\"", m0.Name)
	}

	// Second model
	m1 := result.Models[1]
	if m1.ID != "llama3.2:3b" {
		t.Errorf("models[1].id = %q, want llama3.2:3b", m1.ID)
	}
	if m1.Name != "llama 3.2B F16" {
		t.Errorf("models[1].name = %q, want \"llama 3.2B F16\"", m1.Name)
	}

	// Ollama models carry no reasoning metadata.
	if result.ReasoningDefaults != nil {
		t.Errorf("reasoning_defaults = %#v, want nil for Ollama provider", result.ReasoningDefaults)
	}
}

// TestProvidersHandlerListProviderModelsOllamaStripsV1Suffix verifies that a
// /v1-suffixed api_base (from issue #654 normalization) is correctly stripped
// so the request hits /api/tags at the root, not /v1/api/tags.
func TestProvidersHandlerListProviderModelsOllamaStripsV1Suffix(t *testing.T) {
	token := setupProvidersAdminToken(t)

	var capturedPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models": [{"name": "phi4:latest", "details": {"family": "phi4", "parameter_size": "14B", "quantization_level": "Q4_K_M"}}]}`))
	}))
	t.Cleanup(upstream.Close)

	providerStore := newMockProviderStore()
	// APIBase has /v1 suffix — as would be saved after issue #654 normalization.
	provider := newOllamaProvider(upstream.URL + "/v1")
	if err := providerStore.CreateProvider(t.Context(), provider); err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}

	handler := NewProvidersHandler(providerStore, newMockSecretsStore(), nil, "")
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	result, code := ollamaModelsRequest(t, mux, provider.ID, token)
	if code != http.StatusOK {
		t.Fatalf("status code = %d, want %d (body may be empty if path not stripped)", code, http.StatusOK)
	}
	if capturedPath != "/api/tags" {
		t.Errorf("upstream received path %q, want /api/tags — /v1 suffix was not stripped", capturedPath)
	}
	if len(result.Models) != 1 {
		t.Fatalf("models len = %d, want 1", len(result.Models))
	}
	if result.Models[0].ID != "phi4:latest" {
		t.Errorf("models[0].id = %q, want phi4:latest", result.Models[0].ID)
	}
}

// TestProvidersHandlerListProviderModelsOllamaNoV1Suffix verifies that a plain
// api_base without a /v1 suffix also hits /api/tags correctly.
func TestProvidersHandlerListProviderModelsOllamaNoV1Suffix(t *testing.T) {
	token := setupProvidersAdminToken(t)

	upstream := httptest.NewServer(ollamaTagsHandler(`{"models": [{"name": "mistral:latest", "details": {"family": "mistral", "parameter_size": "7B", "quantization_level": "Q4_0"}}]}`))
	t.Cleanup(upstream.Close)

	providerStore := newMockProviderStore()
	provider := newOllamaProvider(upstream.URL) // no /v1 suffix
	if err := providerStore.CreateProvider(t.Context(), provider); err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}

	handler := NewProvidersHandler(providerStore, newMockSecretsStore(), nil, "")
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	result, code := ollamaModelsRequest(t, mux, provider.ID, token)
	if code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", code, http.StatusOK)
	}
	if len(result.Models) != 1 {
		t.Fatalf("models len = %d, want 1", len(result.Models))
	}
	if result.Models[0].Name != "mistral 7B Q4_0" {
		t.Errorf("models[0].name = %q, want \"mistral 7B Q4_0\"", result.Models[0].Name)
	}
}

// TestProvidersHandlerListProviderModelsOllamaFallbackOn404 verifies that a
// non-200 response from /api/tags causes the handler to return an empty model
// list with HTTP 200 (graceful fallback, not a 5xx to the UI).
func TestProvidersHandlerListProviderModelsOllamaFallbackOn404(t *testing.T) {
	token := setupProvidersAdminToken(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r) // Ollama /api/tags returns 404
	}))
	t.Cleanup(upstream.Close)

	providerStore := newMockProviderStore()
	provider := newOllamaProvider(upstream.URL)
	if err := providerStore.CreateProvider(t.Context(), provider); err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}

	handler := NewProvidersHandler(providerStore, newMockSecretsStore(), nil, "")
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	result, code := ollamaModelsRequest(t, mux, provider.ID, token)
	if code != http.StatusOK {
		t.Fatalf("status code = %d, want %d — error must not bubble to UI", code, http.StatusOK)
	}
	if len(result.Models) != 0 {
		t.Errorf("models len = %d, want 0 on fallback", len(result.Models))
	}
}

// TestProvidersHandlerListProviderModelsOllamaEmptyList verifies that Ollama
// returning {"models": []} results in an empty model list without panic.
func TestProvidersHandlerListProviderModelsOllamaEmptyList(t *testing.T) {
	token := setupProvidersAdminToken(t)

	upstream := httptest.NewServer(ollamaTagsHandler(`{"models": []}`))
	t.Cleanup(upstream.Close)

	providerStore := newMockProviderStore()
	provider := newOllamaProvider(upstream.URL)
	if err := providerStore.CreateProvider(t.Context(), provider); err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}

	handler := NewProvidersHandler(providerStore, newMockSecretsStore(), nil, "")
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	result, code := ollamaModelsRequest(t, mux, provider.ID, token)
	if code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", code, http.StatusOK)
	}
	if len(result.Models) != 0 {
		t.Errorf("models len = %d, want 0 for empty Ollama list", len(result.Models))
	}
}

// TestProvidersHandlerListProviderModelsOllamaPartialDetails verifies that
// models with only some detail fields set still produce a reasonable display
// name (omit missing parts rather than emitting empty tokens).
func TestProvidersHandlerListProviderModelsOllamaPartialDetails(t *testing.T) {
	token := setupProvidersAdminToken(t)

	upstream := httptest.NewServer(ollamaTagsHandler(`{
		"models": [
			{
				"name": "nodetails:latest",
				"details": {}
			},
			{
				"name": "familyonly:latest",
				"details": {"family": "llama"}
			},
			{
				"name": "sizequant:latest",
				"details": {"parameter_size": "7B", "quantization_level": "Q8_0"}
			}
		]
	}`))
	t.Cleanup(upstream.Close)

	providerStore := newMockProviderStore()
	provider := newOllamaProvider(upstream.URL)
	if err := providerStore.CreateProvider(t.Context(), provider); err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}

	handler := NewProvidersHandler(providerStore, newMockSecretsStore(), nil, "")
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	result, code := ollamaModelsRequest(t, mux, provider.ID, token)
	if code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", code, http.StatusOK)
	}
	if len(result.Models) != 3 {
		t.Fatalf("models len = %d, want 3", len(result.Models))
	}

	// No details: name falls back to the raw model name.
	if result.Models[0].Name != "nodetails:latest" {
		t.Errorf("models[0].name = %q, want nodetails:latest (fallback to ID)", result.Models[0].Name)
	}
	// Family only: display name is just the family.
	if result.Models[1].Name != "llama" {
		t.Errorf("models[1].name = %q, want llama", result.Models[1].Name)
	}
	// Size + quantization (no family): display name omits the empty family.
	if result.Models[2].Name != "7B Q8_0" {
		t.Errorf("models[2].name = %q, want \"7B Q8_0\"", result.Models[2].Name)
	}
}

// TestProvidersHandlerListProviderModelsOllamaCloudSendsAuthHeader verifies
// that ProviderOllamaCloud providers forward their API key as a Bearer token.
func TestProvidersHandlerListProviderModelsOllamaCloudSendsAuthHeader(t *testing.T) {
	token := setupProvidersAdminToken(t)

	var capturedAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models": [{"name": "cloud-model:latest", "details": {"family": "cloud"}}]}`))
	}))
	t.Cleanup(upstream.Close)

	providerStore := newMockProviderStore()
	provider := &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		Name:         "ollama-cloud",
		ProviderType: store.ProviderOllamaCloud,
		APIBase:      upstream.URL,
		APIKey:       "cloud-secret-key",
		Enabled:      true,
	}
	if err := providerStore.CreateProvider(t.Context(), provider); err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}

	handler := NewProvidersHandler(providerStore, newMockSecretsStore(), nil, "")
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	result, code := ollamaModelsRequest(t, mux, provider.ID, token)
	if code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", code, http.StatusOK)
	}
	if capturedAuth != "Bearer cloud-secret-key" {
		t.Errorf("Authorization header = %q, want \"Bearer cloud-secret-key\"", capturedAuth)
	}
	if len(result.Models) != 1 || result.Models[0].ID != "cloud-model:latest" {
		t.Errorf("models = %#v, want [{ID: cloud-model:latest}]", result.Models)
	}
}
