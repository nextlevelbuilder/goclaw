package bitrix24

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// testOAuthEncKey is a raw 32-byte key — crypto.DeriveKey accepts a raw
// 32-byte input as-is, so this doubles as both the Channel's encKey and the
// HMAC key oauth_state_codec_test.go already exercises directly.
const testOAuthEncKey = "01234567890123456789012345678901"

// oauthTokenHandler builds an httptest handler for the Bitrix OAuth token
// endpoint. userID/domain feed the success response; when fail is true it
// returns a Bitrix-style application error instead (mirrors
// makeRefreshHandler in portal_test.go).
func oauthTokenHandler(userID int64, domain string, fail bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if fail {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"code already used"}`))
			return
		}
		body, _ := json.Marshal(TokenResponse{
			AccessToken:  "user-access-tok",
			RefreshToken: "user-refresh-tok",
			ExpiresIn:    3600,
			Domain:       domain,
			MemberID:     "mem1",
			UserID:       userID,
		})
		_, _ = w.Write(body)
	}
}

// mcpAutoOnboardHandler builds an httptest handler for the MCP server's
// /api/auto-onboard endpoint.
func mcpAutoOnboardHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"api_key":"minted-key","user_id":"u1","tenant_id":"t1","created":true}`))
	}
}

// newOAuthFlowTestChannel builds a Channel wired for HandleUserOAuthCallback
// tests: mcp provisioning against mcpSrv, a Portal whose OAuth calls hit
// oauthSrv with PublicURL set, and registered with its own router under botID.
func newOAuthFlowTestChannel(t *testing.T, mcpSrv, oauthSrv *httptest.Server) (*Channel, *fakeMCPStore) {
	t.Helper()
	resetWebhookRouterForTest()
	t.Cleanup(resetWebhookRouterForTest)

	mcpStore := newFakeMCPStore()
	serverID := uuid.New()
	mcpStore.serversByName["bitrix-mcp"] = &store.MCPServerData{
		BaseModel: store.BaseModel{ID: serverID},
		Name:      "bitrix-mcp",
	}

	fs := newFakeStore()
	fn := FactoryWithPortalStoreAndMCP(fs, mcpStore, testOAuthEncKey)
	cfgJSON := `{"portal":"p","bot_code":"c","bot_name":"n","bot_type":"B","mcp_server_name":"bitrix-mcp","mcp_base_url":"` + mcpSrv.URL + `"}`
	ch, err := fn("b1", nil, json.RawMessage(cfgJSON), bus.New(), nil)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	bc := ch.(*Channel)
	bc.SetTenantID(store.GenNewID())

	bc.startMu.Lock()
	bc.botID = 42
	bc.client = NewClient("p.bitrix24.com", nil)
	bc.startMu.Unlock()

	if err := bc.initMCPProvisioner(context.Background()); err != nil {
		t.Fatalf("initMCPProvisioner: %v", err)
	}

	portalFS := newFakeStore()
	portal := newTestPortal(t, oauthSrv, portalFS, bc.TenantID(), "p",
		store.BitrixPortalState{PublicURL: "https://goclaw.example.com"})
	bc.startMu.Lock()
	bc.portal = portal
	bc.startMu.Unlock()

	bc.router.RegisterBot(bc.botID, bc)
	return bc, mcpStore
}

func testStatePayload(bc *Channel, userID string) *oauthStatePayload {
	return &oauthStatePayload{
		UserID:      userID,
		TenantID:    bc.TenantID().String(),
		BotID:       bc.BotID(),
		ChannelName: bc.Name(),
		Domain:      "portal.bitrix24.com",
		DialogID:    "chat123",
		ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
	}
}

func TestHandleUserOAuthCallback_Success(t *testing.T) {
	oauthSrv := httptest.NewServer(oauthTokenHandler(1058, "portal.bitrix24.com", false))
	defer oauthSrv.Close()
	mcpSrv := httptest.NewServer(mcpAutoOnboardHandler())
	defer mcpSrv.Close()

	bc, mcpStore := newOAuthFlowTestChannel(t, mcpSrv, oauthSrv)
	payload := testStatePayload(bc, "1058")

	result, err := bc.HandleUserOAuthCallback(context.Background(), "good-code", payload)
	if err != nil {
		t.Fatalf("HandleUserOAuthCallback: %v", err)
	}
	if result.Outcome != "success" {
		t.Fatalf("Outcome = %q, want success", result.Outcome)
	}

	creds, ok := mcpStore.userCreds[credKey(bc.mcpServerID, "1058")]
	if !ok {
		t.Fatal("expected mcp_user_credentials row to be minted")
	}
	if creds.Env["BITRIX_ACCESS_TOKEN"] != "user-access-tok" {
		t.Errorf("BITRIX_ACCESS_TOKEN = %q", creds.Env["BITRIX_ACCESS_TOKEN"])
	}
}

func TestHandleUserOAuthCallback_IdentityMismatch(t *testing.T) {
	// OAuth server authorizes as a DIFFERENT user (999) than the state targets (1058).
	oauthSrv := httptest.NewServer(oauthTokenHandler(999, "portal.bitrix24.com", false))
	defer oauthSrv.Close()
	mcpSrv := httptest.NewServer(mcpAutoOnboardHandler())
	defer mcpSrv.Close()

	bc, mcpStore := newOAuthFlowTestChannel(t, mcpSrv, oauthSrv)
	payload := testStatePayload(bc, "1058")

	result, err := bc.HandleUserOAuthCallback(context.Background(), "good-code", payload)
	if err != nil {
		t.Fatalf("HandleUserOAuthCallback: %v", err)
	}
	if result.Outcome != "identity_mismatch" {
		t.Fatalf("Outcome = %q, want identity_mismatch", result.Outcome)
	}
	if mcpStore.setUserCallCount != 0 {
		t.Errorf("SetUserCredentials must not be called on identity mismatch; setUserCallCount=%d", mcpStore.setUserCallCount)
	}
}

func TestHandleUserOAuthCallback_ExchangeFailure(t *testing.T) {
	oauthSrv := httptest.NewServer(oauthTokenHandler(0, "", true)) // fail=true → invalid_grant
	defer oauthSrv.Close()
	mcpSrv := httptest.NewServer(mcpAutoOnboardHandler())
	defer mcpSrv.Close()

	bc, mcpStore := newOAuthFlowTestChannel(t, mcpSrv, oauthSrv)
	payload := testStatePayload(bc, "1058")

	if _, err := bc.HandleUserOAuthCallback(context.Background(), "stale-code", payload); err == nil {
		t.Fatal("expected error for failed code exchange, got nil")
	}
	if mcpStore.setUserCallCount != 0 {
		t.Errorf("SetUserCredentials must not be called on exchange failure; setUserCallCount=%d", mcpStore.setUserCallCount)
	}
}

// --- HTTP route-level tests (Router.ServeHTTP → handleUserOAuthCallback) ---

func TestRouterUserOAuthCallback_Declined(t *testing.T) {
	oauthSrv := httptest.NewServer(oauthTokenHandler(1058, "portal.bitrix24.com", false))
	defer oauthSrv.Close()
	mcpSrv := httptest.NewServer(mcpAutoOnboardHandler())
	defer mcpSrv.Close()

	bc, mcpStore := newOAuthFlowTestChannel(t, mcpSrv, oauthSrv)
	keyBytes, err := crypto.DeriveKey(testOAuthEncKey)
	if err != nil {
		t.Fatalf("derive key: %v", err)
	}
	state, err := encodeOAuthState(*testStatePayload(bc, "1058"), keyBytes)
	if err != nil {
		t.Fatalf("encode state: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/bitrix24/oauth/user/callback?state="+state+"&error=access_denied", nil)
	rec := httptest.NewRecorder()
	bc.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "từ chối") {
		t.Errorf("body doesn't mention decline: %s", rec.Body.String())
	}
	if mcpStore.setUserCallCount != 0 {
		t.Errorf("declined flow must not mint credentials; setUserCallCount=%d", mcpStore.setUserCallCount)
	}
}

func TestRouterUserOAuthCallback_InvalidState(t *testing.T) {
	oauthSrv := httptest.NewServer(oauthTokenHandler(1058, "portal.bitrix24.com", false))
	defer oauthSrv.Close()
	mcpSrv := httptest.NewServer(mcpAutoOnboardHandler())
	defer mcpSrv.Close()

	bc, _ := newOAuthFlowTestChannel(t, mcpSrv, oauthSrv)

	req := httptest.NewRequest(http.MethodGet, "/bitrix24/oauth/user/callback?state=garbage-not-signed&code=x", nil)
	rec := httptest.NewRecorder()
	bc.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (placeholder page)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "không hợp lệ") {
		t.Errorf("body doesn't mention invalid link: %s", rec.Body.String())
	}
}

func TestRouterUserOAuthCallback_HappyPath(t *testing.T) {
	oauthSrv := httptest.NewServer(oauthTokenHandler(1058, "portal.bitrix24.com", false))
	defer oauthSrv.Close()
	mcpSrv := httptest.NewServer(mcpAutoOnboardHandler())
	defer mcpSrv.Close()

	bc, mcpStore := newOAuthFlowTestChannel(t, mcpSrv, oauthSrv)
	keyBytes, err := crypto.DeriveKey(testOAuthEncKey)
	if err != nil {
		t.Fatalf("derive key: %v", err)
	}
	state, err := encodeOAuthState(*testStatePayload(bc, "1058"), keyBytes)
	if err != nil {
		t.Fatalf("encode state: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/bitrix24/oauth/user/callback?state="+state+"&code=good-code", nil)
	rec := httptest.NewRecorder()
	bc.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := mcpStore.userCreds[credKey(bc.mcpServerID, "1058")]; !ok {
		t.Fatal("expected mcp_user_credentials row to be minted via full HTTP route")
	}
}

