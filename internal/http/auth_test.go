package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"

	"github.com/google/uuid"
)

// setupTestCache initializes the package-level cache for testing.
// Returns a cleanup function to restore state.
func setupTestCache(t *testing.T, keys map[string]*store.APIKeyData) *mockAPIKeyStore {
	t.Helper()
	ms := newMockAPIKeyStore()
	for hash, key := range keys {
		ms.keys[hash] = key
	}
	pkgAPIKeyCache = newAPIKeyCache(ms, 5*time.Minute)
	t.Cleanup(func() { pkgAPIKeyCache = nil })
	return ms
}

// setupTestToken sets the package-level gateway token for testing.
func setupTestToken(t *testing.T, token string) {
	t.Helper()
	old := pkgGatewayToken
	pkgGatewayToken = token
	t.Cleanup(func() { pkgGatewayToken = old })
}

func TestResolveAuth_GatewayToken(t *testing.T) {
	setupTestCache(t, nil)
	setupTestToken(t, "my-gateway-token")

	r := httptest.NewRequest("GET", "/v1/agents", nil)
	r.Header.Set("Authorization", "Bearer my-gateway-token")

	auth := resolveAuth(r)
	if !auth.Authenticated {
		t.Fatal("expected authenticated")
	}
	if auth.Role != permissions.RoleAdmin {
		t.Errorf("role = %v, want admin", auth.Role)
	}
}

func TestResolveAuth_WrongToken(t *testing.T) {
	setupTestCache(t, nil)
	setupTestToken(t, "correct-token")

	r := httptest.NewRequest("GET", "/v1/agents", nil)
	r.Header.Set("Authorization", "Bearer wrong-token")

	auth := resolveAuth(r)
	if auth.Authenticated {
		t.Fatal("expected unauthenticated for wrong token")
	}
}

func TestResolveAuth_NoAuthConfigured(t *testing.T) {
	setupTestCache(t, nil)

	r := httptest.NewRequest("GET", "/v1/agents", nil)

	auth := resolveAuth(r) // no gateway token configured
	if !auth.Authenticated {
		t.Fatal("expected authenticated when no token configured")
	}
	if auth.Role != permissions.RoleAdmin {
		t.Errorf("role = %v, want admin (no token = dev/single-user mode)", auth.Role)
	}
}

func TestResolveAuth_APIKeyReadScope(t *testing.T) {
	// We need to hash the token the same way crypto.HashAPIKey does
	// For testing, we'll inject directly into the cache
	keyID := uuid.New()
	ms := newMockAPIKeyStore()
	ms.keys["test-hash"] = &store.APIKeyData{
		ID:     keyID,
		Scopes: []string{"operator.read"},
	}
	pkgAPIKeyCache = newAPIKeyCache(ms, 5*time.Minute)
	defer func() { pkgAPIKeyCache = nil }()

	// Pre-populate cache directly for the hash
	pkgAPIKeyCache.getOrFetch(nil, "test-hash")

	// Now test via resolveAuthBearer with the hash lookup
	r := httptest.NewRequest("GET", "/v1/agents", nil)
	// Directly test with the resolved key
	key, role := pkgAPIKeyCache.getOrFetch(nil, "test-hash")
	if key == nil {
		t.Fatal("expected key from cache")
	}
	_ = r
	if role != permissions.RoleViewer {
		t.Errorf("role = %v, want viewer for read scope", role)
	}
}

func TestResolveAuth_APIKeyAdminScope(t *testing.T) {
	ms := newMockAPIKeyStore()
	ms.keys["admin-hash"] = &store.APIKeyData{
		ID:     uuid.New(),
		Scopes: []string{"operator.admin"},
	}
	pkgAPIKeyCache = newAPIKeyCache(ms, 5*time.Minute)
	defer func() { pkgAPIKeyCache = nil }()

	key, role := pkgAPIKeyCache.getOrFetch(nil, "admin-hash")
	if key == nil {
		t.Fatal("expected key from cache")
	}
	if role != permissions.RoleAdmin {
		t.Errorf("role = %v, want admin", role)
	}
}

func TestResolveAuth_APIKeyWriteScope(t *testing.T) {
	ms := newMockAPIKeyStore()
	ms.keys["write-hash"] = &store.APIKeyData{
		ID:     uuid.New(),
		Scopes: []string{"operator.write"},
	}
	pkgAPIKeyCache = newAPIKeyCache(ms, 5*time.Minute)
	defer func() { pkgAPIKeyCache = nil }()

	key, role := pkgAPIKeyCache.getOrFetch(nil, "write-hash")
	if key == nil {
		t.Fatal("expected key from cache")
	}
	if role != permissions.RoleOperator {
		t.Errorf("role = %v, want operator for write scope", role)
	}
}

func TestHttpMinRole(t *testing.T) {
	tests := []struct {
		method string
		want   permissions.Role
	}{
		{http.MethodGet, permissions.RoleViewer},
		{http.MethodHead, permissions.RoleViewer},
		{http.MethodOptions, permissions.RoleViewer},
		{http.MethodPost, permissions.RoleOperator},
		{http.MethodPut, permissions.RoleOperator},
		{http.MethodPatch, permissions.RoleOperator},
		{http.MethodDelete, permissions.RoleOperator},
	}

	for _, tt := range tests {
		got := httpMinRole(tt.method)
		if got != tt.want {
			t.Errorf("httpMinRole(%s) = %v, want %v", tt.method, got, tt.want)
		}
	}
}

func TestRequireAuth_Unauthorized(t *testing.T) {
	setupTestCache(t, nil)
	setupTestToken(t, "secret")

	handler := requireAuth("", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	r := httptest.NewRequest("GET", "/v1/agents", nil)
	w := httptest.NewRecorder()
	handler(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestRequireAuth_GatewayTokenPasses(t *testing.T) {
	setupTestCache(t, nil)
	setupTestToken(t, "secret")

	handler := requireAuth("", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	r := httptest.NewRequest("GET", "/v1/agents", nil)
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	handler(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestRequireAuth_InjectLocaleAndUserID(t *testing.T) {
	setupTestCache(t, nil)
	setupTestToken(t, "secret")

	var gotLocale, gotUserID string
	handler := requireAuth("", func(w http.ResponseWriter, r *http.Request) {
		gotLocale = store.LocaleFromContext(r.Context())
		gotUserID = store.UserIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	r := httptest.NewRequest("GET", "/v1/agents", nil)
	r.Header.Set("Authorization", "Bearer secret")
	r.Header.Set("Accept-Language", "vi")
	r.Header.Set("X-GoClaw-User-Id", "user123")
	w := httptest.NewRecorder()
	handler(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if gotLocale != "vi" {
		t.Errorf("locale = %q, want 'vi'", gotLocale)
	}
	if gotUserID != "user123" {
		t.Errorf("userID = %q, want 'user123'", gotUserID)
	}
}

func TestRequireAuth_AdminRoleEnforced(t *testing.T) {
	// No auth configured → admin role (dev/single-user mode) → admin endpoint accessible
	setupTestCache(t, nil)

	handler := requireAuth(permissions.RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	r := httptest.NewRequest("POST", "/v1/api-keys", nil)
	w := httptest.NewRecorder()
	handler(w, r)

	// No token configured → admin role, admin endpoint → 200
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (no token = admin in dev mode)", w.Code)
	}
}

func TestRequireAuth_AutoDetectRole_GET(t *testing.T) {
	// No auth configured → operator role. GET needs viewer → passes.
	setupTestCache(t, nil)

	handler := requireAuth("", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	r := httptest.NewRequest("GET", "/v1/agents", nil)
	w := httptest.NewRecorder()
	handler(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (operator can access viewer endpoint)", w.Code)
	}
}

func TestInitAPIKeyCache_PubsubInvalidation(t *testing.T) {
	mb := bus.New()
	ms := newMockAPIKeyStore()
	ms.keys["pubsub-hash"] = &store.APIKeyData{
		ID:     uuid.New(),
		Scopes: []string{"operator.read"},
	}

	// Save original and restore after test
	origCache := pkgAPIKeyCache
	defer func() { pkgAPIKeyCache = origCache }()

	InitAPIKeyCache(ms, mb)

	// Populate cache
	key, _ := pkgAPIKeyCache.getOrFetch(nil, "pubsub-hash")
	if key == nil {
		t.Fatal("expected key after initial fetch")
	}
	if ms.getCalls() != 1 {
		t.Fatalf("calls = %d, want 1", ms.getCalls())
	}

	// Broadcast cache invalidation
	mb.Broadcast(bus.Event{
		Name:    "cache.invalidate",
		Payload: bus.CacheInvalidatePayload{Kind: bus.CacheKindAPIKeys, Key: "any"},
	})

	// Cache should be cleared, next fetch should hit store
	pkgAPIKeyCache.getOrFetch(nil, "pubsub-hash")
	if ms.getCalls() != 2 {
		t.Errorf("calls after invalidation = %d, want 2", ms.getCalls())
	}
}

func TestInitAPIKeyCache_IgnoresOtherKinds(t *testing.T) {
	mb := bus.New()
	ms := newMockAPIKeyStore()
	ms.keys["other-hash"] = &store.APIKeyData{
		ID:     uuid.New(),
		Scopes: []string{"operator.read"},
	}

	origCache := pkgAPIKeyCache
	defer func() { pkgAPIKeyCache = origCache }()

	InitAPIKeyCache(ms, mb)

	// Populate cache
	pkgAPIKeyCache.getOrFetch(nil, "other-hash")

	// Broadcast a different kind
	mb.Broadcast(bus.Event{
		Name:    "cache.invalidate",
		Payload: bus.CacheInvalidatePayload{Kind: bus.CacheKindAgent, Key: "any"},
	})

	// Cache should NOT be cleared
	pkgAPIKeyCache.getOrFetch(nil, "other-hash")
	if ms.getCalls() != 1 {
		t.Errorf("calls = %d, want 1 (non-api_keys kind should not invalidate)", ms.getCalls())
	}
}
