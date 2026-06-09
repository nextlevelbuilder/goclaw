//go:build integration

package integration

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// --------------------------------------------------------------------------
// Upsert + read-back
// --------------------------------------------------------------------------

func TestMCPOAuthUpsertGlobalToken(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	serverID := seedMCPServer(t, db, tenantID)
	st := oauthStore(t)
	ctx := tenantCtx(tenantID)

	tok := &store.MCPOAuthToken{
		ServerID:      serverID,
		TenantID:      tenantID,
		UserID:        "", // global
		AccessToken:   "global-access-token",
		RefreshToken:  "global-refresh-token",
		TokenType:     "Bearer",
		DCRClientID:   "client-global",
		DCRIssuer:     "https://auth.example.com",
		TokenEndpoint: "https://auth.example.com/token",
	}
	if err := st.UpsertOAuthToken(ctx, tok); err != nil {
		t.Fatalf("UpsertOAuthToken() error: %v", err)
	}

	got, err := st.GetOAuthToken(ctx, serverID, tenantID)
	if err != nil {
		t.Fatalf("GetOAuthToken() error: %v", err)
	}
	if got == nil {
		t.Fatal("GetOAuthToken() returned nil, want token")
	}
	if got.AccessToken != "global-access-token" {
		t.Errorf("AccessToken = %q, want %q", got.AccessToken, "global-access-token")
	}
	if got.DCRClientID != "client-global" {
		t.Errorf("DCRClientID = %q, want %q", got.DCRClientID, "client-global")
	}
}

func TestMCPOAuthUpsertPerUserToken(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	serverID := seedMCPServer(t, db, tenantID)
	st := oauthStore(t)
	ctx := tenantCtx(tenantID)
	userID := "user-" + uuid.New().String()[:8]

	tok := &store.MCPOAuthToken{
		ServerID:      serverID,
		TenantID:      tenantID,
		UserID:        userID,
		AccessToken:   "user-access-token",
		RefreshToken:  "user-refresh-token",
		TokenType:     "Bearer",
		DCRClientID:   "client-user",
		DCRIssuer:     "https://auth.example.com",
		TokenEndpoint: "https://auth.example.com/token",
	}
	if err := st.UpsertOAuthToken(ctx, tok); err != nil {
		t.Fatalf("UpsertOAuthToken() error: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM mcp_oauth_tokens WHERE server_id = $1 AND user_id = $2", serverID, userID)
	})

	got, err := st.GetUserOAuthToken(ctx, serverID, tenantID, userID)
	if err != nil {
		t.Fatalf("GetUserOAuthToken() error: %v", err)
	}
	if got == nil {
		t.Fatal("GetUserOAuthToken() returned nil, want token")
	}
	if got.AccessToken != "user-access-token" {
		t.Errorf("AccessToken = %q, want %q", got.AccessToken, "user-access-token")
	}
	if got.UserID != userID {
		t.Errorf("UserID = %q, want %q", got.UserID, userID)
	}
}

// --------------------------------------------------------------------------
// Upsert idempotency (partial unique index)
// --------------------------------------------------------------------------

func TestMCPOAuthUpsertUpdatesExisting(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	serverID := seedMCPServer(t, db, tenantID)
	st := oauthStore(t)
	ctx := tenantCtx(tenantID)

	tok := &store.MCPOAuthToken{
		ServerID:      serverID,
		TenantID:      tenantID,
		AccessToken:   "first-token",
		TokenType:     "Bearer",
		DCRClientID:   "client-1",
		DCRIssuer:     "https://auth.example.com",
		TokenEndpoint: "https://auth.example.com/token",
	}
	if err := st.UpsertOAuthToken(ctx, tok); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	// Second upsert with different access token — should update, not insert.
	tok.AccessToken = "updated-token"
	if err := st.UpsertOAuthToken(ctx, tok); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	got, err := st.GetOAuthToken(ctx, serverID, tenantID)
	if err != nil {
		t.Fatalf("GetOAuthToken() error: %v", err)
	}
	if got.AccessToken != "updated-token" {
		t.Errorf("AccessToken = %q, want %q (updated)", got.AccessToken, "updated-token")
	}

	// Verify only 1 row in DB.
	var count int
	db.QueryRow("SELECT COUNT(*) FROM mcp_oauth_tokens WHERE server_id = $1 AND tenant_id = $2 AND user_id IS NULL",
		serverID, tenantID).Scan(&count)
	if count != 1 {
		t.Errorf("row count = %d, want 1 (no duplicate)", count)
	}
}

func TestMCPOAuthGlobalAndPerUserCoexist(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	serverID := seedMCPServer(t, db, tenantID)
	st := oauthStore(t)
	ctx := tenantCtx(tenantID)
	userID := "user-" + uuid.New().String()[:8]

	// Insert global token.
	if err := st.UpsertOAuthToken(ctx, &store.MCPOAuthToken{
		ServerID:      serverID,
		TenantID:      tenantID,
		UserID:        "",
		AccessToken:   "global-tok",
		TokenType:     "Bearer",
		DCRClientID:   "g",
		DCRIssuer:     "x",
		TokenEndpoint: "https://t.example.com/token",
	}); err != nil {
		t.Fatalf("upsert global: %v", err)
	}

	// Insert per-user token.
	if err := st.UpsertOAuthToken(ctx, &store.MCPOAuthToken{
		ServerID:      serverID,
		TenantID:      tenantID,
		UserID:        userID,
		AccessToken:   "user-tok",
		TokenType:     "Bearer",
		DCRClientID:   "u",
		DCRIssuer:     "x",
		TokenEndpoint: "https://t.example.com/token",
	}); err != nil {
		t.Fatalf("upsert user: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM mcp_oauth_tokens WHERE server_id = $1", serverID)
	})

	// Both should be readable independently.
	globalTok, _ := st.GetOAuthToken(ctx, serverID, tenantID)
	userTok, _ := st.GetUserOAuthToken(ctx, serverID, tenantID, userID)

	if globalTok == nil || globalTok.AccessToken != "global-tok" {
		t.Errorf("global token = %v", globalTok)
	}
	if userTok == nil || userTok.AccessToken != "user-tok" {
		t.Errorf("per-user token = %v", userTok)
	}

	// Verify 2 rows in DB.
	var count int
	db.QueryRow("SELECT COUNT(*) FROM mcp_oauth_tokens WHERE server_id = $1 AND tenant_id = $2",
		serverID, tenantID).Scan(&count)
	if count != 2 {
		t.Errorf("row count = %d, want 2", count)
	}
}

// --------------------------------------------------------------------------
// Encryption at rest
// --------------------------------------------------------------------------

func TestMCPOAuthEncryptionAtRest(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	serverID := seedMCPServer(t, db, tenantID)
	st := oauthStore(t)
	ctx := tenantCtx(tenantID)

	plainAccess := "plaintext-access-" + uuid.New().String()
	plainRefresh := "plaintext-refresh-" + uuid.New().String()

	tok := &store.MCPOAuthToken{
		ServerID:      serverID,
		TenantID:      tenantID,
		AccessToken:   plainAccess,
		RefreshToken:  plainRefresh,
		TokenType:     "Bearer",
		DCRClientID:   "enc-client",
		DCRIssuer:     "x",
		TokenEndpoint: "https://t.example.com/token",
	}
	if err := st.UpsertOAuthToken(ctx, tok); err != nil {
		t.Fatalf("UpsertOAuthToken() error: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM mcp_oauth_tokens WHERE server_id = $1", serverID)
	})

	// Read raw values from DB — should have aes-gcm: prefix (encrypted).
	var rawAccess, rawRefresh string
	err := db.QueryRow(
		"SELECT access_token, refresh_token FROM mcp_oauth_tokens WHERE server_id = $1 AND tenant_id = $2",
		serverID, tenantID,
	).Scan(&rawAccess, &rawRefresh)
	if err != nil {
		t.Fatalf("raw query: %v", err)
	}

	if !strings.HasPrefix(rawAccess, "aes-gcm:") {
		t.Errorf("access_token in DB not encrypted: %q", rawAccess[:min(30, len(rawAccess))])
	}
	if !strings.HasPrefix(rawRefresh, "aes-gcm:") {
		t.Errorf("refresh_token in DB not encrypted: %q", rawRefresh[:min(30, len(rawRefresh))])
	}

	// Store should decrypt transparently on read.
	got, err := st.GetOAuthToken(ctx, serverID, tenantID)
	if err != nil {
		t.Fatalf("GetOAuthToken() error: %v", err)
	}
	if got.AccessToken != plainAccess {
		t.Errorf("decrypted AccessToken = %q, want %q", got.AccessToken, plainAccess)
	}
	if got.RefreshToken != plainRefresh {
		t.Errorf("decrypted RefreshToken = %q, want %q", got.RefreshToken, plainRefresh)
	}
}

// min helper for Go versions without builtin min.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// --------------------------------------------------------------------------
// Delete
// --------------------------------------------------------------------------

func TestMCPOAuthDeleteGlobal(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	serverID := seedMCPServer(t, db, tenantID)
	seedMCPOAuthToken(t, db, tenantID, serverID, "")
	st := oauthStore(t)
	ctx := tenantCtx(tenantID)

	if err := st.DeleteOAuthToken(ctx, serverID, tenantID); err != nil {
		t.Fatalf("DeleteOAuthToken() error: %v", err)
	}

	got, err := st.GetOAuthToken(ctx, serverID, tenantID)
	if err != nil {
		t.Fatalf("GetOAuthToken() after delete error: %v", err)
	}
	if got != nil {
		t.Error("expected nil after delete, got token")
	}
}

func TestMCPOAuthDeletePerUser(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	serverID := seedMCPServer(t, db, tenantID)
	userID := "user-del-" + uuid.New().String()[:8]

	// Seed both global and per-user.
	seedMCPOAuthToken(t, db, tenantID, serverID, "")
	seedMCPOAuthToken(t, db, tenantID, serverID, userID)
	st := oauthStore(t)
	ctx := tenantCtx(tenantID)

	// Delete per-user only.
	if err := st.DeleteUserOAuthToken(ctx, serverID, tenantID, userID); err != nil {
		t.Fatalf("DeleteUserOAuthToken() error: %v", err)
	}

	// Per-user should be gone.
	userTok, _ := st.GetUserOAuthToken(ctx, serverID, tenantID, userID)
	if userTok != nil {
		t.Error("expected per-user token deleted, still exists")
	}

	// Global should remain.
	globalTok, _ := st.GetOAuthToken(ctx, serverID, tenantID)
	if globalTok == nil {
		t.Error("expected global token to remain after per-user delete")
	}
}

func TestMCPOAuthDeleteServerTokens(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	serverID := seedMCPServer(t, db, tenantID)
	otherServerID := seedMCPServer(t, db, tenantID)
	userA := "user-a-" + uuid.New().String()[:8]
	userB := "user-b-" + uuid.New().String()[:8]

	// Seed global + two per-user tokens for the target server.
	seedMCPOAuthToken(t, db, tenantID, serverID, "")
	seedMCPOAuthToken(t, db, tenantID, serverID, userA)
	seedMCPOAuthToken(t, db, tenantID, serverID, userB)
	// Seed an unrelated token on a different server in the same tenant.
	seedMCPOAuthToken(t, db, tenantID, otherServerID, "")

	st := oauthStore(t)
	ctx := tenantCtx(tenantID)

	if err := st.DeleteServerOAuthTokens(ctx, serverID, tenantID); err != nil {
		t.Fatalf("DeleteServerOAuthTokens() error: %v", err)
	}

	// All tokens for the target server must be gone (global + both per-user).
	if tok, _ := st.GetOAuthToken(ctx, serverID, tenantID); tok != nil {
		t.Error("expected global token deleted")
	}
	if tok, _ := st.GetUserOAuthToken(ctx, serverID, tenantID, userA); tok != nil {
		t.Error("expected per-user token (userA) deleted")
	}
	if tok, _ := st.GetUserOAuthToken(ctx, serverID, tenantID, userB); tok != nil {
		t.Error("expected per-user token (userB) deleted")
	}

	// The other server's token must remain untouched.
	if tok, _ := st.GetOAuthToken(ctx, otherServerID, tenantID); tok == nil {
		t.Error("token for a different server must not be deleted")
	}
}

// --------------------------------------------------------------------------
// Tenant isolation
// --------------------------------------------------------------------------

func TestMCPOAuthTenantIsolation(t *testing.T) {
	db := testDB(t)
	tenantA, _ := seedTenantAgent(t, db)
	tenantB, _ := seedTenantAgent(t, db)

	// Create server for tenant A.
	serverID := seedMCPServer(t, db, tenantA)

	// Seed token for tenant A.
	seedMCPOAuthToken(t, db, tenantA, serverID, "")

	st := oauthStore(t)

	// Tenant A can read its own token.
	tokA, err := st.GetOAuthToken(tenantCtx(tenantA), serverID, tenantA)
	if err != nil {
		t.Fatalf("GetOAuthToken(tenantA): %v", err)
	}
	if tokA == nil {
		t.Error("tenant A should see its own token")
	}

	// Tenant B cannot read tenant A's token (different tenantID in query).
	tokB, err := st.GetOAuthToken(tenantCtx(tenantB), serverID, tenantB)
	if err != nil {
		t.Fatalf("GetOAuthToken(tenantB): %v", err)
	}
	if tokB != nil {
		t.Error("tenant B should NOT see tenant A's token")
	}
}

// --------------------------------------------------------------------------
// Cascade on server delete
// --------------------------------------------------------------------------

func TestMCPOAuthCascadeOnServerDelete(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	serverID := seedMCPServer(t, db, tenantID)
	seedMCPOAuthToken(t, db, tenantID, serverID, "")

	st := oauthStore(t)
	ctx := tenantCtx(tenantID)

	// Verify token exists.
	tok, _ := st.GetOAuthToken(ctx, serverID, tenantID)
	if tok == nil {
		t.Fatal("token should exist before server delete")
	}

	// Delete server — ON DELETE CASCADE should delete token.
	if _, err := db.Exec("DELETE FROM mcp_servers WHERE id = $1", serverID); err != nil {
		t.Fatalf("delete server: %v", err)
	}

	// Token should be gone.
	var count int
	db.QueryRow("SELECT COUNT(*) FROM mcp_oauth_tokens WHERE server_id = $1", serverID).Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 tokens after server delete (CASCADE), got %d", count)
	}
}

// --------------------------------------------------------------------------
// ExpiresAt handling
// --------------------------------------------------------------------------

func TestMCPOAuthExpiresAtRoundTrip(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	serverID := seedMCPServer(t, db, tenantID)
	st := oauthStore(t)
	ctx := tenantCtx(tenantID)

	expiresAt := time.Now().Add(time.Hour).Truncate(time.Second).UTC()
	tok := &store.MCPOAuthToken{
		ServerID:      serverID,
		TenantID:      tenantID,
		AccessToken:   "tok",
		TokenType:     "Bearer",
		ExpiresAt:     &expiresAt,
		DCRClientID:   "c",
		DCRIssuer:     "x",
		TokenEndpoint: "https://t.example.com/token",
	}
	if err := st.UpsertOAuthToken(ctx, tok); err != nil {
		t.Fatalf("UpsertOAuthToken() error: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM mcp_oauth_tokens WHERE server_id = $1", serverID)
	})

	got, err := st.GetOAuthToken(ctx, serverID, tenantID)
	if err != nil {
		t.Fatalf("GetOAuthToken() error: %v", err)
	}
	if got.ExpiresAt == nil {
		t.Fatal("ExpiresAt should not be nil")
	}
	if !got.ExpiresAt.Equal(expiresAt) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, expiresAt)
	}
}

func TestMCPOAuthNullRefreshToken(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	serverID := seedMCPServer(t, db, tenantID)
	st := oauthStore(t)
	ctx := tenantCtx(tenantID)

	tok := &store.MCPOAuthToken{
		ServerID:      serverID,
		TenantID:      tenantID,
		AccessToken:   "access-only",
		RefreshToken:  "", // no refresh token
		TokenType:     "Bearer",
		DCRClientID:   "c",
		DCRIssuer:     "x",
		TokenEndpoint: "https://t.example.com/token",
	}
	if err := st.UpsertOAuthToken(ctx, tok); err != nil {
		t.Fatalf("UpsertOAuthToken() error: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM mcp_oauth_tokens WHERE server_id = $1", serverID)
	})

	got, _ := st.GetOAuthToken(ctx, serverID, tenantID)
	if got == nil {
		t.Fatal("GetOAuthToken() returned nil")
	}
	if got.RefreshToken != "" {
		t.Errorf("RefreshToken = %q, want empty string (no refresh token)", got.RefreshToken)
	}

	// DB should not store encrypted empty string — check raw column.
	var rawRefresh sql.NullString
	db.QueryRow("SELECT refresh_token FROM mcp_oauth_tokens WHERE server_id = $1", serverID).Scan(&rawRefresh)
	if rawRefresh.Valid && strings.HasPrefix(rawRefresh.String, "aes-gcm:") {
		t.Error("empty refresh_token should not be stored as encrypted value in DB")
	}
}
