package agent

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	mcpbridge "github.com/nextlevelbuilder/goclaw/internal/mcp"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ---- mock helpers for getUserMCPTools OAuth tests ----

// minimalMCPServerStore implements store.MCPServerStore with panics for all
// methods except GetUserCredentials, which is the only method called in
// getUserMCPTools.
type minimalMCPServerStore struct {
	userCreds         *store.MCPUserCredentials
	userCredsErr      error
	getUserCredsCalls int
}

func (m *minimalMCPServerStore) GetUserCredentials(_ context.Context, _ uuid.UUID, _ string) (*store.MCPUserCredentials, error) {
	m.getUserCredsCalls++
	return m.userCreds, m.userCredsErr
}

func (m *minimalMCPServerStore) CreateServer(_ context.Context, _ *store.MCPServerData) error {
	panic("not implemented")
}
func (m *minimalMCPServerStore) GetServer(_ context.Context, _ uuid.UUID) (*store.MCPServerData, error) {
	panic("not implemented")
}
func (m *minimalMCPServerStore) GetServerByName(_ context.Context, _ string) (*store.MCPServerData, error) {
	panic("not implemented")
}
func (m *minimalMCPServerStore) ListServers(_ context.Context) ([]store.MCPServerData, error) {
	panic("not implemented")
}
func (m *minimalMCPServerStore) UpdateServer(_ context.Context, _ uuid.UUID, _ map[string]any) error {
	panic("not implemented")
}
func (m *minimalMCPServerStore) DeleteServer(_ context.Context, _ uuid.UUID) error {
	panic("not implemented")
}
func (m *minimalMCPServerStore) GrantToAgent(_ context.Context, _ *store.MCPAgentGrant) error {
	panic("not implemented")
}
func (m *minimalMCPServerStore) RevokeFromAgent(_ context.Context, _, _ uuid.UUID) error {
	panic("not implemented")
}
func (m *minimalMCPServerStore) ListAgentGrants(_ context.Context, _ uuid.UUID) ([]store.MCPAgentGrant, error) {
	panic("not implemented")
}
func (m *minimalMCPServerStore) ListServerGrants(_ context.Context, _ uuid.UUID) ([]store.MCPAgentGrant, error) {
	panic("not implemented")
}
func (m *minimalMCPServerStore) GrantToUser(_ context.Context, _ *store.MCPUserGrant) error {
	panic("not implemented")
}
func (m *minimalMCPServerStore) RevokeFromUser(_ context.Context, _ uuid.UUID, _ string) error {
	panic("not implemented")
}
func (m *minimalMCPServerStore) CountAgentGrantsByServer(_ context.Context) (map[uuid.UUID]int, error) {
	panic("not implemented")
}
func (m *minimalMCPServerStore) ListAccessible(_ context.Context, _ uuid.UUID, _ string) ([]store.MCPAccessInfo, error) {
	panic("not implemented")
}
func (m *minimalMCPServerStore) CreateRequest(_ context.Context, _ *store.MCPAccessRequest) error {
	panic("not implemented")
}
func (m *minimalMCPServerStore) ListPendingRequests(_ context.Context) ([]store.MCPAccessRequest, error) {
	panic("not implemented")
}
func (m *minimalMCPServerStore) ReviewRequest(_ context.Context, _ uuid.UUID, _ bool, _, _ string) error {
	panic("not implemented")
}
func (m *minimalMCPServerStore) SetUserCredentials(_ context.Context, _ uuid.UUID, _ string, _ store.MCPUserCredentials) error {
	panic("not implemented")
}
func (m *minimalMCPServerStore) DeleteUserCredentials(_ context.Context, _ uuid.UUID, _ string) error {
	panic("not implemented")
}

// recordingOAuthProvider records calls to GetValidToken for assertion.
type recordingOAuthProvider struct {
	mu          sync.Mutex
	callCount   int
	returnToken string
	returnErr   error
}

func (r *recordingOAuthProvider) GetValidToken(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callCount++
	return r.returnToken, r.returnErr
}

// newTestPool creates a real *mcpbridge.Pool with short timeouts for unit tests.
// The pool's AcquireUser will fail (no real MCP server), but token injection
// happens before the pool call.
func newTestPool(t *testing.T) *mcpbridge.Pool {
	t.Helper()
	pool := mcpbridge.NewPool(mcpbridge.PoolConfig{
		MaxSize:            10,
		UserAcquireTimeout: 10 * time.Millisecond,
	})
	t.Cleanup(func() { pool.Stop() })
	return pool
}

// oauthSettings returns JSON settings with auth_type: oauth.
func oauthSettings() json.RawMessage {
	return json.RawMessage(`{"oauth":{"auth_type":"oauth"}}`)
}

// oauthServerData returns a minimal MCPServerData for an OAuth-configured HTTP
// server. Transport=http + unreachable URL causes AcquireUser to fail quickly
// (connection refused) without panicking — unlike stdio with empty command
// which would spawn a goroutine reading from a nil pipe.
func oauthServerData() store.MCPServerData {
	return store.MCPServerData{
		BaseModel: store.BaseModel{ID: uuid.New()},
		Name:      "oauth-server-" + uuid.New().String()[:8],
		Transport: "http",
		URL:       "http://127.0.0.1:1", // port 1 = connection refused immediately
		Settings:  oauthSettings(),
	}
}

// TestResolveActorUserID locks the actor-vs-context user-id resolution semantics
// that gate per-user MCP credential lookup (and other per-actor resources).
//
// Two bugs this helper fixes:
//
//  1. Group chats: gateway consumer rewrites UserID to a group-scope composite
//     ("group:<channel>:<chatID>") for shared memory. The Bitrix24 provisioner
//     stores MCPUserCredentials keyed by the real external user id (= SenderID).
//     Lookup with group composite always missed → MCP tools silently absent.
//
//  2. DM with merged contact (C1): gateway consumer rewrites DM UserID to the
//     tenant_user UUID when ContactCollector.ResolveTenantUserID succeeds.
//     Provisioner still stores by SenderID. Lookup with UUID misses → MCP
//     tools fail in DMs after contact merge.
//
// resolveActorUserID accepts channelType so Bitrix24 always recovers SenderID
// (covers both rewrite cases). Other channels retain group-only recovery.
func TestResolveActorUserID(t *testing.T) {
	cases := []struct {
		name        string
		userID      string
		senderID    string
		peerKind    string
		channelType string
		want        string
	}{
		// DM unmerged: UserID == SenderID. No rewrite happened.
		{
			name:        "dm_returns_user_id_unchanged",
			userID:      "99",
			senderID:    "99",
			peerKind:    "direct",
			channelType: "",
			want:        "99",
		},
		// Group: gateway overrides UserID with group composite for shared
		// memory. Helper must recover SenderID for actor-scoped lookups.
		{
			name:        "group_overrides_to_sender",
			userID:      "group:bitrix-synity:chat4838",
			senderID:    "99",
			peerKind:    "group",
			channelType: "",
			want:        "99",
		},
		// Discord guild composite ("guild:<id>:user:<sender>") is also a
		// group peer — fall back to SenderID for credential lookup.
		{
			name:        "discord_guild_overrides_to_sender",
			userID:      "guild:1234:user:5678",
			senderID:    "5678",
			peerKind:    "group",
			channelType: "",
			want:        "5678",
		},
		// Synthetic / system senders (ticker, notification) carry empty
		// SenderID. No per-user credentials exist for them — fall back to
		// UserID so the lookup still uses a sensible key.
		{
			name:        "group_with_empty_sender_falls_back_to_user_id",
			userID:      "group:bitrix-synity:chat4838",
			senderID:    "",
			peerKind:    "group",
			channelType: "",
			want:        "group:bitrix-synity:chat4838",
		},
		// Empty peer_kind defaults to direct semantics.
		{
			name:        "empty_peer_kind_treated_as_direct",
			userID:      "99",
			senderID:    "99",
			peerKind:    "",
			channelType: "",
			want:        "99",
		},
		// Future channel using a peer_kind we don't recognize must NOT be
		// treated as group automatically — DM semantics are the safer
		// default (no override).
		{
			name:        "unknown_peer_kind_does_not_override",
			userID:      "99",
			senderID:    "42",
			peerKind:    "channel",
			channelType: "",
			want:        "99",
		},

		// ── Bitrix24-specific cases (C1 fix) ───────────────────────────

		// Bitrix24 DM, sender NOT merged: UserID == SenderID. Helper returns
		// SenderID (which is identical) — same outcome either way.
		{
			name:        "bitrix24_dm_unmerged_uses_sender",
			userID:      "62",
			senderID:    "62",
			peerKind:    "direct",
			channelType: "bitrix24",
			want:        "62",
		},
		// Bitrix24 DM, sender MERGED to tenant_user (C1 bug): consumer
		// rewrites UserID to tenant_user UUID. Provisioner stored creds
		// by SenderID. Helper must return SenderID so lookup hits.
		{
			name:        "bitrix24_dm_merged_uses_sender_not_uuid",
			userID:      "uuid-abc-def-0123",
			senderID:    "62",
			peerKind:    "direct",
			channelType: "bitrix24",
			want:        "62",
		},
		// Bitrix24 group: same recovery as generic group, channelType
		// discriminator does no harm.
		{
			name:        "bitrix24_group_uses_sender",
			userID:      "group:bitrix-tamgiac:chat4686",
			senderID:    "62",
			peerKind:    "group",
			channelType: "bitrix24",
			want:        "62",
		},
		// Bitrix24 synthetic event (system/ticker) with no sender: fall
		// back to UserID. No creds exist anyway.
		{
			name:        "bitrix24_synthetic_no_sender_falls_back",
			userID:      "system",
			senderID:    "",
			peerKind:    "direct",
			channelType: "bitrix24",
			want:        "system",
		},

		// ── Other channel backward compat ──────────────────────────────

		// Telegram DM (no channelType match): keep original behavior.
		// Telegram doesn't provision per-user creds today; if it did, the
		// consumer's UserID rewrite for merged contacts would still apply
		// and Telegram support would be added here when introduced.
		{
			name:        "telegram_dm_unchanged",
			userID:      "user-456",
			senderID:    "789",
			peerKind:    "direct",
			channelType: "telegram",
			want:        "user-456",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveActorUserID(tc.userID, tc.senderID, tc.peerKind, tc.channelType)
			if got != tc.want {
				t.Errorf("resolveActorUserID(%q, %q, %q, %q) = %q; want %q",
					tc.userID, tc.senderID, tc.peerKind, tc.channelType, got, tc.want)
			}
		})
	}
}

// ---- getUserMCPTools OAuth token injection tests ----
//
// These tests verify that mcpOAuthTokenProvider.GetValidToken is called (or
// not) based on whether the MCP server settings declare auth_type=oauth.
// Pool.AcquireUser fails (no real MCP server), but GetValidToken is injected
// BEFORE the pool call, so we can assert on call count without needing a
// working MCP connection.

// TestGetUserMCPToolsOAuthTokenProviderCalled verifies that when a server's
// settings declare auth_type=oauth, GetValidToken is called for the user.
func TestGetUserMCPToolsOAuthTokenProviderCalled(t *testing.T) {
	pool := newTestPool(t)
	provider := &recordingOAuthProvider{returnToken: "test-bearer-token"}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	l := &Loop{
		tenantID:              uuid.New(),
		mcpPool:               pool,
		mcpStore:              &minimalMCPServerStore{},
		mcpUserCredSrvs:       []store.MCPAccessInfo{{Server: oauthServerData()}},
		mcpOAuthTokenProvider: provider,
	}

	_ = l.getUserMCPTools(ctx, "user-1")

	provider.mu.Lock()
	callCount := provider.callCount
	provider.mu.Unlock()

	if callCount == 0 {
		t.Error("expected GetValidToken to be called for OAuth-configured server, but it was not")
	}
}

// TestGetUserMCPToolsOAuthTokenNotCalledForNonOAuth verifies that when a
// server has no OAuth settings, the token provider is never invoked and the
// server is skipped (neither static nor OAuth creds).
func TestGetUserMCPToolsOAuthTokenNotCalledForNonOAuth(t *testing.T) {
	pool := newTestPool(t)
	provider := &recordingOAuthProvider{}

	l := &Loop{
		tenantID: uuid.New(),
		mcpPool:  pool,
		mcpStore: &minimalMCPServerStore{},
		mcpUserCredSrvs: []store.MCPAccessInfo{
			{Server: store.MCPServerData{
				BaseModel: store.BaseModel{ID: uuid.New()},
				Name:      "plain-server",
				Transport: "http",
				URL:       "http://127.0.0.1:1",
				// No settings → auth_type defaults to "" (not "oauth")
			}},
		},
		mcpOAuthTokenProvider: provider,
	}

	_ = l.getUserMCPTools(context.Background(), "user-1")

	provider.mu.Lock()
	callCount := provider.callCount
	provider.mu.Unlock()

	if callCount != 0 {
		t.Errorf("GetValidToken must not be called for non-OAuth server, called %d times", callCount)
	}
}

// TestGetUserMCPToolsOAuthNoTokenSkipsServer verifies that when GetValidToken
// returns no valid token (expired / not authorized yet), the OAuth server is
// SKIPPED entirely — it must NOT fall back to the shared server-level
// headers/api_key as Authorization (which would let an unauthorized user call
// tools with the common credential). GetValidToken is still consulted (proving
// we entered the OAuth branch) but no tools are produced.
func TestGetUserMCPToolsOAuthNoTokenSkipsServer(t *testing.T) {
	pool := newTestPool(t)
	expiredErr := errTokenExpiredForTest("oauth token expired")
	provider := &recordingOAuthProvider{
		returnToken: "",
		returnErr:   expiredErr,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	// Server carries a server-level API key — must NOT be used as a fallback.
	srv := oauthServerData()
	srv.APIKey = "shared-server-key"

	l := &Loop{
		tenantID:              uuid.New(),
		mcpPool:               pool,
		mcpStore:              &minimalMCPServerStore{},
		mcpUserCredSrvs:       []store.MCPAccessInfo{{Server: srv}},
		mcpOAuthTokenProvider: provider,
	}

	result := l.getUserMCPTools(ctx, "user-1")
	if result != nil {
		t.Errorf("expected nil tools when OAuth has no token (skip, no fallback), got %d tools", len(result))
	}
	provider.mu.Lock()
	calls := provider.callCount
	provider.mu.Unlock()
	if calls == 0 {
		t.Error("expected GetValidToken to be consulted before skipping the OAuth server")
	}
}

// TestGetUserMCPToolsOAuthIgnoresStaticCreds verifies that for an OAuth server
// the user's static per-user credentials are NOT consulted or used: even when the
// user has set static creds, an OAuth server without a valid token is skipped
// (the OAuth token is the sole credential). This closes the bypass where a user
// with static headers could call OAuth tools without authorizing.
func TestGetUserMCPToolsOAuthIgnoresStaticCreds(t *testing.T) {
	pool := newTestPool(t)
	provider := &recordingOAuthProvider{returnToken: ""} // not authorized yet

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	// Store has valid static creds for the user — must be ignored for OAuth servers.
	st := &minimalMCPServerStore{
		userCreds: &store.MCPUserCredentials{
			APIKey:  "user-static-key",
			Headers: map[string]string{"Authorization": "Bearer user-static-key"},
		},
	}

	l := &Loop{
		tenantID:              uuid.New(),
		mcpPool:               pool,
		mcpStore:              st,
		mcpUserCredSrvs:       []store.MCPAccessInfo{{Server: oauthServerData()}},
		mcpOAuthTokenProvider: provider,
	}

	result := l.getUserMCPTools(ctx, "user-1")
	if result != nil {
		t.Errorf("expected nil tools: OAuth server without token must be skipped even with static creds, got %d tools", len(result))
	}
	if st.getUserCredsCalls != 0 {
		t.Errorf("static GetUserCredentials must NOT be consulted for OAuth servers, called %d times", st.getUserCredsCalls)
	}
	provider.mu.Lock()
	calls := provider.callCount
	provider.mu.Unlock()
	if calls == 0 {
		t.Error("expected GetValidToken to be consulted for the OAuth server")
	}
}

// TestGetUserMCPToolsNilPoolEarlyReturn verifies that a nil pool causes the
// function to return immediately without calling the token provider.
func TestGetUserMCPToolsNilPoolEarlyReturn(t *testing.T) {
	provider := &recordingOAuthProvider{}

	l := &Loop{
		tenantID:              uuid.New(),
		mcpPool:               nil, // triggers early return
		mcpStore:              &minimalMCPServerStore{},
		mcpUserCredSrvs:       []store.MCPAccessInfo{{Server: oauthServerData()}},
		mcpOAuthTokenProvider: provider,
	}

	result := l.getUserMCPTools(context.Background(), "user-1")
	if result != nil {
		t.Errorf("expected nil when pool is nil, got %v", result)
	}
	if provider.callCount != 0 {
		t.Error("GetValidToken must not be called when pool is nil (early return path)")
	}
}

// errTokenExpiredForTest is a simple error type used to simulate ErrTokenExpired
// without importing the oauth package (avoids potential import cycles in tests).
type errTokenExpiredForTest string

func (e errTokenExpiredForTest) Error() string { return string(e) }
