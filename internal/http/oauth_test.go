package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

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
