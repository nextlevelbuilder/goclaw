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
	t.Setenv(mcpAdminTokenEnv, "admintok")

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
	t.Setenv(mcpAdminTokenEnv, "admintok")
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

	err = bc.provisionIfMissing(context.Background(), "42", validAuth())
	if !errors.Is(err, ErrProvisionSkippedOpenChannel) {
		t.Fatalf("err = %v; want ErrProvisionSkippedOpenChannel", err)
	}
	if mcpStore.getUserCallCount != 0 {
		t.Errorf("Open Channel bot must not hit MCP store; got %d GetUserCredentials calls", mcpStore.getUserCallCount)
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

	err = bc.provisionIfMissing(context.Background(), "42", validAuth())
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
	mcpStore.userCreds[credKey(bc.mcpServerID, "42")] = store.MCPUserCredentials{
		APIKey: "prior-key",
	}

	if err := bc.provisionIfMissing(context.Background(), "42", validAuth()); err != nil {
		t.Fatalf("err = %v; want nil", err)
	}
	if httpCalls != 0 {
		t.Errorf("warm path must not call auto-onboard; got %d HTTP calls", httpCalls)
	}
	if mcpStore.setUserCallCount != 0 {
		t.Errorf("warm path must not re-persist; got %d SetUserCredentials calls", mcpStore.setUserCallCount)
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
	err := bc.provisionIfMissing(context.Background(), "42", validAuth())
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
	if err := bc.provisionIfMissing(context.Background(), "42", validAuth()); err != nil {
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

	err := bc.provisionIfMissing(context.Background(), "42", validAuth())
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
		_, _ = w.Write([]byte(`{"error":"bad_admin_token"}`))
	}))
	defer srv.Close()

	mcpStore := newFakeMCPStore()
	bc := newProvisionerTestChannel(t, mcpStore, srv.URL, "B")

	err := bc.provisionIfMissing(context.Background(), "42", validAuth())
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
			err := bc.provisionIfMissing(context.Background(), tc.name, tc.auth)
			if err == nil {
				t.Fatalf("missing %s should fail", tc.name)
			}
			if httpCalls != before {
				t.Errorf("incomplete auth must not hit HTTP; got +%d calls", httpCalls-before)
			}
		})
	}
}

// TestInitMCPProvisioner_DisabledModes covers the four configurations
// that leave the provisioner off at startup (all non-fatal):
//   - nil MCPServerStore
//   - empty mcp_server_name or mcp_base_url
//   - missing admin token env var
//   - mcp_server_name points at a server that doesn't exist in the store
//
// Each case should leave mcpClient nil + mcpServerID zero, so
// provisionIfMissing returns ErrProvisionDisabled.
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

	t.Run("missing_admin_token", func(t *testing.T) {
		t.Setenv(mcpAdminTokenEnv, "") // explicitly unset
		fs := newFakeStore()
		resetWebhookRouterForTest()
		defer resetWebhookRouterForTest()

		mcpStore := newFakeMCPStore()
		fn := FactoryWithPortalStoreAndMCP(fs, mcpStore, "")
		ch, _ := fn("b1", nil, json.RawMessage(`{"portal":"p","bot_code":"c","bot_name":"n","mcp_server_name":"bitrix-mcp","mcp_base_url":"http://x"}`),
			bus.New(), nil)
		bc := ch.(*Channel)
		if err := bc.initMCPProvisioner(context.Background()); err != nil {
			t.Fatalf("init: %v", err)
		}
		if bc.mcpClient != nil {
			t.Errorf("missing admin token should leave mcpClient nil")
		}
	})

	t.Run("server_not_found", func(t *testing.T) {
		t.Setenv(mcpAdminTokenEnv, "admintok")
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
