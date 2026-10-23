package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// The "custom" provider_type is a create/update-time alias for openai_compat
// (#918). These tests pin the HTTP contract: the alias is canonicalized before
// persistence, and URL validation behaves exactly as it does for openai_compat.

func serveProvidersRequest(t *testing.T, mux *http.ServeMux, token, method, path string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	rawBody, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(rawBody))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func newProvidersTestMux(providerStore *mockProviderStore) *http.ServeMux {
	handler := NewProvidersHandler(providerStore, newMockSecretsStore(), nil, "")
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	return mux
}

func TestProvidersHandlerCreateCanonicalizesCustomAlias(t *testing.T) {
	token := setupProvidersAdminToken(t)
	saveAndRestoreGlobals(t)
	allowPrivateProviderURLsFn = func() bool { return false }
	dnsResolverFn = stubResolver(map[string][]string{"llm.example.com": {"203.0.113.50"}})
	providerStore := newMockProviderStore()
	mux := newProvidersTestMux(providerStore)

	w := serveProvidersRequest(t, mux, token, http.MethodPost, "/v1/providers", map[string]any{
		"name":          "my-gateway",
		"provider_type": "custom",
		"api_base":      "https://llm.example.com/v1",
		"api_key":       "sk-test",
		"enabled":       true,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("status code = %d, want %d, body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	saved, ok := providerStore.providers["my-gateway"]
	if !ok {
		t.Fatal("provider was not stored")
	}
	if saved.ProviderType != store.ProviderOpenAICompat {
		t.Fatalf("stored provider_type = %q, want %q", saved.ProviderType, store.ProviderOpenAICompat)
	}
}

func TestProvidersHandlerUpdateCanonicalizesCustomAlias(t *testing.T) {
	token := setupProvidersAdminToken(t)
	saveAndRestoreGlobals(t)
	allowPrivateProviderURLsFn = func() bool { return false }
	dnsResolverFn = stubResolver(map[string][]string{"llm.example.com": {"203.0.113.50"}})
	providerStore := newMockProviderStore()
	provider := &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		Name:         "my-gateway",
		ProviderType: store.ProviderOpenRouter,
		APIBase:      "https://openrouter.ai/api/v1",
		Enabled:      true,
	}
	if err := providerStore.CreateProvider(context.Background(), provider); err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}
	mux := newProvidersTestMux(providerStore)

	w := serveProvidersRequest(t, mux, token, http.MethodPut, "/v1/providers/"+provider.ID.String(), map[string]any{
		"provider_type": "custom",
		"api_base":      "https://llm.example.com/v1",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	current, err := providerStore.GetProvider(context.Background(), provider.ID)
	if err != nil {
		t.Fatalf("GetProvider() error = %v", err)
	}
	if current.ProviderType != store.ProviderOpenAICompat {
		t.Fatalf("stored provider_type = %q, want %q", current.ProviderType, store.ProviderOpenAICompat)
	}
}

// The alias must not loosen or tighten URL validation: for every base URL the
// create and update handlers answer "custom" exactly as they answer openai_compat.
func TestProvidersHandlerCustomAliasKeepsURLValidation(t *testing.T) {
	token := setupProvidersAdminToken(t)
	saveAndRestoreGlobals(t)
	allowPrivateProviderURLsFn = func() bool { return false }
	dnsResolverFn = stubResolver(map[string][]string{"llm.example.com": {"203.0.113.50"}})
	urls := []string{
		"https://llm.example.com/v1",
		"http://169.254.169.254/latest/meta-data",
		"ftp://llm.example.com/v1",
	}
	for _, apiBase := range urls {
		createCode := func(providerType string) int {
			mux := newProvidersTestMux(newMockProviderStore())
			return serveProvidersRequest(t, mux, token, http.MethodPost, "/v1/providers", map[string]any{
				"name":          "gw",
				"provider_type": providerType,
				"api_base":      apiBase,
				"api_key":       "sk-test",
			}).Code
		}
		updateCode := func(providerType string) int {
			providerStore := newMockProviderStore()
			provider := &store.LLMProviderData{
				BaseModel:    store.BaseModel{ID: uuid.New()},
				Name:         "gw",
				ProviderType: store.ProviderOpenAICompat,
				APIBase:      "https://llm.example.com/v1",
				Enabled:      true,
			}
			if err := providerStore.CreateProvider(context.Background(), provider); err != nil {
				t.Fatalf("CreateProvider() error = %v", err)
			}
			mux := newProvidersTestMux(providerStore)
			return serveProvidersRequest(t, mux, token, http.MethodPut, "/v1/providers/"+provider.ID.String(), map[string]any{
				"provider_type": providerType,
				"api_base":      apiBase,
			}).Code
		}
		if got, want := createCode("custom"), createCode(store.ProviderOpenAICompat); got != want {
			t.Errorf("create api_base=%q: custom status = %d, openai_compat status = %d", apiBase, got, want)
		}
		if got, want := updateCode("custom"), updateCode(store.ProviderOpenAICompat); got != want {
			t.Errorf("update api_base=%q: custom status = %d, openai_compat status = %d", apiBase, got, want)
		}
	}
}
