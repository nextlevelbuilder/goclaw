package http

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"

	"github.com/google/uuid"
)

// setupTestCache initializes the package-level cache for testing.
// Returns a cleanup function to restore state.
func setupTestCache(t *testing.T, keys map[string]*store.APIKeyData) *mockAPIKeyStore {
	t.Helper()
	ms := newMockAPIKeyStore()
	maps.Copy(ms.keys, keys)
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

func setupTestTenantStore(t *testing.T, ts store.TenantStore) {
	t.Helper()
	old := pkgTenantCache
	pkgTenantCache = newTenantCache(ts, 5*time.Minute)
	t.Cleanup(func() { pkgTenantCache = old })
}

func setupTestPairingStore(t *testing.T, ps store.PairingStore) {
	t.Helper()
	old := pkgPairingStore
	pkgPairingStore = ps
	t.Cleanup(func() { pkgPairingStore = old })
}

type mockTenantStore struct {
	tenantsByID   map[uuid.UUID]*store.TenantData
	tenantsBySlug map[string]*store.TenantData
	roles         map[uuid.UUID]map[string]string
}

func newMockTenantStore() *mockTenantStore {
	return &mockTenantStore{
		tenantsByID:   make(map[uuid.UUID]*store.TenantData),
		tenantsBySlug: make(map[string]*store.TenantData),
		roles:         make(map[uuid.UUID]map[string]string),
	}
}

func (m *mockTenantStore) addTenant(id uuid.UUID, slug string) {
	t := &store.TenantData{ID: id, Slug: slug, Name: slug}
	m.tenantsByID[id] = t
	m.tenantsBySlug[slug] = t
}

func (m *mockTenantStore) setUserRole(tenantID uuid.UUID, userID, role string) {
	if m.roles[tenantID] == nil {
		m.roles[tenantID] = make(map[string]string)
	}
	m.roles[tenantID][userID] = role
}

func (m *mockTenantStore) CreateTenant(context.Context, *store.TenantData) error { return nil }
func (m *mockTenantStore) GetTenant(_ context.Context, id uuid.UUID) (*store.TenantData, error) {
	if t := m.tenantsByID[id]; t != nil {
		return t, nil
	}
	return nil, fmt.Errorf("not found")
}
func (m *mockTenantStore) GetTenantBySlug(_ context.Context, slug string) (*store.TenantData, error) {
	if t := m.tenantsBySlug[slug]; t != nil {
		return t, nil
	}
	return nil, fmt.Errorf("not found")
}
func (m *mockTenantStore) ListTenants(context.Context) ([]store.TenantData, error) { return nil, nil }
func (m *mockTenantStore) UpdateTenant(context.Context, uuid.UUID, map[string]any) error {
	return nil
}
func (m *mockTenantStore) AddUser(context.Context, uuid.UUID, string, string) error { return nil }
func (m *mockTenantStore) RemoveUser(context.Context, uuid.UUID, string) error      { return nil }
func (m *mockTenantStore) GetUserRole(_ context.Context, tenantID uuid.UUID, userID string) (string, error) {
	if role := m.roles[tenantID][userID]; role != "" {
		return role, nil
	}
	return "", nil
}
func (m *mockTenantStore) ListUsers(context.Context, uuid.UUID) ([]store.TenantUserData, error) {
	return nil, nil
}
func (m *mockTenantStore) ListUserTenants(context.Context, string) ([]store.TenantUserData, error) {
	return nil, nil
}
func (m *mockTenantStore) ResolveUserTenant(context.Context, string) (uuid.UUID, error) {
	return store.MasterTenantID, nil
}
func (m *mockTenantStore) GetTenantUser(context.Context, uuid.UUID) (*store.TenantUserData, error) {
	return nil, fmt.Errorf("not found")
}
func (m *mockTenantStore) CreateTenantUserReturning(context.Context, uuid.UUID, string, string, string) (*store.TenantUserData, error) {
	return nil, fmt.Errorf("not implemented")
}

type mockPairingStore struct {
	paired map[string]bool
}

func newMockPairingStore() *mockPairingStore {
	return &mockPairingStore{paired: make(map[string]bool)}
}

func (m *mockPairingStore) RequestPairing(context.Context, string, string, string, string, map[string]string) (string, error) {
	return "", nil
}
func (m *mockPairingStore) ApprovePairing(context.Context, string, string) (*store.PairedDeviceData, error) {
	return nil, nil
}
func (m *mockPairingStore) DenyPairing(context.Context, string) error           { return nil }
func (m *mockPairingStore) RevokePairing(context.Context, string, string) error { return nil }
func (m *mockPairingStore) IsPaired(_ context.Context, senderID, channel string) (bool, error) {
	return m.paired[senderID+":"+channel], nil
}
func (m *mockPairingStore) ListPending(context.Context) []store.PairingRequestData { return nil }
func (m *mockPairingStore) ListPaired(context.Context) []store.PairedDeviceData    { return nil }

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

func TestResolveAuth_GatewayTokenScopesNonOwnerToMemberTenant(t *testing.T) {
	setupTestCache(t, nil)
	setupTestToken(t, "my-gateway-token")
	ts := newMockTenantStore()
	tenantID := uuid.New()
	ts.addTenant(tenantID, "acme")
	ts.setUserRole(tenantID, "user-1", store.TenantRoleAdmin)
	setupTestTenantStore(t, ts)

	r := httptest.NewRequest("GET", "/v1/agents", nil)
	r.Header.Set("Authorization", "Bearer my-gateway-token")
	r.Header.Set("X-GoClaw-User-Id", "user-1")
	r.Header.Set("X-GoClaw-Tenant-Id", "acme")

	auth := resolveAuth(r)
	if !auth.Authenticated {
		t.Fatal("expected authenticated")
	}
	if auth.Role != permissions.RoleAdmin {
		t.Fatalf("role = %v, want admin", auth.Role)
	}
	if auth.TenantID != tenantID {
		t.Fatalf("tenantID = %v, want %v", auth.TenantID, tenantID)
	}
}

func TestResolveAuth_GatewayTokenRejectsUnauthorizedTenantScope(t *testing.T) {
	setupTestCache(t, nil)
	setupTestToken(t, "my-gateway-token")
	ts := newMockTenantStore()
	ts.addTenant(uuid.New(), "acme")
	setupTestTenantStore(t, ts)

	r := httptest.NewRequest("GET", "/v1/agents", nil)
	r.Header.Set("Authorization", "Bearer my-gateway-token")
	r.Header.Set("X-GoClaw-User-Id", "user-1")
	r.Header.Set("X-GoClaw-Tenant-Id", "acme")

	auth := resolveAuth(r)
	if auth.Authenticated {
		t.Fatal("expected unauthenticated for unauthorized tenant scope")
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

func TestResolveAuth_SystemAPIKeyKeepsScopeDerivedRole(t *testing.T) {
	token := "system-admin-key"
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(token): {
			ID:     uuid.New(),
			Scopes: []string{"operator.admin"},
		},
	})

	r := httptest.NewRequest("GET", "/v1/providers", nil)
	r.Header.Set("Authorization", "Bearer "+token)

	auth := resolveAuth(r)
	if !auth.Authenticated {
		t.Fatal("expected authenticated")
	}
	if auth.Role != permissions.RoleAdmin {
		t.Fatalf("role = %v, want admin", auth.Role)
	}
	if auth.TenantID != store.MasterTenantID {
		t.Fatalf("tenantID = %v, want master tenant", auth.TenantID)
	}
}

func TestResolveAuth_SystemAPIKeyHonorsTenantScopeHeader(t *testing.T) {
	token := "system-admin-key"
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(token): {
			ID:     uuid.New(),
			Scopes: []string{"operator.admin"},
		},
	})
	ts := newMockTenantStore()
	tenantID := uuid.New()
	ts.addTenant(tenantID, "acme")
	setupTestTenantStore(t, ts)

	r := httptest.NewRequest("GET", "/v1/providers", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("X-GoClaw-Tenant-Id", "acme")

	auth := resolveAuth(r)
	if !auth.Authenticated {
		t.Fatal("expected authenticated")
	}
	if auth.Role != permissions.RoleAdmin {
		t.Fatalf("role = %v, want admin", auth.Role)
	}
	if auth.TenantID != tenantID {
		t.Fatalf("tenantID = %v, want %v", auth.TenantID, tenantID)
	}
}

func TestResolveAuth_BrowserPairingScopesToMemberTenant(t *testing.T) {
	setupTestToken(t, "gateway-token")
	ps := newMockPairingStore()
	ps.paired["browser-1:browser"] = true
	setupTestPairingStore(t, ps)
	ts := newMockTenantStore()
	tenantID := uuid.New()
	ts.addTenant(tenantID, "acme")
	ts.setUserRole(tenantID, "user-1", store.TenantRoleAdmin)
	setupTestTenantStore(t, ts)

	r := httptest.NewRequest("GET", "/v1/agents", nil)
	r.Header.Set("X-GoClaw-Sender-Id", "browser-1")
	r.Header.Set("X-GoClaw-User-Id", "user-1")
	r.Header.Set("X-GoClaw-Tenant-Id", "acme")

	auth := resolveAuth(r)
	if !auth.Authenticated {
		t.Fatal("expected authenticated")
	}
	if auth.Role != permissions.RoleOperator {
		t.Fatalf("role = %v, want operator", auth.Role)
	}
	if auth.TenantID != tenantID {
		t.Fatalf("tenantID = %v, want %v", auth.TenantID, tenantID)
	}
}

func TestResolveAuth_BrowserPairingRejectsUnauthorizedTenantScope(t *testing.T) {
	setupTestToken(t, "gateway-token")
	ps := newMockPairingStore()
	ps.paired["browser-1:browser"] = true
	setupTestPairingStore(t, ps)
	ts := newMockTenantStore()
	ts.addTenant(uuid.New(), "acme")
	setupTestTenantStore(t, ts)

	r := httptest.NewRequest("GET", "/v1/agents", nil)
	r.Header.Set("X-GoClaw-Sender-Id", "browser-1")
	r.Header.Set("X-GoClaw-User-Id", "user-1")
	r.Header.Set("X-GoClaw-Tenant-Id", "acme")

	auth := resolveAuth(r)
	if auth.Authenticated {
		t.Fatal("expected unauthenticated for unauthorized tenant scope")
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
