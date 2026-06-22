package bitrix24

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// fakeMCPStore implements store.MCPServerStore for provisioner tests.
// Only the methods provisionIfMissing actually exercises are implemented
// with behaviour — the rest satisfy the interface with zero returns so the
// test file compiles cleanly.
//
// All tests that touch fakeMCPStore pass a *preloaded* server row because
// GetServerByName is what initMCPProvisioner calls at startup; the other
// fields exist to be mutated by SetUserCredentials and observed by the
// test assertions.
type fakeMCPStore struct {
	mu sync.Mutex

	serversByName map[string]*store.MCPServerData
	userCreds     map[string]store.MCPUserCredentials // key = serverID + ":" + userID

	getUserCallCount int
	setUserCallCount int
}

func newFakeMCPStore() *fakeMCPStore {
	return &fakeMCPStore{
		serversByName: map[string]*store.MCPServerData{},
		userCreds:     map[string]store.MCPUserCredentials{},
	}
}

func credKey(serverID uuid.UUID, userID string) string {
	return serverID.String() + ":" + userID
}

// --- MCPServerStore methods that matter for the provisioner --------------

func (f *fakeMCPStore) GetServerByName(_ context.Context, name string) (*store.MCPServerData, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.serversByName[name]; ok {
		return s, nil
	}
	return nil, nil // partner's contract: nil + nil when absent
}

func (f *fakeMCPStore) GetUserCredentials(_ context.Context, serverID uuid.UUID, userID string) (*store.MCPUserCredentials, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getUserCallCount++
	if c, ok := f.userCreds[credKey(serverID, userID)]; ok {
		return &c, nil
	}
	return nil, nil
}

func (f *fakeMCPStore) SetUserCredentials(_ context.Context, serverID uuid.UUID, userID string, creds store.MCPUserCredentials) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setUserCallCount++
	f.userCreds[credKey(serverID, userID)] = creds
	return nil
}

// --- MCPServerStore methods the provisioner doesn't touch ----------------

func (f *fakeMCPStore) CreateServer(_ context.Context, _ *store.MCPServerData) error {
	return nil
}
func (f *fakeMCPStore) GetServer(_ context.Context, _ uuid.UUID) (*store.MCPServerData, error) {
	return nil, nil
}
func (f *fakeMCPStore) ListServers(_ context.Context) ([]store.MCPServerData, error) { return nil, nil }
func (f *fakeMCPStore) UpdateServer(_ context.Context, _ uuid.UUID, _ map[string]any) error {
	return nil
}
func (f *fakeMCPStore) DeleteServer(_ context.Context, _ uuid.UUID) error { return nil }
func (f *fakeMCPStore) GrantToAgent(_ context.Context, _ *store.MCPAgentGrant) error {
	return nil
}
func (f *fakeMCPStore) RevokeFromAgent(_ context.Context, _, _ uuid.UUID) error { return nil }
func (f *fakeMCPStore) ListAgentGrants(_ context.Context, _ uuid.UUID) ([]store.MCPAgentGrant, error) {
	return nil, nil
}
func (f *fakeMCPStore) ListServerGrants(_ context.Context, _ uuid.UUID) ([]store.MCPAgentGrant, error) {
	return nil, nil
}
func (f *fakeMCPStore) GrantToUser(_ context.Context, _ *store.MCPUserGrant) error { return nil }
func (f *fakeMCPStore) RevokeFromUser(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}
func (f *fakeMCPStore) CountAgentGrantsByServer(_ context.Context) (map[uuid.UUID]int, error) {
	return nil, nil
}
func (f *fakeMCPStore) ListAccessible(_ context.Context, _ uuid.UUID, _ string) ([]store.MCPAccessInfo, error) {
	return nil, nil
}
func (f *fakeMCPStore) CreateRequest(_ context.Context, _ *store.MCPAccessRequest) error {
	return nil
}
func (f *fakeMCPStore) ListPendingRequests(_ context.Context) ([]store.MCPAccessRequest, error) {
	return nil, nil
}
func (f *fakeMCPStore) ReviewRequest(_ context.Context, _ uuid.UUID, _ bool, _, _ string) error {
	return nil
}
func (f *fakeMCPStore) DeleteUserCredentials(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}

// --- Test helpers --------------------------------------------------------

// newProvisionerTestChannel builds a started Channel with provisioning
// enabled against the given (fake MCP server URL, fake MCP store). Used
// by the happy-path tests that need the full provisioner wired up.
func newProvisionerTestChannel(t *testing.T, mcpStore *fakeMCPStore, mcpBaseURL string, botType string) *Channel {
	t.Helper()

	fs := newFakeStore()
	resetWebhookRouterForTest()
	t.Cleanup(resetWebhookRouterForTest)

	// Seed the fake MCP store so initMCPProvisioner's GetServerByName
	// succeeds. ID doesn't need to match anything real — provisioner just
	// caches and passes it through.
	serverID := uuid.New()
	mcpStore.serversByName["bitrix-mcp"] = &store.MCPServerData{
		BaseModel: store.BaseModel{ID: serverID},
		Name:      "bitrix-mcp",
	}

	fn := FactoryWithPortalStoreAndMCP(fs, mcpStore, "")
	cfgJSON := `{"portal":"p","bot_code":"c","bot_name":"n","bot_type":"` + botType +
		`","mcp_server_name":"bitrix-mcp","mcp_base_url":"` + mcpBaseURL + `"}`
	ch, err := fn("b1", nil, json.RawMessage(cfgJSON), bus.New(), nil)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	bc := ch.(*Channel)
	bc.SetTenantID(store.GenNewID())

	// Bypass Start() — set the wiring initMCPProvisioner would set.
	// Calling Start() would also try to hit Bitrix portal which we don't
	// want in these focused tests.
	bc.startMu.Lock()
	bc.botID = 1
	bc.client = NewClient("p.bitrix24.com", nil)
	bc.startMu.Unlock()

	if err := bc.initMCPProvisioner(context.Background()); err != nil {
		t.Fatalf("initMCPProvisioner: %v", err)
	}
	return bc
}

// validAuth returns an EventAuth with every field provisionIfMissing
// needs populated so individual tests can focus on the outcome, not
// fixture plumbing.
func validAuth() EventAuth {
	return EventAuth{
		Domain:       "acme.bitrix24.com",
		AccessToken:  "at-tok",
		RefreshToken: "rt-tok",
		ExpiresIn:    3600,
	}
}

// --- Tests ---------------------------------------------------------------

// TestProvisionIfMissing_OpenChannelBot_Skipped verifies bot_type=O short-
// circuits the provisioner without touching the MCP store. This is a
// Phase C skip (not failure) and is the expected outcome for every
// message delivered to an Open Channel bot.
func TestProvisionIfMissing_OpenChannelBot_Skipped(t *testing.T) {
	fs := newFakeStore()
	resetWebhookRouterForTest()
	defer resetWebhookRouterForTest()

	mcpStore := newFakeMCPStore()
	// Don't seed a server — if provisioner tries to use it we'll catch the
	// mistake via the nil-check, but this test is really about the early
	// IsOpenChannelBot() exit.

	fn := FactoryWithPortalStoreAndMCP(fs, mcpStore, "")
	ch, err := fn("b1", nil, json.RawMessage(`{"portal":"p","bot_code":"c","bot_name":"n","bot_type":"O","mcp_server_name":"bitrix-mcp","mcp_base_url":"http://example.test"}`),
		bus.New(), nil)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	bc := ch.(*Channel)

	// IS_CONNECTOR=Y (external connector customer) → skipped: not a Bitrix user.
	err = bc.provisionIfMissing(context.Background(), "42", true, validAuth())
	if !errors.Is(err, ErrProvisionSkippedOpenChannel) {
		t.Fatalf("connector message: err = %v; want ErrProvisionSkippedOpenChannel", err)
	}
	if mcpStore.getUserCallCount != 0 {
		t.Errorf("Open Channel connector message must not hit MCP store; got %d GetUserCredentials calls", mcpStore.getUserCallCount)
	}

	// IS_CONNECTOR=N (internal staff in an Open Channel) → must NOT connector-skip.
	if err := bc.provisionIfMissing(context.Background(), "42", false, validAuth()); errors.Is(err, ErrProvisionSkippedOpenChannel) {
		t.Fatalf("internal staff (IS_CONNECTOR=N) must not be connector-skipped; got %v", err)
	}
}

// TestProvisionIfMissing_Disabled verifies a channel built via the
// no-MCP factory variant (or with half-config) reports Disabled without
// touching anything external.
func TestProvisionIfMissing_Disabled(t *testing.T) {
	fs := newFakeStore()
	resetWebhookRouterForTest()
	defer resetWebhookRouterForTest()

	// FactoryWithPortalStore (2-arg) leaves mcpStore nil → provisioning
	// stays disabled regardless of config fields.
	fn := FactoryWithPortalStore(fs, "")
	ch, err := fn("b1", nil, json.RawMessage(`{"portal":"p","bot_code":"c","bot_name":"n"}`),
		bus.New(), nil)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	bc := ch.(*Channel)

	err = bc.provisionIfMissing(context.Background(), "42", false, validAuth())
	if !errors.Is(err, ErrProvisionDisabled) {
		t.Fatalf("err = %v; want ErrProvisionDisabled", err)
	}
}

// TestProvisionIfMissing_ExistingCreds_NoHTTP ensures the warm path skips
// auto-onboard entirely when the user already has credentials. This is
// the hottest path — all subsequent messages from the same user — so
// verifying it does NOT re-hit the HTTP endpoint protects against a
// regression that'd show up as "burning 1 HTTP request per message".
func TestProvisionIfMissing_ExistingCreds_NoHTTP(t *testing.T) {
	httpCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpCalls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	mcpStore := newFakeMCPStore()
	bc := newProvisionerTestChannel(t, mcpStore, srv.URL, "B")

	// Preload credentials for user 42 before the first provision attempt.
	// Warm path requires BITRIX_EXPIRES_AT meta (added 260512 C3 fix). Legacy
	// rows without expiry meta now actively refresh on next event to write the
	// meta column — see TestProvisionIfMissing_LegacyNoExpiry_RefreshHTTP.
	warmExpiry := time.Now().UTC().Add(30 * time.Minute).Format(time.RFC3339)
	mcpStore.userCreds[credKey(bc.mcpServerID, "42")] = store.MCPUserCredentials{
		APIKey: "prior-key",
		Env: map[string]string{
			"BITRIX_EXPIRES_AT": warmExpiry,
		},
	}

	if err := bc.provisionIfMissing(context.Background(), "42", false, validAuth()); err != nil {
		t.Fatalf("err = %v; want nil", err)
	}
	if httpCalls != 0 {
		t.Errorf("warm path must not call auto-onboard; got %d HTTP calls", httpCalls)
	}
	if mcpStore.setUserCallCount != 0 {
		t.Errorf("warm path must not re-persist; got %d SetUserCredentials calls", mcpStore.setUserCallCount)
	}
}

// TestProvisionIfMissing_NearExpiry_RefreshHTTP locks the C3 (Phase 3) fix:
// when cached BITRIX_EXPIRES_AT is within mcpCredsRefreshWindow (5 min) of
// now, the provisioner MUST refresh proactively — preventing the upcoming
// tool call from racing a stale token. Without this, a user chatting just
// before expiry would hit 401 on the tool call and need a follow-up message
// to recover.
func TestProvisionIfMissing_NearExpiry_RefreshHTTP(t *testing.T) {
	httpCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"api_key":"refreshed-key","user_id":"u-42","tenant_id":"t-1","created":false}`))
	}))
	defer srv.Close()

	mcpStore := newFakeMCPStore()
	bc := newProvisionerTestChannel(t, mcpStore, srv.URL, "B")

	// Preload creds with expiry 2 minutes in the future (inside refresh window).
	nearExpiry := time.Now().UTC().Add(2 * time.Minute).Format(time.RFC3339)
	mcpStore.userCreds[credKey(bc.mcpServerID, "42")] = store.MCPUserCredentials{
		APIKey: "stale-key",
		Env: map[string]string{
			"BITRIX_EXPIRES_AT": nearExpiry,
		},
	}

	if err := bc.provisionIfMissing(context.Background(), "42", false, validAuth()); err != nil {
		t.Fatalf("err = %v; want nil", err)
	}
	if httpCalls != 1 {
		t.Errorf("near-expiry path must refresh once; got %d HTTP calls", httpCalls)
	}
	if mcpStore.setUserCallCount != 1 {
		t.Errorf("near-expiry path must persist refreshed creds; got %d SetUserCredentials calls", mcpStore.setUserCallCount)
	}
}

// TestProvisionIfMissing_LegacyNoExpiry_RefreshHTTP locks the 260512 fix:
// rows without BITRIX_EXPIRES_AT meta (legacy onboards before C3) MUST be
// refreshed once to write expiry meta. Without this, mcp-bx-syn rejects with
// 401 when its stored token expires (1h TTL after onboard) → loop-side
// purge breaks the in-flight conversation (observed user 1 group chat 2150).
func TestProvisionIfMissing_LegacyNoExpiry_RefreshHTTP(t *testing.T) {
	httpCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"api_key":"refreshed-key","user_id":"u-42","tenant_id":"t-1","created":false}`))
	}))
	defer srv.Close()

	mcpStore := newFakeMCPStore()
	bc := newProvisionerTestChannel(t, mcpStore, srv.URL, "B")

	// Legacy row: APIKey set, NO BITRIX_EXPIRES_AT.
	mcpStore.userCreds[credKey(bc.mcpServerID, "42")] = store.MCPUserCredentials{
		APIKey: "legacy-key",
	}

	if err := bc.provisionIfMissing(context.Background(), "42", false, validAuth()); err != nil {
		t.Fatalf("err = %v; want nil", err)
	}
	if httpCalls != 1 {
		t.Errorf("legacy path must refresh once; got %d HTTP calls", httpCalls)
	}
	if mcpStore.setUserCallCount != 1 {
		t.Errorf("legacy path must persist refreshed creds; got %d SetUserCredentials", mcpStore.setUserCallCount)
	}
}

// TestProvisionIfMissing_WarmExpiry_NoHTTP locks the inverse of C3: when
// cached expiry is comfortably beyond the refresh window (e.g. 30 min away),
// provisioner MUST skip HTTP. Refreshing on every event when token is still
// fresh would burn 1 HTTP per message and DDoS mcp-bx-syn.
func TestProvisionIfMissing_WarmExpiry_NoHTTP(t *testing.T) {
	httpCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpCalls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	mcpStore := newFakeMCPStore()
	bc := newProvisionerTestChannel(t, mcpStore, srv.URL, "B")

	warmExpiry := time.Now().UTC().Add(30 * time.Minute).Format(time.RFC3339)
	mcpStore.userCreds[credKey(bc.mcpServerID, "42")] = store.MCPUserCredentials{
		APIKey: "warm-key",
		Env: map[string]string{
			"BITRIX_EXPIRES_AT": warmExpiry,
		},
	}

	if err := bc.provisionIfMissing(context.Background(), "42", false, validAuth()); err != nil {
		t.Fatalf("err = %v; want nil", err)
	}
	if httpCalls != 0 {
		t.Errorf("warm-expiry path must not call HTTP; got %d HTTP calls", httpCalls)
	}
}

// TestProvisionIfMissing_MintAndPersist covers the full happy path: no
// existing creds → auto-onboard call → credential saved with OAuth tokens
// in Env. This is the core value-add of Phase C.
func TestProvisionIfMissing_MintAndPersist(t *testing.T) {
	var gotReq autoOnboardRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"api_key":"minted-k","user_id":"mu","tenant_id":"mt","created":true}`))
	}))
	defer srv.Close()

	mcpStore := newFakeMCPStore()
	bc := newProvisionerTestChannel(t, mcpStore, srv.URL, "B")

	before := time.Now()
	err := bc.provisionIfMissing(context.Background(), "42", false, validAuth())
	if err != nil {
		t.Fatalf("provisionIfMissing: %v", err)
	}

	// Request body reached the MCP server verbatim.
	if gotReq.Domain != "acme.bitrix24.com" || gotReq.BitrixUserID != "42" ||
		gotReq.AccessToken != "at-tok" || gotReq.RefreshToken != "rt-tok" {
		t.Errorf("unexpected request to MCP server: %+v", gotReq)
	}

	// Credential was persisted with API key + OAuth Env.
	stored, ok := mcpStore.userCreds[credKey(bc.mcpServerID, "42")]
	if !ok {
		t.Fatalf("credentials were not persisted")
	}
	if stored.APIKey != "minted-k" {
		t.Errorf("APIKey = %q; want minted-k", stored.APIKey)
	}
	for _, key := range []string{"BITRIX_DOMAIN", "BITRIX_ACCESS_TOKEN", "BITRIX_REFRESH_TOKEN", "BITRIX_EXPIRES_AT"} {
		if _, has := stored.Env[key]; !has {
			t.Errorf("Env missing %q (full Env: %v)", key, stored.Env)
		}
	}
	if stored.Env["BITRIX_DOMAIN"] != "acme.bitrix24.com" {
		t.Errorf("Env[BITRIX_DOMAIN] = %q; want acme.bitrix24.com", stored.Env["BITRIX_DOMAIN"])
	}

	// Sanity-check EXPIRES_AT is ~now + expires_in, not some fossil.
	parsed, err := time.Parse(time.RFC3339, stored.Env["BITRIX_EXPIRES_AT"])
	if err != nil {
		t.Fatalf("BITRIX_EXPIRES_AT not RFC3339: %v", err)
	}
	expectedMin := before.Add(3599 * time.Second)
	expectedMax := before.Add(3601 * time.Second)
	if parsed.Before(expectedMin) || parsed.After(expectedMax) {
		t.Errorf("BITRIX_EXPIRES_AT out of expected window (%v–%v): got %v",
			expectedMin, expectedMax, parsed)
	}
}

// TestProvisionIfMissing_Debounce verifies that after one attempt (success
// OR failure), a second attempt within mcpProvisionDebounceTTL returns
// ErrProvisionDebounced without calling the MCP server again. Critical
// guard against Bitrix24 webhook retry storms.
func TestProvisionIfMissing_Debounce(t *testing.T) {
	httpCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"api_key":"k","user_id":"u","tenant_id":"t","created":true}`))
	}))
	defer srv.Close()

	mcpStore := newFakeMCPStore()
	bc := newProvisionerTestChannel(t, mcpStore, srv.URL, "B")

	// First attempt succeeds and marks the debounce.
	if err := bc.provisionIfMissing(context.Background(), "42", false, validAuth()); err != nil {
		t.Fatalf("first attempt: %v", err)
	}
	if httpCalls != 1 {
		t.Fatalf("first attempt should call HTTP once, got %d", httpCalls)
	}

	// Second attempt within TTL → debounced. We need to wipe the stored
	// credential first; otherwise the "existing creds" short-circuit
	// returns nil before the debounce check fires. (This ordering is
	// documented in provisionIfMissing; the test makes it observable.)
	mcpStore.mu.Lock()
	delete(mcpStore.userCreds, credKey(bc.mcpServerID, "42"))
	mcpStore.mu.Unlock()

	err := bc.provisionIfMissing(context.Background(), "42", false, validAuth())
	if !errors.Is(err, ErrProvisionDebounced) {
		t.Fatalf("second attempt: %v; want ErrProvisionDebounced", err)
	}
	if httpCalls != 1 {
		t.Errorf("debounced attempt must not hit HTTP; got %d total calls", httpCalls)
	}
}

// TestProvisionIfMissing_HTTPFailure_Surfaces ensures auto-onboard errors
// propagate out of provisionIfMissing (wrapped, but distinguishable). The
// caller in handle.go swallows these after logging, but tests need to see
// them to assert the error path exists.
func TestProvisionIfMissing_HTTPFailure_Surfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_bitrix_user"}`))
	}))
	defer srv.Close()

	mcpStore := newFakeMCPStore()
	bc := newProvisionerTestChannel(t, mcpStore, srv.URL, "B")

	err := bc.provisionIfMissing(context.Background(), "42", false, validAuth())
	if err == nil {
		t.Fatal("401 from MCP must produce an error")
	}
	// We don't assert exact string, but do check the caller can tell this
	// is NOT one of the expected no-op sentinels — otherwise handle.go
	// would misclassify it as "skipped" and hide the 401 in Debug logs.
	if errors.Is(err, ErrProvisionDisabled) ||
		errors.Is(err, ErrProvisionDebounced) ||
		errors.Is(err, ErrProvisionSkippedOpenChannel) {
		t.Errorf("HTTP 401 should not match any no-op sentinel; got %v", err)
	}
	// Underlying message should mention the status so operators can debug.
	if !strings.Contains(err.Error(), "auto-onboard failed") {
		t.Errorf("err = %v; expected wrapped 'auto-onboard failed' prefix", err)
	}
}

// TestProvisionIfMissing_MissingAuthBlock catches the case where the
// event somehow reached handleMessage with partial auth data. Should
// fail BEFORE calling the MCP server (saves an HTTP round-trip that
// we know will fail validation client-side).
func TestProvisionIfMissing_MissingAuthBlock(t *testing.T) {
	httpCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpCalls++
	}))
	defer srv.Close()

	mcpStore := newFakeMCPStore()
	bc := newProvisionerTestChannel(t, mcpStore, srv.URL, "B")

	cases := []struct {
		name string
		auth EventAuth
	}{
		{"empty_domain", EventAuth{AccessToken: "a", RefreshToken: "r"}},
		{"empty_access_token", EventAuth{Domain: "d", RefreshToken: "r"}},
		{"empty_refresh_token", EventAuth{Domain: "d", AccessToken: "a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := httpCalls
			// Use a fresh userID per subcase so the debounce from a prior
			// case doesn't mask a regression.
			err := bc.provisionIfMissing(context.Background(), tc.name, false, tc.auth)
			if err == nil {
				t.Fatalf("missing %s should fail", tc.name)
			}
			if httpCalls != before {
				t.Errorf("incomplete auth must not hit HTTP; got +%d calls", httpCalls-before)
			}
		})
	}
}

// TestInitMCPProvisioner_DisabledModes covers the configurations that
// leave the provisioner off at startup (all non-fatal):
//   - nil MCPServerStore
//   - mcp_server_name points at a server that doesn't exist in the store
//
// (Half-config — only one of mcp_server_name / mcp_base_url set —
// is rejected earlier at factory load; see TestFactory_HalfConfigRejected.
// Both-empty is accepted with provisioning off, covered by
// TestProvisionIfMissing_Disabled.)
//
// Each case should leave mcpClient nil + mcpServerID zero, so
// provisionIfMissing returns ErrProvisionDisabled.
//
// Auto-onboard auth note: there is no longer an admin-token branch to test —
// the MCP server authenticates each /api/auto-onboard call via the
// caller-supplied Bitrix access_token, not a shared bearer.
func TestInitMCPProvisioner_DisabledModes(t *testing.T) {
	t.Run("nil_mcp_store", func(t *testing.T) {
		fs := newFakeStore()
		resetWebhookRouterForTest()
		defer resetWebhookRouterForTest()

		fn := FactoryWithPortalStoreAndMCP(fs, nil, "")
		ch, _ := fn("b1", nil, json.RawMessage(`{"portal":"p","bot_code":"c","bot_name":"n","mcp_server_name":"x","mcp_base_url":"http://x"}`),
			bus.New(), nil)
		bc := ch.(*Channel)
		if err := bc.initMCPProvisioner(context.Background()); err != nil {
			t.Fatalf("init: %v", err)
		}
		if bc.mcpClient != nil || bc.mcpServerID != uuid.Nil {
			t.Errorf("nil mcpStore should leave provisioner off")
		}
	})

	t.Run("server_not_found", func(t *testing.T) {
		fs := newFakeStore()
		resetWebhookRouterForTest()
		defer resetWebhookRouterForTest()

		mcpStore := newFakeMCPStore()
		// Intentionally do NOT seed serversByName — GetServerByName returns nil.

		fn := FactoryWithPortalStoreAndMCP(fs, mcpStore, "")
		ch, _ := fn("b1", nil, json.RawMessage(`{"portal":"p","bot_code":"c","bot_name":"n","mcp_server_name":"missing","mcp_base_url":"http://x"}`),
			bus.New(), nil)
		bc := ch.(*Channel)
		if err := bc.initMCPProvisioner(context.Background()); err != nil {
			t.Fatalf("init: %v", err)
		}
		if bc.mcpClient != nil || bc.mcpServerID != uuid.Nil {
			t.Errorf("missing server row should leave provisioner off")
		}
	})
}

// newBareChannelForNotifyTest builds a Channel that's wired enough for
// notifyUserOfMCPIssueOnce tests but skips MCP provisioner setup — the
// function under test only touches c.notifyMu / c.notifyDebounce and then
// delegates to sendChunk, which is out of scope here (send.go owns it).
//
// sendChunk will fail immediately with "portal not bound" because the test
// Client has no Portal attached — notifyUserOfMCPIssueOnce swallows that
// error via slog.Debug, so the debounce state is still the primary
// observable. Tests that care about the wire-level Send behavior should
// use the full newProvisionerTestChannel + portal helper instead.
func newBareChannelForNotifyTest(t *testing.T) *Channel {
	t.Helper()
	fs := newFakeStore()
	resetWebhookRouterForTest()
	t.Cleanup(resetWebhookRouterForTest)

	fn := FactoryWithPortalStore(fs, "")
	ch, err := fn("b1", nil,
		json.RawMessage(`{"portal":"p","bot_code":"c","bot_name":"n"}`),
		bus.New(), nil)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	bc := ch.(*Channel)
	bc.SetTenantID(store.GenNewID())
	bc.startMu.Lock()
	bc.botID = 1
	bc.client = NewClient("p.bitrix24.com", nil)
	bc.startMu.Unlock()
	return bc
}

// TestNotifyUserOfMCPIssueOnce_FirstCallMarksDebounce verifies the first
// notification for a user stamps the debounce map, regardless of whether
// the downstream sendChunk actually delivered the message. The debounce
// stamp is what prevents a webhook retry burst from flooding the user —
// if we only stamped on successful Send, a Bitrix-portal outage would
// simultaneously block delivery AND disable the rate limit, giving the
// user a queue of identical notices once the portal recovers.
func TestNotifyUserOfMCPIssueOnce_FirstCallMarksDebounce(t *testing.T) {
	bc := newBareChannelForNotifyTest(t)

	before := time.Now()
	bc.notifyUserOfMCPIssueOnce(context.Background(), "user-42", "chat-9")
	after := time.Now()

	bc.notifyMu.Lock()
	defer bc.notifyMu.Unlock()
	ts, ok := bc.notifyDebounce["user-42"]
	if !ok {
		t.Fatalf("first notify did not stamp debounce map for user-42")
	}
	if ts.Before(before) || ts.After(after) {
		t.Errorf("debounce timestamp %v out of window [%v, %v]", ts, before, after)
	}
}

// TestNotifyUserOfMCPIssueOnce_SecondCallWithinTTLIsDebounced verifies
// that a second call within mcpUserNotifyDebounceTTL does NOT refresh
// the stamp. Two invariants matter:
//  1. Rate limit holds — user gets exactly one notice per TTL window.
//  2. Timestamp stays pinned to the FIRST call, so the window rolls
//     forward from there (not from every subsequent silenced call).
//     Otherwise a sustained outage + steady webhook retry traffic
//     would keep bumping the stamp forward and the user would never
//     see a refreshed notice when the TTL legitimately expired.
func TestNotifyUserOfMCPIssueOnce_SecondCallWithinTTLIsDebounced(t *testing.T) {
	bc := newBareChannelForNotifyTest(t)

	bc.notifyUserOfMCPIssueOnce(context.Background(), "user-42", "chat-9")
	bc.notifyMu.Lock()
	firstStamp := bc.notifyDebounce["user-42"]
	bc.notifyMu.Unlock()

	// Small sleep so a naive "refresh stamp every call" bug would produce
	// a strictly later timestamp than firstStamp. 10ms is enough resolution
	// on every supported platform.
	time.Sleep(10 * time.Millisecond)

	bc.notifyUserOfMCPIssueOnce(context.Background(), "user-42", "chat-9")

	bc.notifyMu.Lock()
	defer bc.notifyMu.Unlock()
	secondStamp := bc.notifyDebounce["user-42"]
	if !secondStamp.Equal(firstStamp) {
		t.Errorf("debounced call should not refresh stamp: first=%v second=%v", firstStamp, secondStamp)
	}
}

// TestNotifyUserOfMCPIssueOnce_ExpiredDebounceAllowsNewNotice verifies
// the stamp gets refreshed once the TTL elapses. We manipulate the
// debounce map directly (planting a stale timestamp) to avoid a real
// 5-minute test runtime — this is the standard pattern for testing
// TTL-based caches without the wall-clock penalty.
func TestNotifyUserOfMCPIssueOnce_ExpiredDebounceAllowsNewNotice(t *testing.T) {
	bc := newBareChannelForNotifyTest(t)

	// Plant a stamp that's safely outside the TTL window.
	stale := time.Now().Add(-(mcpUserNotifyDebounceTTL + time.Minute))
	bc.notifyMu.Lock()
	bc.notifyDebounce = map[string]time.Time{"user-42": stale}
	bc.notifyMu.Unlock()

	bc.notifyUserOfMCPIssueOnce(context.Background(), "user-42", "chat-9")

	bc.notifyMu.Lock()
	defer bc.notifyMu.Unlock()
	got := bc.notifyDebounce["user-42"]
	if !got.After(stale) {
		t.Errorf("expired stamp should have been refreshed: stale=%v got=%v", stale, got)
	}
	if time.Since(got) > time.Second {
		t.Errorf("refreshed stamp should be ~now, got age=%v", time.Since(got))
	}
}

// TestNotifyUserOfMCPIssueOnce_EmptyInputsAreNoop guards the two
// defensive branches at the top of notifyUserOfMCPIssueOnce: empty
// chatID (no reply target) and empty userID (shouldn't reach here
// from handle.go but cheap to defend). Either one must short-circuit
// BEFORE the debounce map is touched — otherwise a webhook with a
// blank FromUserID could silently poison the "" key and prevent
// legitimate future notices from firing.
func TestNotifyUserOfMCPIssueOnce_EmptyInputsAreNoop(t *testing.T) {
	cases := []struct {
		name   string
		userID string
		chatID string
	}{
		{"empty_chat_id", "user-42", ""},
		{"whitespace_chat_id", "user-42", "   "},
		{"empty_user_id", "", "chat-9"},
		{"whitespace_user_id", "\t", "chat-9"},
		{"both_empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bc := newBareChannelForNotifyTest(t)
			bc.notifyUserOfMCPIssueOnce(context.Background(), tc.userID, tc.chatID)

			bc.notifyMu.Lock()
			defer bc.notifyMu.Unlock()
			if len(bc.notifyDebounce) != 0 {
				t.Errorf("no-op case must leave debounce map empty, got %v", bc.notifyDebounce)
			}
		})
	}
}

// TestNotifyUserOfMCPIssueOnce_DifferentUsersIndependent verifies per-
// user debounce isolation: user A hitting the rate limit must not
// silence notices for user B. This matters when a single MCP outage
// affects many users — each should be independently informed on their
// next message.
func TestNotifyUserOfMCPIssueOnce_DifferentUsersIndependent(t *testing.T) {
	bc := newBareChannelForNotifyTest(t)

	bc.notifyUserOfMCPIssueOnce(context.Background(), "alice", "chat-1")
	bc.notifyUserOfMCPIssueOnce(context.Background(), "bob", "chat-2")

	bc.notifyMu.Lock()
	defer bc.notifyMu.Unlock()
	if _, ok := bc.notifyDebounce["alice"]; !ok {
		t.Errorf("alice missing from debounce map: %v", bc.notifyDebounce)
	}
	if _, ok := bc.notifyDebounce["bob"]; !ok {
		t.Errorf("bob missing from debounce map: %v", bc.notifyDebounce)
	}
}

// TestFactory_HalfConfigRejected codifies the "both or neither" rule for
// the mcp_server_name + mcp_base_url pair so admin typos fail fast at
// load rather than manifesting as a silently-disabled provisioner.
func TestFactory_HalfConfigRejected(t *testing.T) {
	fs := newFakeStore()
	resetWebhookRouterForTest()
	defer resetWebhookRouterForTest()

	fn := FactoryWithPortalStoreAndMCP(fs, newFakeMCPStore(), "")

	cases := []struct {
		name string
		cfg  string
	}{
		{"only_server_name", `{"portal":"p","bot_code":"c","bot_name":"n","mcp_server_name":"x"}`},
		{"only_base_url", `{"portal":"p","bot_code":"c","bot_name":"n","mcp_base_url":"http://x"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := fn("b1", nil, json.RawMessage(tc.cfg), bus.New(), nil)
			if err == nil {
				t.Fatal("half-config must fail at factory load")
			}
		})
	}
}
