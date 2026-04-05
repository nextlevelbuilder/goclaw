package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/oauth"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// --- mock stores for tests ---

type mockProviderStore struct {
	providers map[string]*store.LLMProviderData
}

func newMockProviderStore() *mockProviderStore {
	return &mockProviderStore{providers: make(map[string]*store.LLMProviderData)}
}

func (m *mockProviderStore) CreateProvider(_ context.Context, p *store.LLMProviderData) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	m.providers[p.Name] = p
	return nil
}

func (m *mockProviderStore) GetProvider(_ context.Context, id uuid.UUID) (*store.LLMProviderData, error) {
	for _, p := range m.providers {
		if p.ID == id {
			return p, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (m *mockProviderStore) GetProviderByName(_ context.Context, name string) (*store.LLMProviderData, error) {
	if p, ok := m.providers[name]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("not found")
}

func (m *mockProviderStore) ListProviders(_ context.Context) ([]store.LLMProviderData, error) {
	var out []store.LLMProviderData
	for _, p := range m.providers {
		out = append(out, *p)
	}
	return out, nil
}

func (m *mockProviderStore) UpdateProvider(_ context.Context, id uuid.UUID, updates map[string]any) error {
	for _, p := range m.providers {
		if p.ID == id {
			if v, ok := updates["api_key"]; ok {
				p.APIKey = v.(string)
			}
			if v, ok := updates["settings"]; ok {
				p.Settings = v.(json.RawMessage)
			}
			if v, ok := updates["enabled"]; ok {
				p.Enabled = v.(bool)
			}
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (m *mockProviderStore) DeleteProvider(_ context.Context, id uuid.UUID) error {
	for name, p := range m.providers {
		if p.ID == id {
			delete(m.providers, name)
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (m *mockProviderStore) ListAllProviders(_ context.Context) ([]store.LLMProviderData, error) {
	var out []store.LLMProviderData
	for _, p := range m.providers {
		out = append(out, *p)
	}
	return out, nil
}

type mockSecretsStore struct {
	data map[string]string
}

func newMockSecretsStore() *mockSecretsStore {
	return &mockSecretsStore{data: make(map[string]string)}
}

func (m *mockSecretsStore) Get(_ context.Context, key string) (string, error) {
	if v, ok := m.data[key]; ok {
		return v, nil
	}
	return "", fmt.Errorf("not found: %s", key)
}

func (m *mockSecretsStore) Set(_ context.Context, key, value string) error {
	m.data[key] = value
	return nil
}

func (m *mockSecretsStore) Delete(_ context.Context, key string) error {
	delete(m.data, key)
	return nil
}

func (m *mockSecretsStore) GetAll(_ context.Context) (map[string]string, error) {
	return m.data, nil
}

// --- helper ---

func newTestOAuthHandler(t *testing.T, token string) *OAuthHandler {
	t.Helper()
	old := pkgGatewayToken
	pkgGatewayToken = token
	t.Cleanup(func() { pkgGatewayToken = old })
	return NewOAuthHandler(newMockProviderStore(), newMockSecretsStore(), nil, nil)
}

func newTestOAuthHandlerWithStores(t *testing.T, token string) (*OAuthHandler, *mockProviderStore, *mockSecretsStore) {
	t.Helper()
	old := pkgGatewayToken
	pkgGatewayToken = token
	t.Cleanup(func() { pkgGatewayToken = old })
	provStore := newMockProviderStore()
	secretStore := newMockSecretsStore()
	return NewOAuthHandler(provStore, secretStore, nil, nil), provStore, secretStore
}

// --- tests ---

func TestOAuthHandlerStatusNoToken(t *testing.T) {
	h := newTestOAuthHandler(t, "")
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/v1/auth/openai/status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var result map[string]any
	json.NewDecoder(w.Body).Decode(&result)

	if result["authenticated"] != false {
		t.Errorf("authenticated = %v, want false", result["authenticated"])
	}
}

func TestOAuthHandlerAuth(t *testing.T) {
	h := newTestOAuthHandler(t, "secret-token")
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Without token - should be unauthorized
	req := httptest.NewRequest("GET", "/v1/auth/openai/status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status code without token = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	// With correct token - should work
	req2 := httptest.NewRequest("GET", "/v1/auth/openai/status", nil)
	req2.Header.Set("Authorization", "Bearer secret-token")
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("status code with token = %d, want %d", w2.Code, http.StatusOK)
	}
}

func TestOAuthHandlerSaveAndRegisterAppliesCodexPoolDefaults(t *testing.T) {
	provStore := newMockProviderStore()
	secretStore := newMockSecretsStore()
	providerReg := providers.NewRegistry(nil)
	handler := NewOAuthHandler(provStore, secretStore, providerReg, nil)

	tenantID := uuid.New()
	ctx := store.WithTenantID(context.Background(), tenantID)
	if err := provStore.CreateProvider(ctx, &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		TenantID:     tenantID,
		Name:         oauth.DefaultProviderName,
		ProviderType: store.ProviderChatGPTOAuth,
		APIBase:      oauth.DefaultProviderAPIBase,
		Enabled:      true,
		Settings: json.RawMessage(`{
			"codex_pool": {
				"strategy": "priority_order",
				"extra_provider_names": ["openai-codex-team"]
			}
		}`),
	}); err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	if _, err := handler.saveAndRegister(ctx, oauth.DefaultProviderName, "", "", &oauth.OpenAITokenResponse{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		ExpiresIn:    3600,
	}); err != nil {
		t.Fatalf("saveAndRegister: %v", err)
	}

	runtimeProvider, err := providerReg.GetForTenant(tenantID, oauth.DefaultProviderName)
	if err != nil {
		t.Fatalf("GetForTenant: %v", err)
	}
	codex, ok := runtimeProvider.(*providers.CodexProvider)
	if !ok {
		t.Fatalf("runtime provider = %T, want *providers.CodexProvider", runtimeProvider)
	}
	defaults := codex.RoutingDefaults()
	if defaults == nil {
		t.Fatal("RoutingDefaults() = nil, want defaults")
	}
	if defaults.Strategy != store.ChatGPTOAuthStrategyPriority {
		t.Fatalf("Strategy = %q, want %q", defaults.Strategy, store.ChatGPTOAuthStrategyPriority)
	}
	if len(defaults.ExtraProviderNames) != 1 || defaults.ExtraProviderNames[0] != "openai-codex-team" {
		t.Fatalf("ExtraProviderNames = %#v, want [\"openai-codex-team\"]", defaults.ExtraProviderNames)
	}
}

func TestOAuthHandlerLogoutNoProvider(t *testing.T) {
	h := newTestOAuthHandler(t, "")
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/v1/auth/openai/logout", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var result map[string]string
	json.NewDecoder(w.Body).Decode(&result)

	if result["status"] != "logged out" {
		t.Errorf("status = %q, want 'logged out'", result["status"])
	}
}

func TestOAuthHandlerRouteRegistration(t *testing.T) {
	h := newTestOAuthHandler(t, "")
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	routes := []struct {
		method string
		path   string
	}{
		{"GET", "/v1/auth/openai/status"},
		{"POST", "/v1/auth/openai/logout"},
		{"POST", "/v1/auth/openai/start"},
	}

	for _, r := range routes {
		req := httptest.NewRequest(r.method, r.path, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Errorf("%s %s returned 404", r.method, r.path)
		}
	}
}

func TestOAuthHandlerStartReturnsAuthURL(t *testing.T) {
	h := newTestOAuthHandler(t, "")
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/v1/auth/openai/start", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Skip if port 1455 is already in use (environment issue, not code bug)
	if w.Code == http.StatusInternalServerError {
		t.Skip("port 1455 unavailable, skipping")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var result map[string]any
	json.NewDecoder(w.Body).Decode(&result)

	_, hasURL := result["auth_url"]
	_, hasStatus := result["status"]

	if !hasURL && !hasStatus {
		t.Fatal("response has neither auth_url nor status")
	}
}

func TestOAuthHandlerProviderStatusRoute(t *testing.T) {
	h, provStore, _ := newTestOAuthHandlerWithStores(t, "")
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	if err := provStore.CreateProvider(context.Background(), &store.LLMProviderData{
		Name:         "codex-work",
		DisplayName:  "Codex Work",
		ProviderType: store.ProviderChatGPTOAuth,
		APIBase:      "https://chatgpt.com/backend-api",
		APIKey:       "token-work",
		Enabled:      true,
	}); err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	req := httptest.NewRequest("GET", "/v1/auth/chatgpt/codex-work/status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var result map[string]any
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if result["authenticated"] != true {
		t.Fatalf("authenticated = %v, want true", result["authenticated"])
	}
	if result["provider_name"] != "codex-work" {
		t.Fatalf("provider_name = %v, want codex-work", result["provider_name"])
	}
}

func TestOAuthHandlerProviderStatusRouteRejectsTypeConflict(t *testing.T) {
	h, provStore, _ := newTestOAuthHandlerWithStores(t, "")
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	if err := provStore.CreateProvider(context.Background(), &store.LLMProviderData{
		Name:         "codex-work",
		DisplayName:  "Codex Work",
		ProviderType: store.ProviderOpenRouter,
		APIBase:      "https://openrouter.ai/api/v1",
		APIKey:       "sk-live",
		Enabled:      true,
	}); err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	req := httptest.NewRequest("GET", "/v1/auth/chatgpt/codex-work/status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestOAuthHandlerProviderLogoutRoute(t *testing.T) {
	h, provStore, secretStore := newTestOAuthHandlerWithStores(t, "")
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	if err := provStore.CreateProvider(context.Background(), &store.LLMProviderData{
		Name:         "codex-work",
		DisplayName:  "Codex Work",
		ProviderType: store.ProviderChatGPTOAuth,
		APIBase:      "https://chatgpt.com/backend-api",
		APIKey:       "token-work",
		Enabled:      true,
	}); err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}
	if err := secretStore.Set(context.Background(), oauth.RefreshTokenSecretKey("codex-work"), "refresh-work"); err != nil {
		t.Fatalf("Set refresh token: %v", err)
	}

	req := httptest.NewRequest("POST", "/v1/auth/chatgpt/codex-work/logout", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusOK)
	}
	if _, err := provStore.GetProviderByName(context.Background(), "codex-work"); err == nil {
		t.Fatal("provider still exists after logout")
	}
	if _, err := secretStore.Get(context.Background(), oauth.RefreshTokenSecretKey("codex-work")); err == nil {
		t.Fatal("refresh token still exists after logout")
	}
}

func TestProvidersHandlerRequiresAdmin(t *testing.T) {
	token := "operator-key"
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(token): {
			ID:       uuid.New(),
			Scopes:   []string{"operator.write"},
			TenantID: store.MasterTenantID,
		},
	})

	h := NewProvidersHandler(newMockProviderStore(), newMockSecretsStore(), nil, "")
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/v1/providers", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestOAuthHandlerRequiresAdmin(t *testing.T) {
	token := "operator-key"
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(token): {
			ID:       uuid.New(),
			Scopes:   []string{"operator.write"},
			TenantID: store.MasterTenantID,
		},
	})
	h := newTestOAuthHandler(t, "")
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/v1/auth/openai/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestOAuthHandlerStartReusesPendingFlowForSameRequester(t *testing.T) {
	h := newTestOAuthHandler(t, "")
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req1 := httptest.NewRequest("POST", "/v1/auth/chatgpt/codex-work/start", nil)
	w1 := httptest.NewRecorder()
	mux.ServeHTTP(w1, req1)

	if w1.Code == http.StatusInternalServerError {
		t.Skip("port 1455 unavailable, skipping")
	}
	if w1.Code != http.StatusOK {
		t.Fatalf("first start status = %d, want %d", w1.Code, http.StatusOK)
	}
	key := oauthFlowKey(req1.Context(), "codex-work")
	firstPending := h.pending[key]
	if firstPending == nil {
		t.Fatal("pending flow = nil after first start")
	}
	defer func() {
		firstPending.cancel()
		firstPending.login.Shutdown()
	}()

	req2 := httptest.NewRequest("POST", "/v1/auth/chatgpt/codex-work/start", nil)
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("second start status = %d, want %d", w2.Code, http.StatusOK)
	}
	if h.pending[key] == nil {
		t.Fatal("pending flow = nil after second start")
	}
	if h.pending[key] != firstPending {
		t.Fatal("pending flow should be reused for the same requester")
	}
}

func TestOAuthHandlerStartRejectsConcurrentFlowForDifferentRequester(t *testing.T) {
	h := newTestOAuthHandler(t, "")
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req1 := httptest.NewRequest("POST", "/v1/auth/chatgpt/codex-work/start", nil)
	req1.Header.Set("X-GoClaw-User-Id", "alice")
	w1 := httptest.NewRecorder()
	mux.ServeHTTP(w1, req1)

	if w1.Code == http.StatusInternalServerError {
		t.Skip("port 1455 unavailable, skipping")
	}
	if w1.Code != http.StatusOK {
		t.Fatalf("first start status = %d, want %d", w1.Code, http.StatusOK)
	}
	key := oauthFlowKey(req1.Context(), "codex-work")
	firstPending := h.pending[key]
	if firstPending == nil {
		t.Fatal("pending flow = nil after first start")
	}
	defer func() {
		firstPending.cancel()
		firstPending.login.Shutdown()
	}()

	req2 := httptest.NewRequest("POST", "/v1/auth/chatgpt/codex-personal/start", nil)
	req2.Header.Set("X-GoClaw-User-Id", "bob")
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)

	if w2.Code != http.StatusConflict {
		t.Fatalf("second start status = %d, want %d", w2.Code, http.StatusConflict)
	}
	if h.pending[key] != firstPending {
		t.Fatal("first pending flow should remain active")
	}
}

func TestOAuthHandlerManualCallbackRequiresSameRequester(t *testing.T) {
	h := newTestOAuthHandler(t, "")
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req1 := httptest.NewRequest("POST", "/v1/auth/chatgpt/codex-work/start", nil)
	req1.Header.Set("X-GoClaw-User-Id", "alice")
	w1 := httptest.NewRecorder()
	mux.ServeHTTP(w1, req1)

	if w1.Code == http.StatusInternalServerError {
		t.Skip("port 1455 unavailable, skipping")
	}
	if w1.Code != http.StatusOK {
		t.Fatalf("first start status = %d, want %d", w1.Code, http.StatusOK)
	}
	key := oauthFlowKey(req1.Context(), "codex-work")
	firstPending := h.pending[key]
	if firstPending == nil {
		t.Fatal("pending flow = nil after first start")
	}
	defer func() {
		firstPending.cancel()
		firstPending.login.Shutdown()
	}()

	req2 := httptest.NewRequest("POST", "/v1/auth/chatgpt/codex-work/callback", nil)
	req2.Header.Set("X-GoClaw-User-Id", "bob")
	req2 = httptest.NewRequest(
		"POST",
		"/v1/auth/chatgpt/codex-work/callback",
		strings.NewReader(`{"redirect_url":"http://localhost:1455/auth/callback?code=test&state=wrong"}`),
	)
	req2.Header.Set("X-GoClaw-User-Id", "bob")
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)

	if w2.Code != http.StatusBadRequest {
		t.Fatalf("callback status = %d, want %d", w2.Code, http.StatusBadRequest)
	}
}
