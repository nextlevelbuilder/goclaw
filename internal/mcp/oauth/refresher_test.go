package mcpoauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/security"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// mockOAuthStore is a minimal in-memory implementation of store.MCPOAuthTokenStore.
type mockOAuthStore struct {
	global  map[string]*store.MCPOAuthToken // key: serverID
	perUser map[string]*store.MCPOAuthToken // key: serverID+":"+userID
	upserts int
}

func newMockOAuthStore() *mockOAuthStore {
	return &mockOAuthStore{
		global:  make(map[string]*store.MCPOAuthToken),
		perUser: make(map[string]*store.MCPOAuthToken),
	}
}

func (m *mockOAuthStore) GetOAuthToken(_ context.Context, serverID, _ uuid.UUID) (*store.MCPOAuthToken, error) {
	tok, ok := m.global[serverID.String()]
	if !ok {
		return nil, nil
	}
	cp := *tok
	return &cp, nil
}

func (m *mockOAuthStore) GetUserOAuthToken(_ context.Context, serverID, _ uuid.UUID, userID string) (*store.MCPOAuthToken, error) {
	tok, ok := m.perUser[serverID.String()+":"+userID]
	if !ok {
		return nil, nil
	}
	cp := *tok
	return &cp, nil
}

func (m *mockOAuthStore) UpsertOAuthToken(_ context.Context, tok *store.MCPOAuthToken) error {
	m.upserts++
	cp := *tok
	if tok.UserID == "" {
		m.global[tok.ServerID.String()] = &cp
	} else {
		m.perUser[tok.ServerID.String()+":"+tok.UserID] = &cp
	}
	return nil
}

func (m *mockOAuthStore) DeleteOAuthToken(_ context.Context, serverID, _ uuid.UUID) error {
	delete(m.global, serverID.String())
	return nil
}

func (m *mockOAuthStore) DeleteUserOAuthToken(_ context.Context, serverID, _ uuid.UUID, userID string) error {
	delete(m.perUser, serverID.String()+":"+userID)
	return nil
}

func (m *mockOAuthStore) DeleteServerOAuthTokens(_ context.Context, serverID, _ uuid.UUID) error {
	delete(m.global, serverID.String())
	prefix := serverID.String() + ":"
	for k := range m.perUser {
		if strings.HasPrefix(k, prefix) {
			delete(m.perUser, k)
		}
	}
	return nil
}

// futureTime returns a time this far in the future.
func futureTime(d time.Duration) *time.Time {
	t := time.Now().Add(d)
	return &t
}

// pastTime returns a time this far in the past.
func pastTime(d time.Duration) *time.Time {
	t := time.Now().Add(-d)
	return &t
}

func TestGetValidTokenCacheHit(t *testing.T) {
	st := newMockOAuthStore()
	r := NewRefresher(st, http.DefaultClient)

	serverID := uuid.New()
	tenantID := uuid.New()

	// Seed the store with a valid token.
	validTok := &store.MCPOAuthToken{
		ServerID:    serverID,
		TenantID:    tenantID,
		AccessToken: "cached-token",
		TokenType:   "Bearer",
		ExpiresAt:   futureTime(2 * time.Hour),
	}
	st.global[serverID.String()] = validTok

	// First call — loads from store and caches.
	tok1, err := r.GetValidToken(context.Background(), serverID, tenantID, "")
	if err != nil {
		t.Fatalf("first GetValidToken() error: %v", err)
	}
	if tok1 != "cached-token" {
		t.Errorf("token = %q, want %q", tok1, "cached-token")
	}

	// Remove from store to prove second call uses cache.
	delete(st.global, serverID.String())

	tok2, err := r.GetValidToken(context.Background(), serverID, tenantID, "")
	if err != nil {
		t.Fatalf("second GetValidToken() error: %v", err)
	}
	if tok2 != "cached-token" {
		t.Errorf("second token = %q, want %q (from cache)", tok2, "cached-token")
	}
}

func TestGetValidTokenCacheMiss(t *testing.T) {
	st := newMockOAuthStore()
	r := NewRefresher(st, http.DefaultClient)

	serverID := uuid.New()
	tenantID := uuid.New()

	tok := &store.MCPOAuthToken{
		ServerID:    serverID,
		TenantID:    tenantID,
		AccessToken: "fresh-from-db",
		TokenType:   "Bearer",
		ExpiresAt:   futureTime(30 * time.Minute),
	}
	st.global[serverID.String()] = tok

	got, err := r.GetValidToken(context.Background(), serverID, tenantID, "")
	if err != nil {
		t.Fatalf("GetValidToken() error: %v", err)
	}
	if got != "fresh-from-db" {
		t.Errorf("token = %q, want %q", got, "fresh-from-db")
	}
}

func TestGetValidTokenNoRecord(t *testing.T) {
	st := newMockOAuthStore()
	r := NewRefresher(st, http.DefaultClient)

	_, err := r.GetValidToken(context.Background(), uuid.New(), uuid.New(), "")
	if !errors.Is(err, ErrTokenExpired) {
		t.Errorf("expected ErrTokenExpired, got %v", err)
	}
}

func TestGetValidTokenExpiredNoRefreshToken(t *testing.T) {
	st := newMockOAuthStore()
	r := NewRefresher(st, http.DefaultClient)

	serverID := uuid.New()
	tenantID := uuid.New()

	st.global[serverID.String()] = &store.MCPOAuthToken{
		ServerID:     serverID,
		TenantID:     tenantID,
		AccessToken:  "old-token",
		TokenType:    "Bearer",
		ExpiresAt:    pastTime(time.Hour),
		RefreshToken: "", // no refresh token
	}

	_, err := r.GetValidToken(context.Background(), serverID, tenantID, "")
	if !errors.Is(err, ErrTokenExpired) {
		t.Errorf("expected ErrTokenExpired for expired token without refresh, got %v", err)
	}
}

func TestGetValidTokenRefreshSuccess(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		if r.FormValue("grant_type") != "refresh_token" {
			http.Error(w, "wrong grant_type", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(OAuthTokens{
			AccessToken: "new-access-token",
			TokenType:   "Bearer",
			ExpiresIn:   3600,
		})
	}))
	defer srv.Close()

	st := newMockOAuthStore()
	r := NewRefresher(st, security.NewSafeClient(5*time.Second))

	serverID := uuid.New()
	tenantID := uuid.New()

	st.global[serverID.String()] = &store.MCPOAuthToken{
		ServerID:      serverID,
		TenantID:      tenantID,
		AccessToken:   "expired-token",
		RefreshToken:  "valid-refresh",
		TokenType:     "Bearer",
		ExpiresAt:     pastTime(time.Hour),
		TokenEndpoint: srv.URL,
	}

	got, err := r.GetValidToken(context.Background(), serverID, tenantID, "")
	if err != nil {
		t.Fatalf("GetValidToken() with refresh error: %v", err)
	}
	if got != "new-access-token" {
		t.Errorf("token = %q, want %q", got, "new-access-token")
	}
	if st.upserts == 0 {
		t.Error("expected UpsertOAuthToken to be called after refresh")
	}
}

func TestGetValidTokenRefreshFails(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
	}))
	defer srv.Close()

	st := newMockOAuthStore()
	r := NewRefresher(st, security.NewSafeClient(5*time.Second))

	serverID := uuid.New()
	tenantID := uuid.New()

	st.global[serverID.String()] = &store.MCPOAuthToken{
		ServerID:      serverID,
		TenantID:      tenantID,
		AccessToken:   "expired-token",
		RefreshToken:  "bad-refresh",
		ExpiresAt:     pastTime(time.Hour),
		TokenEndpoint: srv.URL,
	}

	_, err := r.GetValidToken(context.Background(), serverID, tenantID, "")
	if err == nil {
		t.Fatal("expected error when refresh fails, got nil")
	}
}

func TestInvalidateCacheEvictsEntry(t *testing.T) {
	st := newMockOAuthStore()
	r := NewRefresher(st, http.DefaultClient)

	serverID := uuid.New()
	tenantID := uuid.New()

	tok := &store.MCPOAuthToken{
		ServerID:    serverID,
		TenantID:    tenantID,
		AccessToken: "first-token",
		TokenType:   "Bearer",
		ExpiresAt:   futureTime(2 * time.Hour),
	}
	st.global[serverID.String()] = tok

	// Load into cache.
	if _, err := r.GetValidToken(context.Background(), serverID, tenantID, ""); err != nil {
		t.Fatalf("GetValidToken() error: %v", err)
	}

	// Update store with new token.
	st.global[serverID.String()] = &store.MCPOAuthToken{
		ServerID:    serverID,
		TenantID:    tenantID,
		AccessToken: "updated-token",
		TokenType:   "Bearer",
		ExpiresAt:   futureTime(2 * time.Hour),
	}

	// Without invalidation, cache returns old token.
	got, _ := r.GetValidToken(context.Background(), serverID, tenantID, "")
	if got != "first-token" {
		t.Errorf("before invalidation: got %q, want %q", got, "first-token")
	}

	// Invalidate and retry.
	r.InvalidateCache(serverID, "")
	got, err := r.GetValidToken(context.Background(), serverID, tenantID, "")
	if err != nil {
		t.Fatalf("GetValidToken() after invalidate error: %v", err)
	}
	if got != "updated-token" {
		t.Errorf("after invalidation: got %q, want %q", got, "updated-token")
	}
}

func TestGetValidTokenPerUserVsGlobal(t *testing.T) {
	st := newMockOAuthStore()
	r := NewRefresher(st, http.DefaultClient)

	serverID := uuid.New()
	tenantID := uuid.New()
	userID := "user-123"

	// Global token.
	st.global[serverID.String()] = &store.MCPOAuthToken{
		ServerID:    serverID,
		TenantID:    tenantID,
		AccessToken: "global-token",
		TokenType:   "Bearer",
		ExpiresAt:   futureTime(time.Hour),
	}
	// Per-user token.
	st.perUser[serverID.String()+":"+userID] = &store.MCPOAuthToken{
		ServerID:    serverID,
		TenantID:    tenantID,
		UserID:      userID,
		AccessToken: "user-token",
		TokenType:   "Bearer",
		ExpiresAt:   futureTime(time.Hour),
	}

	globalTok, err := r.GetValidToken(context.Background(), serverID, tenantID, "")
	if err != nil {
		t.Fatalf("global GetValidToken() error: %v", err)
	}
	userTok, err := r.GetValidToken(context.Background(), serverID, tenantID, userID)
	if err != nil {
		t.Fatalf("per-user GetValidToken() error: %v", err)
	}

	if globalTok != "global-token" {
		t.Errorf("global token = %q, want %q", globalTok, "global-token")
	}
	if userTok != "user-token" {
		t.Errorf("per-user token = %q, want %q", userTok, "user-token")
	}
}
