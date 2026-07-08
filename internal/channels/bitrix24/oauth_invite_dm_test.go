package bitrix24

import (
	"context"
	"encoding/json"
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

// capturedImbotCall is one recorded imbot.v2.Chat.Message.send request, as
// observed by the fake imbot server in newOAuthInviteTestChannel.
type capturedImbotCall struct {
	DialogID string
	Message  string
	Keyboard []map[string]any // decoded from the JSON-string fields[keyboard] param
}

// fakeImbotServer records every imbot.v2.Chat.Message.send call it receives
// and always answers with a minimal success envelope.
type fakeImbotServer struct {
	mu    sync.Mutex
	calls []capturedImbotCall
	// failDialogIDs marks dialog ids whose call should return a Bitrix error
	// instead of success — used to simulate the DM-send-fails-falls-back case.
	failDialogIDs map[string]bool
}

func newFakeImbotServer() *fakeImbotServer {
	return &fakeImbotServer{failDialogIDs: map[string]bool{}}
}

func (f *fakeImbotServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		dialogID := r.Form.Get("dialogId")

		call := capturedImbotCall{
			DialogID: dialogID,
			Message:  r.Form.Get("fields[message]"),
		}
		if kb := r.Form.Get("fields[keyboard]"); kb != "" {
			_ = json.Unmarshal([]byte(kb), &call.Keyboard)
		}
		f.mu.Lock()
		f.calls = append(f.calls, call)
		fail := f.failDialogIDs[dialogID]
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if fail {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"ACCESS_DENIED","error_description":"cannot message this user"}`))
			return
		}
		_, _ = w.Write([]byte(`{"result":123}`))
	}
}

func (f *fakeImbotServer) snapshot() []capturedImbotCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]capturedImbotCall, len(f.calls))
	copy(out, f.calls)
	return out
}

// newOAuthInviteTestChannel builds a Channel where a webhook event with no
// auth block will fall all the way through provisionIfMissing into
// sendOAuthInvite, and captures whatever imbot.v2.Chat.Message.send calls
// that triggers via imbotSrv.
func newOAuthInviteTestChannel(t *testing.T, imbotSrv *httptest.Server) (*Channel, *bus.MessageBus) {
	t.Helper()
	resetWebhookRouterForTest()
	t.Cleanup(resetWebhookRouterForTest)

	mcpStore := newFakeMCPStore()
	serverID := uuid.New()
	mcpStore.serversByName["bitrix-mcp"] = &store.MCPServerData{
		BaseModel: store.BaseModel{ID: serverID},
		Name:      "bitrix-mcp",
	}
	// MCP auto-onboard is never expected to be hit in these tests (no auth
	// in the event → brand-new-user branch short-circuits before it).
	mcpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("auto-onboard must not be called when the event carries no auth block")
	}))
	t.Cleanup(mcpSrv.Close)

	fs := newFakeStore()
	mb := bus.New()
	fn := FactoryWithPortalStoreAndMCP(fs, mcpStore, testOAuthEncKey)
	cfgJSON := `{"portal":"p","bot_code":"c","bot_name":"n","bot_type":"B","dm_policy":"open","group_policy":"open","require_mention":false,"mcp_server_name":"bitrix-mcp","mcp_base_url":"` + mcpSrv.URL + `"}`
	ch, err := fn("b1", nil, json.RawMessage(cfgJSON), mb, nil)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	bc := ch.(*Channel)
	bc.SetTenantID(store.GenNewID())

	bc.startMu.Lock()
	bc.botID = 42
	bc.startMu.Unlock()

	if err := bc.initMCPProvisioner(context.Background()); err != nil {
		t.Fatalf("initMCPProvisioner: %v", err)
	}

	// Portal with BOTH a captured public_url (BuildUserAuthorizeURL) AND a
	// pre-seeded, non-expired access token (so Client().Call reaches
	// imbotSrv directly, no OAuth refresh round-trip needed).
	portalFS := newFakeStore()
	portal := newTestPortal(t, httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no OAuth token call expected — access token is pre-seeded fresh")
	})), portalFS, bc.TenantID(), "p", store.BitrixPortalState{
		PublicURL:    "https://goclaw.example.com",
		AccessToken:  "bot-access-tok",
		RefreshToken: "bot-refresh-tok",
		ExpiresAt:    time.Now().Add(time.Hour),
	})

	bc.startMu.Lock()
	bc.portal = portal
	bc.client = NewClient("portal.bitrix24.com", &http.Client{
		Transport: &rewriteRT{target: imbotSrv.URL, base: http.DefaultTransport},
	})
	bc.client.SetPortal(portal)
	bc.startMu.Unlock()

	return bc, mb
}

func TestHandleMessage_OAuthInvite_GroupOrigin_SendsDMAndHint(t *testing.T) {
	imbot := newFakeImbotServer()
	srv := httptest.NewServer(imbot.handler())
	defer srv.Close()

	bc, mb := newOAuthInviteTestChannel(t, srv)

	bc.DispatchEvent(context.Background(), &Event{
		Type: EventMessageAdd,
		Params: EventParams{
			FromUserID:  "1058",
			DialogID:    "chat777",
			ChatID:      "777",
			MessageID:   "m-1",
			MessageType: "chat", // group
			Message:     "hello bot",
		},
	})

	if _, ok := drainOne(mb, 200*time.Millisecond); ok {
		t.Error("must not publish to agent bus while user has no MCP access")
	}

	calls := imbot.snapshot()
	if len(calls) != 2 {
		t.Fatalf("expected 2 imbot calls (DM + group hint), got %d: %+v", len(calls), calls)
	}

	dm := calls[0]
	if dm.DialogID != "1058" {
		t.Errorf("DM dialogId = %q, want the plain user id 1058", dm.DialogID)
	}
	if len(dm.Keyboard) != 1 {
		t.Fatalf("DM keyboard should have exactly 1 button, got %d", len(dm.Keyboard))
	}
	link, _ := dm.Keyboard[0]["LINK"].(string)
	if !strings.Contains(link, "/oauth/authorize/") {
		t.Errorf("keyboard button LINK = %q, want an authorize URL", link)
	}
	if strings.Contains(dm.Message, link) {
		t.Error("DM message text must NOT contain the raw URL (must live only in the keyboard button)")
	}

	hint := calls[1]
	if hint.DialogID != "chat777" {
		t.Errorf("group hint dialogId = %q, want original chatID chat777", hint.DialogID)
	}
	if strings.Contains(hint.Message, "http") {
		t.Error("group hint must not leak the authorize URL")
	}
}

func TestHandleMessage_OAuthInvite_DMOrigin_NoHint(t *testing.T) {
	imbot := newFakeImbotServer()
	srv := httptest.NewServer(imbot.handler())
	defer srv.Close()

	bc, _ := newOAuthInviteTestChannel(t, srv)

	bc.DispatchEvent(context.Background(), &Event{
		Type: EventMessageAdd,
		Params: EventParams{
			FromUserID:  "1058",
			DialogID:    "1058", // DM: dialog IS the user id
			MessageID:   "m-1",
			MessageType: "private",
			Message:     "hello bot",
		},
	})

	calls := imbot.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 imbot call (DM only, no hint), got %d: %+v", len(calls), calls)
	}
	if calls[0].DialogID != "1058" {
		t.Errorf("dialogId = %q, want 1058", calls[0].DialogID)
	}
}

func TestHandleMessage_OAuthInvite_DMSendFails_FallsBackToChat(t *testing.T) {
	imbot := newFakeImbotServer()
	imbot.failDialogIDs["1058"] = true // DM to the user fails
	srv := httptest.NewServer(imbot.handler())
	defer srv.Close()

	bc, _ := newOAuthInviteTestChannel(t, srv)

	bc.DispatchEvent(context.Background(), &Event{
		Type: EventMessageAdd,
		Params: EventParams{
			FromUserID:  "1058",
			DialogID:    "chat777",
			ChatID:      "777",
			MessageID:   "m-1",
			MessageType: "chat",
			Message:     "hello bot",
		},
	})

	calls := imbot.snapshot()
	// Expect: failed DM attempt (dialogId=1058) + fallback into chat777
	// carrying the SAME message+keyboard (not the group-hint text).
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls (failed DM + fallback), got %d: %+v", len(calls), calls)
	}
	if calls[0].DialogID != "1058" {
		t.Errorf("first call dialogId = %q, want 1058 (the attempted DM)", calls[0].DialogID)
	}
	fallback := calls[1]
	if fallback.DialogID != "chat777" {
		t.Errorf("fallback dialogId = %q, want chat777", fallback.DialogID)
	}
	if len(fallback.Keyboard) != 1 {
		t.Fatalf("fallback must carry the SAME keyboard button, got %d buttons", len(fallback.Keyboard))
	}
	if fallback.Message != oauthInviteMessage {
		t.Errorf("fallback message = %q, want the full invite hint (not the group-only hint text)", fallback.Message)
	}
}

// TestHandleMessage_OAuthInvite_TotalFailure_ReleasesDebounce covers the
// double-failure case (DM AND the chatID fallback both error) — the user got
// NOTHING delivered, so the debounce slot taken for this attempt must be
// released immediately rather than blocking a retry for the full 5-minute TTL.
func TestHandleMessage_OAuthInvite_TotalFailure_ReleasesDebounce(t *testing.T) {
	imbot := newFakeImbotServer()
	imbot.failDialogIDs["1058"] = true    // DM fails
	imbot.failDialogIDs["chat777"] = true // fallback also fails
	srv := httptest.NewServer(imbot.handler())
	defer srv.Close()

	bc, _ := newOAuthInviteTestChannel(t, srv)

	bc.DispatchEvent(context.Background(), &Event{
		Type: EventMessageAdd,
		Params: EventParams{
			FromUserID:  "1058",
			DialogID:    "chat777",
			ChatID:      "777",
			MessageID:   "m-1",
			MessageType: "chat",
			Message:     "hello bot",
		},
	})

	calls := imbot.snapshot()
	if len(calls) != 2 {
		t.Fatalf("expected 2 attempted calls (DM + fallback, both failing), got %d: %+v", len(calls), calls)
	}

	// The debounce slot must have been released — a second trigger right
	// after must be allowed to attempt delivery again immediately.
	if !bc.tryAcquireOAuthInviteNotify("1058") {
		t.Fatal("debounce slot should have been released after total delivery failure, but a retry was blocked")
	}
}

// TestOAuthInviteDebounce_IndependentOfNotifyDebounce exercises
// tryAcquireOAuthInviteNotify directly (unit-level, not through the full
// DispatchEvent pipeline — the outer 60s auto-onboard debounce in
// provisionIfMissing would otherwise swallow a second same-second webhook
// retry before ever reaching this code, making the two debounces
// impossible to tell apart end-to-end). Verifies: (1) a second acquire for
// the same user within the TTL is rejected, (2) marking notifyDebounce
// (the OTHER notice type's map) does not affect oauthInviteDebounce at all.
func TestOAuthInviteDebounce_IndependentOfNotifyDebounce(t *testing.T) {
	imbot := newFakeImbotServer()
	srv := httptest.NewServer(imbot.handler())
	defer srv.Close()
	bc, _ := newOAuthInviteTestChannel(t, srv)

	if !bc.tryAcquireOAuthInviteNotify("1058") {
		t.Fatal("first acquire should succeed")
	}
	if bc.tryAcquireOAuthInviteNotify("1058") {
		t.Fatal("second acquire within TTL should be debounced")
	}

	// A DIFFERENT user must not be affected by the first user's debounce.
	if !bc.tryAcquireOAuthInviteNotify("999") {
		t.Fatal("a different user must not be debounced by user 1058's entry")
	}

	// Marking the OTHER notice type's debounce map must not influence this one.
	bc.notifyMu.Lock()
	if bc.notifyDebounce == nil {
		bc.notifyDebounce = make(map[string]time.Time)
	}
	bc.notifyDebounce["777"] = time.Now()
	bc.notifyMu.Unlock()
	if !bc.tryAcquireOAuthInviteNotify("777") {
		t.Fatal("oauthInviteDebounce must be independent of notifyDebounce — user 777 was only debounced in the OTHER map")
	}
}

// TestOAuthInviteDebounce_SweepsExpiredEntries verifies tryAcquireOAuthInviteNotify
// evicts stale entries instead of letting the map grow unbounded for the life
// of the process (code-review finding: the map must not repeat the never-evicted
// pattern of mcpDebounce/notifyDebounce).
func TestOAuthInviteDebounce_SweepsExpiredEntries(t *testing.T) {
	imbot := newFakeImbotServer()
	srv := httptest.NewServer(imbot.handler())
	defer srv.Close()
	bc, _ := newOAuthInviteTestChannel(t, srv)

	// Seed an already-expired entry directly (can't sleep 5 real minutes in a test).
	bc.oauthInviteMu.Lock()
	bc.oauthInviteDebounce = map[string]time.Time{
		"stale-user": time.Now().Add(-mcpUserNotifyDebounceTTL - time.Second),
	}
	bc.oauthInviteMu.Unlock()

	// Any call sweeps expired entries before doing its own check-and-set.
	if !bc.tryAcquireOAuthInviteNotify("fresh-user") {
		t.Fatal("acquire for a new user should succeed")
	}

	bc.oauthInviteMu.Lock()
	_, staleStillPresent := bc.oauthInviteDebounce["stale-user"]
	_, freshPresent := bc.oauthInviteDebounce["fresh-user"]
	mapSize := len(bc.oauthInviteDebounce)
	bc.oauthInviteMu.Unlock()

	if staleStillPresent {
		t.Error("expired entry 'stale-user' should have been swept, but is still present")
	}
	if !freshPresent {
		t.Error("'fresh-user' should be present after its own acquire")
	}
	if mapSize != 1 {
		t.Errorf("map should contain exactly 1 entry (fresh-user) after sweep, got %d", mapSize)
	}
}
