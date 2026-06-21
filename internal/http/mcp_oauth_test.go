package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	mcpoauth "github.com/nextlevelbuilder/goclaw/internal/mcp/oauth"
	"github.com/nextlevelbuilder/goclaw/internal/security"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// hasMCPCacheInvalidate reports whether a CacheKindMCP cache-invalidate event was broadcast.
func hasMCPCacheInvalidate(events []bus.Event) bool {
	for _, e := range events {
		if e.Name != protocol.EventCacheInvalidate {
			continue
		}
		if p, ok := e.Payload.(bus.CacheInvalidatePayload); ok && p.Kind == bus.CacheKindMCP {
			return true
		}
	}
	return false
}

// --------------------------------------------------------------------------
// Minimal mock implementations
// --------------------------------------------------------------------------

// mockMCPServerForOAuth implements only GetServer; everything else panics.
type mockMCPServerForOAuth struct {
	servers map[uuid.UUID]*store.MCPServerData
}

func newMockMCPServerForOAuth() *mockMCPServerForOAuth {
	return &mockMCPServerForOAuth{servers: make(map[uuid.UUID]*store.MCPServerData)}
}

func (m *mockMCPServerForOAuth) GetServer(_ context.Context, id uuid.UUID) (*store.MCPServerData, error) {
	s, ok := m.servers[id]
	if !ok {
		return nil, nil
	}
	return s, nil
}

// Stub implementations to satisfy the interface.
func (m *mockMCPServerForOAuth) CreateServer(_ context.Context, _ *store.MCPServerData) error {
	panic("not implemented")
}
func (m *mockMCPServerForOAuth) GetServerByName(_ context.Context, _ string) (*store.MCPServerData, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockMCPServerForOAuth) ListServers(_ context.Context) ([]store.MCPServerData, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockMCPServerForOAuth) UpdateServer(_ context.Context, _ uuid.UUID, _ map[string]any) error {
	return nil
}
func (m *mockMCPServerForOAuth) DeleteServer(_ context.Context, _ uuid.UUID) error {
	return fmt.Errorf("not implemented")
}
func (m *mockMCPServerForOAuth) GrantToAgent(_ context.Context, _ *store.MCPAgentGrant) error {
	return fmt.Errorf("not implemented")
}
func (m *mockMCPServerForOAuth) RevokeFromAgent(_ context.Context, _, _ uuid.UUID) error {
	return fmt.Errorf("not implemented")
}
func (m *mockMCPServerForOAuth) ListAgentGrants(_ context.Context, _ uuid.UUID) ([]store.MCPAgentGrant, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockMCPServerForOAuth) ListServerGrants(_ context.Context, _ uuid.UUID) ([]store.MCPAgentGrant, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockMCPServerForOAuth) GrantToUser(_ context.Context, _ *store.MCPUserGrant) error {
	return fmt.Errorf("not implemented")
}
func (m *mockMCPServerForOAuth) RevokeFromUser(_ context.Context, _ uuid.UUID, _ string) error {
	return fmt.Errorf("not implemented")
}
func (m *mockMCPServerForOAuth) CountAgentGrantsByServer(_ context.Context) (map[uuid.UUID]int, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockMCPServerForOAuth) ListAccessible(_ context.Context, _ uuid.UUID, _ string) ([]store.MCPAccessInfo, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockMCPServerForOAuth) CreateRequest(_ context.Context, _ *store.MCPAccessRequest) error {
	return fmt.Errorf("not implemented")
}
func (m *mockMCPServerForOAuth) ListPendingRequests(_ context.Context) ([]store.MCPAccessRequest, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockMCPServerForOAuth) ReviewRequest(_ context.Context, _ uuid.UUID, _ bool, _, _ string) error {
	return fmt.Errorf("not implemented")
}
func (m *mockMCPServerForOAuth) GetUserCredentials(_ context.Context, _ uuid.UUID, _ string) (*store.MCPUserCredentials, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockMCPServerForOAuth) SetUserCredentials(_ context.Context, _ uuid.UUID, _ string, _ store.MCPUserCredentials) error {
	return nil
}
func (m *mockMCPServerForOAuth) DeleteUserCredentials(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}

// mockOAuthTokenStore is a minimal in-memory MCPOAuthTokenStore.
type mockOAuthTokenStore struct {
	global  map[string]*store.MCPOAuthToken
	perUser map[string]*store.MCPOAuthToken
	deletes int
}

func newMockOAuthTokenStore() *mockOAuthTokenStore {
	return &mockOAuthTokenStore{
		global:  make(map[string]*store.MCPOAuthToken),
		perUser: make(map[string]*store.MCPOAuthToken),
	}
}

func (m *mockOAuthTokenStore) GetOAuthToken(_ context.Context, serverID, _ uuid.UUID) (*store.MCPOAuthToken, error) {
	if tok, ok := m.global[serverID.String()]; ok {
		cp := *tok
		return &cp, nil
	}
	return nil, nil
}
func (m *mockOAuthTokenStore) GetUserOAuthToken(_ context.Context, serverID, _ uuid.UUID, userID string) (*store.MCPOAuthToken, error) {
	if tok, ok := m.perUser[serverID.String()+":"+userID]; ok {
		cp := *tok
		return &cp, nil
	}
	return nil, nil
}
func (m *mockOAuthTokenStore) UpsertOAuthToken(_ context.Context, tok *store.MCPOAuthToken) error {
	cp := *tok
	if tok.UserID == "" {
		m.global[tok.ServerID.String()] = &cp
	} else {
		m.perUser[tok.ServerID.String()+":"+tok.UserID] = &cp
	}
	return nil
}
func (m *mockOAuthTokenStore) DeleteOAuthToken(_ context.Context, serverID, _ uuid.UUID) error {
	m.deletes++
	delete(m.global, serverID.String())
	return nil
}
func (m *mockOAuthTokenStore) DeleteUserOAuthToken(_ context.Context, serverID, _ uuid.UUID, userID string) error {
	m.deletes++
	delete(m.perUser, serverID.String()+":"+userID)
	return nil
}
func (m *mockOAuthTokenStore) DeleteServerOAuthTokens(_ context.Context, serverID, _ uuid.UUID) error {
	m.deletes++
	delete(m.global, serverID.String())
	prefix := serverID.String() + ":"
	for k := range m.perUser {
		if strings.HasPrefix(k, prefix) {
			delete(m.perUser, k)
		}
	}
	return nil
}

// mockPoolEvictor records calls to Evict/EvictServer.
type mockPoolEvictor struct {
	calls            int
	evictCalls       int
	evictServerCalls int
}

func (m *mockPoolEvictor) Evict(_ uuid.UUID, _ string)       { m.calls++; m.evictCalls++ }
func (m *mockPoolEvictor) EvictServer(_ uuid.UUID, _ string) { m.calls++; m.evictServerCalls++ }

// mockEventBus records broadcast events.
type mockEventBus struct {
	events []bus.Event
}

func (m *mockEventBus) Subscribe(_ string, _ bus.EventHandler) {}
func (m *mockEventBus) Unsubscribe(_ string)                   {}
func (m *mockEventBus) Broadcast(e bus.Event)                  { m.events = append(m.events, e) }

// --------------------------------------------------------------------------
// Test helpers
// --------------------------------------------------------------------------

// setAdminToken sets pkgGatewayToken for the test and restores it on cleanup.
func setAdminToken(t *testing.T, token string) {
	t.Helper()
	old := pkgGatewayToken
	pkgGatewayToken = token
	t.Cleanup(func() { pkgGatewayToken = old })
}

// adminRequest builds a request with admin Bearer token and optional body.
func adminRequest(method, path string, body any) *http.Request {
	var buf *bytes.Buffer
	if body != nil {
		data, _ := json.Marshal(body)
		buf = bytes.NewBuffer(data)
	} else {
		buf = &bytes.Buffer{}
	}
	req := httptest.NewRequest(method, path, buf)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	return req
}

// newTestMCPOAuthHandler creates an MCPOAuthHandler with mock dependencies.
// flowMgr and oauthStore are returned for callers that need to pre-seed state.
// oauthAdminTS is a test TenantStore that treats every caller as a tenant
// admin so requireTenantAdmin passes. Negative tests use oauthDenyTS instead.
type oauthAdminTS struct{ store.TenantStore }

func (oauthAdminTS) GetUserRole(context.Context, uuid.UUID, string) (string, error) {
	return store.TenantRoleAdmin, nil
}

// oauthDenyTS is a test TenantStore that grants no role to any caller, so a
// RoleAdmin caller who is not a tenant admin is rejected by requireTenantAdmin.
type oauthDenyTS struct{ store.TenantStore }

func (oauthDenyTS) GetUserRole(context.Context, uuid.UUID, string) (string, error) {
	return "", nil
}

func newTestMCPOAuthHandler(
	t *testing.T,
	mcpStore *mockMCPServerForOAuth,
	oauthStore *mockOAuthTokenStore,
	evictor MCPPoolEvictor,
	eventBus *mockEventBus,
) (*MCPOAuthHandler, *mcpoauth.FlowManager) {
	t.Helper()
	setAdminToken(t, "test-admin-token")
	fm := mcpoauth.NewFlowManager(http.DefaultClient)
	h := NewMCPOAuthHandler(MCPOAuthHandlerDeps{
		MCPStore:    mcpStore,
		OAuthStore:  oauthStore,
		FlowMgr:     fm,
		EventBus:    eventBus,
		Evictor:     evictor,
		Port:        18790,
		TenantStore: oauthAdminTS{},
	})
	return h, fm
}

// --------------------------------------------------------------------------
// callbackURL helper tests
// --------------------------------------------------------------------------

func TestCallbackURLPublicURL(t *testing.T) {
	h := &MCPOAuthHandler{publicURL: "https://goclaw.example.com"}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	got := h.callbackURL(r)
	want := "https://goclaw.example.com/v1/mcp/oauth/callback"
	if got != want {
		t.Errorf("callbackURL = %q, want %q", got, want)
	}
}

func TestCallbackURLForwardedHost(t *testing.T) {
	h := &MCPOAuthHandler{}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-Host", "goclaw.example.com")
	r.Header.Set("X-Forwarded-Proto", "https")
	got := h.callbackURL(r)
	if !strings.HasPrefix(got, "https://goclaw.example.com") {
		t.Errorf("callbackURL = %q, want https://goclaw.example.com prefix", got)
	}
}

func TestCallbackURLFallback(t *testing.T) {
	h := &MCPOAuthHandler{port: 18790}
	// httptest request has no Host header by default.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = ""
	got := h.callbackURL(r)
	if !strings.Contains(got, "18790") {
		t.Errorf("callbackURL fallback = %q, want port 18790", got)
	}
}

// --------------------------------------------------------------------------
// handleStart
// --------------------------------------------------------------------------

func TestHandleStartRequiresAdmin(t *testing.T) {
	setAdminToken(t, "test-admin-token")
	fm := mcpoauth.NewFlowManager(http.DefaultClient)
	h := NewMCPOAuthHandler(MCPOAuthHandlerDeps{
		MCPStore:   newMockMCPServerForOAuth(),
		OAuthStore: newMockOAuthTokenStore(),
		FlowMgr:    fm,
	})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// No Authorization header.
	req := httptest.NewRequest(http.MethodPost, "/v1/mcp/oauth/start",
		strings.NewReader(`{"server_id":"irrelevant","mcp_url":"http://example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestHandleStartMissingServerID(t *testing.T) {
	mcpSt := newMockMCPServerForOAuth()
	oauthSt := newMockOAuthTokenStore()
	h, _ := newTestMCPOAuthHandler(t, mcpSt, oauthSt, nil, &mockEventBus{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := map[string]string{"mcp_url": "http://example.com"} // missing server_id
	req := adminRequest(http.MethodPost, "/v1/mcp/oauth/start", body)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandleStartServerNotFound(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	// Discovery server so handleStart can get past validation.
	discSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, ".well-known") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"authorization_endpoint": "https://auth.example.com/authorize",
				"token_endpoint":         "https://auth.example.com/token",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer discSrv.Close()

	mcpSt := newMockMCPServerForOAuth() // empty — no servers
	oauthSt := newMockOAuthTokenStore()
	h, _ := newTestMCPOAuthHandler(t, mcpSt, oauthSt, nil, &mockEventBus{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := map[string]string{
		"server_id": uuid.New().String(),
		"mcp_url":   discSrv.URL,
	}
	req := adminRequest(http.MethodPost, "/v1/mcp/oauth/start", body)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Discovery succeeds but server not in DB → 404.
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleStartDiscoveryFails(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	// Server that returns 404 for all well-known paths → discovery fails.
	discSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer discSrv.Close()

	serverID := uuid.New()
	mcpSt := newMockMCPServerForOAuth()
	mcpSt.servers[serverID] = &store.MCPServerData{
		BaseModel: store.BaseModel{ID: serverID},
		Name:      "test-server",
		URL:       discSrv.URL,
	}

	oauthSt := newMockOAuthTokenStore()
	h, _ := newTestMCPOAuthHandler(t, mcpSt, oauthSt, nil, &mockEventBus{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := map[string]string{
		"server_id": serverID.String(),
		"mcp_url":   discSrv.URL,
	}
	req := adminRequest(http.MethodPost, "/v1/mcp/oauth/start", body)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (discovery failed)", w.Code)
	}
}

func TestHandleStartWithManualClientID(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	// Discovery server (no registration endpoint — forces use of manual credentials).
	discSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, ".well-known") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"authorization_endpoint": "https://auth.example.com/authorize",
				"token_endpoint":         "https://auth.example.com/token",
				// No registration_endpoint — DCR not available.
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer discSrv.Close()

	serverID := uuid.New()
	mcpSt := newMockMCPServerForOAuth()
	mcpSt.servers[serverID] = &store.MCPServerData{
		BaseModel: store.BaseModel{ID: serverID},
		Name:      "test-server",
		URL:       discSrv.URL,
		Settings:  json.RawMessage(`{"oauth":{"client_id":"manual-client-id"}}`),
	}

	oauthSt := newMockOAuthTokenStore()
	h, _ := newTestMCPOAuthHandler(t, mcpSt, oauthSt, nil, &mockEventBus{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := map[string]string{
		"server_id": serverID.String(),
		"mcp_url":   discSrv.URL,
	}
	req := adminRequest(http.MethodPost, "/v1/mcp/oauth/start", body)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		AuthURL  string `json:"auth_url"`
		ClientID string `json:"client_id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ClientID != "manual-client-id" {
		t.Errorf("client_id = %q, want %q", resp.ClientID, "manual-client-id")
	}
	if !strings.Contains(resp.AuthURL, "https://auth.example.com/authorize") {
		t.Errorf("auth_url = %q, want auth.example.com", resp.AuthURL)
	}
}

// TestHandleStartManualEndpoints verifies use_dcr=false honors operator-supplied
// auth/token endpoints without auto-discovery (no discovery server is wired).
func TestHandleStartManualEndpoints(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	serverID := uuid.New()
	mcpSt := newMockMCPServerForOAuth()
	mcpSt.servers[serverID] = &store.MCPServerData{
		BaseModel: store.BaseModel{ID: serverID},
		Name:      "manual-server",
		URL:       "http://127.0.0.1:1/mcp",
		Settings: json.RawMessage(`{"oauth":{"use_dcr":false,` +
			`"auth_endpoint":"http://127.0.0.1:1/authorize",` +
			`"token_endpoint":"http://127.0.0.1:1/token",` +
			`"client_id":"manual-client-id"}}`),
	}
	h, _ := newTestMCPOAuthHandler(t, mcpSt, newMockOAuthTokenStore(), nil, &mockEventBus{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := adminRequest(http.MethodPost, "/v1/mcp/oauth/start", map[string]string{
		"server_id": serverID.String(),
		"mcp_url":   "http://127.0.0.1:1/mcp",
	})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		AuthURL  string `json:"auth_url"`
		ClientID string `json:"client_id"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.ClientID != "manual-client-id" {
		t.Errorf("client_id = %q, want manual-client-id", resp.ClientID)
	}
	if !strings.Contains(resp.AuthURL, "http://127.0.0.1:1/authorize") {
		t.Errorf("auth_url = %q, want the manual authorize endpoint", resp.AuthURL)
	}
}

// TestHandleStartManualMissingEndpoints: use_dcr=false without endpoints → 400.
func TestHandleStartManualMissingEndpoints(t *testing.T) {
	serverID := uuid.New()
	mcpSt := newMockMCPServerForOAuth()
	mcpSt.servers[serverID] = &store.MCPServerData{
		BaseModel: store.BaseModel{ID: serverID},
		Name:      "manual-server",
		URL:       "http://127.0.0.1:1/mcp",
		Settings:  json.RawMessage(`{"oauth":{"use_dcr":false,"client_id":"x"}}`),
	}
	h, _ := newTestMCPOAuthHandler(t, mcpSt, newMockOAuthTokenStore(), nil, &mockEventBus{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := adminRequest(http.MethodPost, "/v1/mcp/oauth/start", map[string]string{
		"server_id": serverID.String(), "mcp_url": "http://127.0.0.1:1/mcp",
	})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

// TestHandleStartManualEndpointSSRF: a manual endpoint pointing at cloud metadata
// must be rejected by the SSRF guard (no allowLoopback).
func TestHandleStartManualEndpointSSRF(t *testing.T) {
	serverID := uuid.New()
	mcpSt := newMockMCPServerForOAuth()
	mcpSt.servers[serverID] = &store.MCPServerData{
		BaseModel: store.BaseModel{ID: serverID},
		Name:      "manual-server",
		URL:       "https://mcp.example.com/mcp",
		Settings: json.RawMessage(`{"oauth":{"use_dcr":false,` +
			`"auth_endpoint":"http://169.254.169.254/authorize",` +
			`"token_endpoint":"http://169.254.169.254/token",` +
			`"client_id":"x"}}`),
	}
	h, _ := newTestMCPOAuthHandler(t, mcpSt, newMockOAuthTokenStore(), nil, &mockEventBus{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := adminRequest(http.MethodPost, "/v1/mcp/oauth/start", map[string]string{
		"server_id": serverID.String(), "mcp_url": "https://mcp.example.com/mcp",
	})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (SSRF); body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "endpoint") {
		t.Errorf("expected an endpoint SSRF error, got: %s", w.Body.String())
	}
}

// TestHandleStartManualClientCredentials: use_dcr=false with client_credentials
// needs only token_endpoint (no authorization URL) and mints a token directly.
func TestHandleStartManualClientCredentials(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "cc-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer tokenSrv.Close()

	serverID := uuid.New()
	mcpSt := newMockMCPServerForOAuth()
	mcpSt.servers[serverID] = &store.MCPServerData{
		BaseModel: store.BaseModel{ID: serverID},
		Name:      "cc-server",
		URL:       "http://127.0.0.1:1/mcp",
		Settings: json.RawMessage(`{"oauth":{"use_dcr":false,"grant_type":"client_credentials",` +
			`"token_endpoint":"` + tokenSrv.URL + `",` +
			`"client_id":"cc-client","client_secret":"cc-secret"}}`),
	}
	oauthSt := newMockOAuthTokenStore()
	h, _ := newTestMCPOAuthHandler(t, mcpSt, oauthSt, nil, &mockEventBus{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := adminRequest(http.MethodPost, "/v1/mcp/oauth/start", map[string]string{
		"server_id": serverID.String(), "mcp_url": "http://127.0.0.1:1/mcp",
	})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Completed bool `json:"completed"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if !resp.Completed {
		t.Error("expected completed=true for manual client_credentials")
	}
	if tok, _ := oauthSt.GetOAuthToken(context.Background(), serverID, uuid.Nil); tok == nil || tok.AccessToken != "cc-access-token" {
		t.Errorf("expected cc token persisted, got %+v", tok)
	}
}

// TestHandleStartRejectsNonTenantAdmin: a RoleAdmin caller that is not a tenant
// admin for the target tenant must be rejected (Finding 1).
func TestHandleStartRejectsNonTenantAdmin(t *testing.T) {
	setAdminToken(t, "test-admin-token")
	serverID := uuid.New()
	mcpSt := newMockMCPServerForOAuth()
	mcpSt.servers[serverID] = &store.MCPServerData{
		BaseModel: store.BaseModel{ID: serverID},
		Name:      "srv",
		URL:       "https://mcp.example.com/mcp",
	}
	h := NewMCPOAuthHandler(MCPOAuthHandlerDeps{
		MCPStore:    mcpSt,
		OAuthStore:  newMockOAuthTokenStore(),
		FlowMgr:     mcpoauth.NewFlowManager(http.DefaultClient),
		TenantStore: oauthDenyTS{}, // RoleAdmin but no tenant-admin role
		Port:        18790,
	})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := adminRequest(http.MethodPost, "/v1/mcp/oauth/start", map[string]string{
		"server_id": serverID.String(), "mcp_url": "https://mcp.example.com/mcp",
	})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("non-tenant-admin start status = %d, want 403; body: %s", w.Code, w.Body.String())
	}
}

// TestHandleStartSelfServicePerUser: a non-admin user may authorize their OWN
// per-user token without tenant-admin rights (matches the user-credentials flow).
func TestHandleStartSelfServicePerUser(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	serverID := uuid.New()
	mcpSt := newMockMCPServerForOAuth()
	mcpSt.servers[serverID] = &store.MCPServerData{
		BaseModel: store.BaseModel{ID: serverID},
		Name:      "manual-server",
		URL:       "http://127.0.0.1:1/mcp",
		Settings: json.RawMessage(`{"oauth":{"use_dcr":false,` +
			`"auth_endpoint":"http://127.0.0.1:1/authorize",` +
			`"token_endpoint":"http://127.0.0.1:1/token",` +
			`"client_id":"manual-client-id"}}`),
	}
	// denyTenantStore would 403 if the request fell through to the tenant-admin gate.
	h := NewMCPOAuthHandler(MCPOAuthHandlerDeps{
		MCPStore:    mcpSt,
		OAuthStore:  newMockOAuthTokenStore(),
		FlowMgr:     mcpoauth.NewFlowManager(http.DefaultClient),
		TenantStore: oauthDenyTS{},
		Port:        18790,
	})

	body, _ := json.Marshal(map[string]string{
		"server_id": serverID.String(),
		"mcp_url":   "http://127.0.0.1:1/mcp",
		"user_id":   "user-1",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/mcp/oauth/start", bytes.NewReader(body))
	// Regular user "user-1" authorizing their own per-user token (call handler
	// directly, mirroring the user-credentials tests).
	req = req.WithContext(store.WithTenantID(store.WithUserID(req.Context(), "user-1"), uuid.New()))
	w := httptest.NewRecorder()
	h.handleStart(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("self-service per-user start status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

// TestHandleStartPerUserOnBehalfRequiresAdmin: a non-admin user may NOT authorize
// another user's token — that requires tenant-admin.
func TestHandleStartPerUserOnBehalfRequiresAdmin(t *testing.T) {
	serverID := uuid.New()
	mcpSt := newMockMCPServerForOAuth()
	mcpSt.servers[serverID] = &store.MCPServerData{
		BaseModel: store.BaseModel{ID: serverID},
		Name:      "srv",
		URL:       "https://mcp.example.com/mcp",
	}
	h := NewMCPOAuthHandler(MCPOAuthHandlerDeps{
		MCPStore:    mcpSt,
		OAuthStore:  newMockOAuthTokenStore(),
		FlowMgr:     mcpoauth.NewFlowManager(http.DefaultClient),
		TenantStore: oauthDenyTS{},
		Port:        18790,
	})

	body, _ := json.Marshal(map[string]string{
		"server_id": serverID.String(),
		"mcp_url":   "https://mcp.example.com/mcp",
		"user_id":   "other-user",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/mcp/oauth/start", bytes.NewReader(body))
	req = req.WithContext(store.WithTenantID(store.WithUserID(req.Context(), "user-1"), uuid.New()))
	w := httptest.NewRecorder()
	h.handleStart(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("on-behalf-of-another start status = %d, want 403; body: %s", w.Code, w.Body.String())
	}
}

// --------------------------------------------------------------------------
// handleCallback
// --------------------------------------------------------------------------

func TestHandleCallbackOAuthProviderError(t *testing.T) {
	mcpSt := newMockMCPServerForOAuth()
	oauthSt := newMockOAuthTokenStore()
	h, _ := newTestMCPOAuthHandler(t, mcpSt, oauthSt, nil, &mockEventBus{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet,
		"/v1/mcp/oauth/callback?error=access_denied&error_description=User+denied", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (error page is still 200)", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "mcp-oauth-complete") {
		t.Error("response should contain mcp-oauth-complete JS message")
	}
	if !strings.Contains(body, `"error"`) {
		t.Error("response should contain error status")
	}
}

func TestHandleCallbackMissingCodeAndState(t *testing.T) {
	mcpSt := newMockMCPServerForOAuth()
	oauthSt := newMockOAuthTokenStore()
	h, _ := newTestMCPOAuthHandler(t, mcpSt, oauthSt, nil, &mockEventBus{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// No code and no state query params.
	req := httptest.NewRequest(http.MethodGet, "/v1/mcp/oauth/callback", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (HTML error page)", w.Code)
	}
	if !strings.Contains(w.Body.String(), "error") {
		t.Error("response should indicate error")
	}
}

func TestHandleCallbackInvalidState(t *testing.T) {
	mcpSt := newMockMCPServerForOAuth()
	oauthSt := newMockOAuthTokenStore()
	h, _ := newTestMCPOAuthHandler(t, mcpSt, oauthSt, nil, &mockEventBus{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// state that was never registered in FlowManager.
	req := httptest.NewRequest(http.MethodGet,
		"/v1/mcp/oauth/callback?code=abc&state=no-such-state", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (HTML error page)", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "error") {
		t.Error("response should indicate error for invalid state")
	}
}

func TestHandleCallbackSuccessPublishesWSEvent(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	// Mock token endpoint.
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "new-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer tokenSrv.Close()

	serverID := uuid.New()
	tenantID := uuid.New()
	mcpSt := newMockMCPServerForOAuth()
	mcpSt.servers[serverID] = &store.MCPServerData{
		BaseModel: store.BaseModel{ID: serverID},
		Name:      "oauth-server",
	}
	oauthSt := newMockOAuthTokenStore()
	evtBus := &mockEventBus{}
	evictor := &mockPoolEvictor{}

	setAdminToken(t, "test-admin-token")
	fm := mcpoauth.NewFlowManager(security.NewSafeClient(5 * time.Second))
	h := NewMCPOAuthHandler(MCPOAuthHandlerDeps{
		MCPStore:   mcpSt,
		OAuthStore: oauthSt,
		FlowMgr:    fm,
		EventBus:   evtBus,
		Evictor:    evictor,
	})

	// Seed a pending flow directly using StartFlow.
	disc := &mcpoauth.DiscoveryResult{
		AuthorizationEndpoint: "https://auth.example.com/authorize",
		TokenEndpoint:         tokenSrv.URL,
	}
	_, state, err := fm.StartFlow(context.Background(), mcpoauth.StartFlowParams{
		ServerID:        serverID,
		TenantID:        tenantID,
		DiscoveryResult: disc,
		ClientID:        "test-client",
		RedirectURI:     "http://localhost:18790/v1/mcp/oauth/callback",
	})
	if err != nil {
		t.Fatalf("StartFlow() error: %v", err)
	}

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet,
		"/v1/mcp/oauth/callback?code=auth-code&state="+state, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "success") {
		t.Errorf("response body missing 'success': %s", body)
	}

	// Token should be saved.
	if tok := oauthSt.global[serverID.String()]; tok == nil {
		t.Error("expected OAuth token to be saved in store")
	}
	// WS event should be published.
	if len(evtBus.events) == 0 {
		t.Error("expected mcp.oauth_complete WS event to be published")
	}
	// Pool should be evicted.
	if evictor.calls == 0 {
		t.Error("expected EvictServer to be called")
	}
	// MCP cache-invalidate event must be broadcast so per-user pool connections are
	// evicted and agent Loop caches reload (new token takes effect immediately).
	if !hasMCPCacheInvalidate(evtBus.events) {
		t.Error("expected an MCP CacheKindMCP cache-invalidate event after authorize")
	}
}

// --------------------------------------------------------------------------
// handleStatus
// --------------------------------------------------------------------------

func TestHandleStatusGlobalToken(t *testing.T) {
	mcpSt := newMockMCPServerForOAuth()
	oauthSt := newMockOAuthTokenStore()
	h, _ := newTestMCPOAuthHandler(t, mcpSt, oauthSt, nil, &mockEventBus{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	serverID := uuid.New()
	oauthSt.global[serverID.String()] = &store.MCPOAuthToken{
		ServerID:    serverID,
		AccessToken: "tok",
		DCRClientID: "client-xyz",
	}

	req := adminRequest(http.MethodGet, "/v1/mcp/oauth/status/"+serverID.String(), nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var resp struct {
		HasToken bool   `json:"has_token"`
		ClientID string `json:"client_id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.HasToken {
		t.Error("expected has_token = true")
	}
	if resp.ClientID != "client-xyz" {
		t.Errorf("client_id = %q, want %q", resp.ClientID, "client-xyz")
	}
}

func TestHandleStatusPerUserToken(t *testing.T) {
	mcpSt := newMockMCPServerForOAuth()
	oauthSt := newMockOAuthTokenStore()
	h, _ := newTestMCPOAuthHandler(t, mcpSt, oauthSt, nil, &mockEventBus{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	serverID := uuid.New()
	userID := "user-abc"
	oauthSt.perUser[serverID.String()+":"+userID] = &store.MCPOAuthToken{
		ServerID:    serverID,
		UserID:      userID,
		AccessToken: "user-tok",
	}

	req := adminRequest(http.MethodGet,
		"/v1/mcp/oauth/status/"+serverID.String()+"?user_id="+userID, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var resp struct {
		HasToken bool `json:"has_token"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.HasToken {
		t.Error("expected has_token = true for per-user token")
	}
}

func TestHandleStatusNoToken(t *testing.T) {
	mcpSt := newMockMCPServerForOAuth()
	oauthSt := newMockOAuthTokenStore()
	h, _ := newTestMCPOAuthHandler(t, mcpSt, oauthSt, nil, &mockEventBus{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := adminRequest(http.MethodGet, "/v1/mcp/oauth/status/"+uuid.New().String(), nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var resp struct {
		HasToken bool `json:"has_token"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.HasToken {
		t.Error("expected has_token = false when no token exists")
	}
}

// --------------------------------------------------------------------------
// handleRevoke
// --------------------------------------------------------------------------

func TestHandleRevokeGlobal(t *testing.T) {
	serverID := uuid.New()
	mcpSt := newMockMCPServerForOAuth()
	mcpSt.servers[serverID] = &store.MCPServerData{
		BaseModel: store.BaseModel{ID: serverID},
		Name:      "test-server",
	}
	oauthSt := newMockOAuthTokenStore()
	oauthSt.global[serverID.String()] = &store.MCPOAuthToken{ServerID: serverID, AccessToken: "tok"}
	evictor := &mockPoolEvictor{}
	h, _ := newTestMCPOAuthHandler(t, mcpSt, oauthSt, evictor, &mockEventBus{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := adminRequest(http.MethodDelete, "/v1/mcp/oauth/token/"+serverID.String(), nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if oauthSt.deletes == 0 {
		t.Error("expected DeleteOAuthToken to be called")
	}
	if evictor.calls == 0 {
		t.Error("expected EvictServer to be called on revoke")
	}
}

func TestHandleRevokePerUser(t *testing.T) {
	serverID := uuid.New()
	userID := "user-xyz"
	mcpSt := newMockMCPServerForOAuth()
	mcpSt.servers[serverID] = &store.MCPServerData{
		BaseModel: store.BaseModel{ID: serverID},
		Name:      "test-server",
	}
	oauthSt := newMockOAuthTokenStore()
	oauthSt.perUser[serverID.String()+":"+userID] = &store.MCPOAuthToken{
		ServerID:    serverID,
		UserID:      userID,
		AccessToken: "user-tok",
	}
	h, _ := newTestMCPOAuthHandler(t, mcpSt, oauthSt, nil, &mockEventBus{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := adminRequest(http.MethodDelete,
		"/v1/mcp/oauth/token/"+serverID.String()+"?user_id="+userID, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if oauthSt.deletes == 0 {
		t.Error("expected DeleteUserOAuthToken to be called")
	}
}

// --------------------------------------------------------------------------
// handleDiscover (admin only)
// --------------------------------------------------------------------------

func TestHandleDiscoverRequiresAdmin(t *testing.T) {
	setAdminToken(t, "test-admin-token")
	fm := mcpoauth.NewFlowManager(http.DefaultClient)
	h := NewMCPOAuthHandler(MCPOAuthHandlerDeps{
		MCPStore:   newMockMCPServerForOAuth(),
		OAuthStore: newMockOAuthTokenStore(),
		FlowMgr:    fm,
	})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// No auth header.
	req := httptest.NewRequest(http.MethodPost, "/v1/mcp/oauth/discover/"+uuid.New().String(), nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestHandleDiscoverSuccess(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	discSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, ".well-known") {
			w.Header().Set("Content-Type", "application/json")
			// Issuer omitted: an absent issuer is allowed by discovery (some OIDC
			// fallbacks omit it). A cross-origin issuer would be rejected — see
			// TestDiscoverIssuerMismatchRejected in the oauth package.
			_ = json.NewEncoder(w).Encode(map[string]string{
				"authorization_endpoint": "https://auth.example.com/authorize",
				"token_endpoint":         "https://auth.example.com/token",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer discSrv.Close()

	serverID := uuid.New()
	mcpSt := newMockMCPServerForOAuth()
	mcpSt.servers[serverID] = &store.MCPServerData{
		BaseModel: store.BaseModel{ID: serverID},
		Name:      "test-server",
		URL:       discSrv.URL,
	}

	oauthSt := newMockOAuthTokenStore()
	h, _ := newTestMCPOAuthHandler(t, mcpSt, oauthSt, nil, &mockEventBus{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := adminRequest(http.MethodPost, "/v1/mcp/oauth/discover/"+serverID.String(), nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		AuthorizationEndpoint string `json:"authorization_endpoint"`
		TokenEndpoint         string `json:"token_endpoint"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.AuthorizationEndpoint == "" {
		t.Error("expected authorization_endpoint in response")
	}
}
