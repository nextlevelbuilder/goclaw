package bitrix24

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// restHandler is a tiny dispatch map for the subset of REST methods
// registerBot / verifyBot / findBotIDByCode touch. Keys are the bare method
// name (e.g. "imbot.register"). Unmapped methods return 404 so a test that
// forgot to stub a call fails loudly rather than silently passing.
type restHandler map[string]http.HandlerFunc

func (h restHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Client endpoint shape: /rest/<method>.json
	path := strings.TrimPrefix(r.URL.Path, "/rest/")
	method := strings.TrimSuffix(path, ".json")
	if fn, ok := h[method]; ok {
		fn(w, r)
		return
	}
	http.Error(w, "method not stubbed: "+method, http.StatusNotFound)
}

// newRegisterTestChannel builds a Channel whose portal's Client routes every
// REST call to the supplied httptest server. The portal is pre-seeded with a
// refresh token so AccessToken() serves the in-memory token without hitting
// the OAuth endpoint (which would be another stub we'd need to maintain).
func newRegisterTestChannel(t *testing.T, srv *httptest.Server, state store.BitrixPortalState) *Channel {
	t.Helper()
	resetWebhookRouterForTest()
	fs := newFakeStore()
	tid := store.GenNewID()

	// Seed portal with creds + state (access token pre-set so the REST client
	// short-circuits the refresh path).
	creds, _ := json.Marshal(store.BitrixPortalCredentials{ClientID: "cid", ClientSecret: "secret"})
	stateBytes, _ := json.Marshal(state)
	fs.seed(tid, "p", "portal.bitrix24.com", creds, stateBytes)

	fn := FactoryWithPortalStore(fs, "")
	cfg := json.RawMessage(`{"portal":"p","bot_code":"support_bot","bot_name":"Support","public_url":"https://gw.test"}`)
	ch, err := fn("b1", nil, cfg, bus.New(), nil)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	bc := ch.(*Channel)
	bc.SetTenantID(tid)

	p, err := bc.router.ResolveOrLoadPortal(context.Background(), tid, "p")
	if err != nil {
		t.Fatalf("resolve portal: %v", err)
	}
	bc.router.RegisterPortal(p)

	// Redirect the portal's REST client transport at our test server so
	// https://portal.bitrix24.com/rest/... lands here.
	p.client.http = &http.Client{
		Transport: &rewriteRT{target: srv.URL, base: http.DefaultTransport},
	}

	bc.startMu.Lock()
	bc.portal = p
	bc.client = p.Client()
	bc.startMu.Unlock()
	return bc
}

// ---------- Path 1: state recovery (cached bot_id verified) ----------

func TestRegisterBot_Path1_CachedBotIDStillValid_NoRegisterCall(t *testing.T) {
	var registerHits, listHits int32
	h := restHandler{
		"imbot.register": func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&registerHits, 1)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"should_not_be_called"}`))
		},
		"imbot.bot.list": func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&listHits, 1)
			w.Header().Set("Content-Type", "application/json")
			// Bot 42 still present on portal → verifyBot returns true.
			_, _ = w.Write([]byte(`{"result":[{"BOT_ID":42,"CODE":"support_bot"}]}`))
		},
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	ch := newRegisterTestChannel(t, srv, store.BitrixPortalState{
		RefreshToken:   "RT",
		AccessToken:    "AT",
		ExpiresAt:      time.Now().Add(time.Hour),
		RegisteredBots: map[string]int{"support_bot": 42},
	})
	defer resetWebhookRouterForTest()

	id, err := ch.registerBot(context.Background())
	if err != nil {
		t.Fatalf("registerBot: %v", err)
	}
	if id != 42 {
		t.Errorf("bot_id = %d; want 42 (cached)", id)
	}
	if n := atomic.LoadInt32(&registerHits); n != 0 {
		t.Errorf("imbot.register hits = %d; want 0 (cache path must not re-register)", n)
	}
	if n := atomic.LoadInt32(&listHits); n != 1 {
		t.Errorf("imbot.bot.list hits = %d; want 1 (for verifyBot)", n)
	}
}

func TestRegisterBot_Path1_CachedBotIDMissing_FallsThroughToRegister(t *testing.T) {
	var registerHits int32
	h := restHandler{
		"imbot.register": func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&registerHits, 1)
			_ = r.ParseForm()
			if got := r.Form.Get("CODE"); got != "support_bot" {
				t.Errorf("register CODE = %q; want support_bot", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":777}`))
		},
		"imbot.bot.list": func(w http.ResponseWriter, r *http.Request) {
			// Cached bot 42 is NOT in the portal's list → verifyBot returns false.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":[{"BOT_ID":99,"CODE":"other_bot"}]}`))
		},
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	ch := newRegisterTestChannel(t, srv, store.BitrixPortalState{
		RefreshToken:   "RT",
		AccessToken:    "AT",
		ExpiresAt:      time.Now().Add(time.Hour),
		RegisteredBots: map[string]int{"support_bot": 42},
	})
	defer resetWebhookRouterForTest()

	id, err := ch.registerBot(context.Background())
	if err != nil {
		t.Fatalf("registerBot: %v", err)
	}
	if id != 777 {
		t.Errorf("bot_id = %d; want 777 (freshly-registered)", id)
	}
	if n := atomic.LoadInt32(&registerHits); n != 1 {
		t.Errorf("imbot.register hits = %d; want 1 (fall-through expected)", n)
	}
}

// ---------- Path 2: fresh register with no prior state ----------

func TestRegisterBot_Path2_FreshRegisterSucceeds(t *testing.T) {
	var registerHits, listHits int32
	h := restHandler{
		"imbot.register": func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&registerHits, 1)
			_ = r.ParseForm()
			// Spot-check the handler URL made it into the form body. The
			// nested PROPERTIES[] and EVENT_MESSAGE_ADD keys are how operators
			// would realise an empty public_url doesn't reach Bitrix.
			if got := r.Form.Get("EVENT_MESSAGE_ADD"); got != "https://gw.test/bitrix24/events" {
				t.Errorf("EVENT_MESSAGE_ADD = %q; want absolute gw URL", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":{"BOT_ID":555}}`))
		},
		"imbot.bot.list": func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&listHits, 1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":[]}`))
		},
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	// No RegisteredBots → skips Path 1 entirely.
	ch := newRegisterTestChannel(t, srv, store.BitrixPortalState{
		RefreshToken: "RT", AccessToken: "AT",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	defer resetWebhookRouterForTest()

	id, err := ch.registerBot(context.Background())
	if err != nil {
		t.Fatalf("registerBot: %v", err)
	}
	if id != 555 {
		t.Errorf("bot_id = %d; want 555", id)
	}
	if n := atomic.LoadInt32(&registerHits); n != 1 {
		t.Errorf("imbot.register hits = %d; want 1", n)
	}
	if n := atomic.LoadInt32(&listHits); n != 0 {
		t.Errorf("imbot.bot.list hits = %d; want 0 (no cached id to verify)", n)
	}
}

// ---------- Path 3: duplicate CODE fallback ----------

func TestRegisterBot_Path3_DuplicateCode_ResolvesViaList(t *testing.T) {
	var listHits int32
	h := restHandler{
		"imbot.register": func(w http.ResponseWriter, r *http.Request) {
			// Simulate Bitrix rejecting our register call because the CODE
			// already exists on the portal (another goclaw instance, or a
			// prior incarnation whose state was wiped).
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{
				"error":"ERROR_ARGUMENT",
				"error_description":"Bot code already exists on portal"
			}`))
		},
		"imbot.bot.list": func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&listHits, 1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":[
				{"BOT_ID":888,"CODE":"support_bot"},
				{"BOT_ID":999,"CODE":"other"}
			]}`))
		},
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	ch := newRegisterTestChannel(t, srv, store.BitrixPortalState{
		RefreshToken: "RT", AccessToken: "AT",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	defer resetWebhookRouterForTest()

	id, err := ch.registerBot(context.Background())
	if err != nil {
		t.Fatalf("registerBot: %v", err)
	}
	if id != 888 {
		t.Errorf("bot_id = %d; want 888 (resolved by CODE lookup)", id)
	}
	if n := atomic.LoadInt32(&listHits); n == 0 {
		t.Errorf("expected imbot.bot.list to be called during duplicate-code fallback")
	}
}

func TestRegisterBot_Path3_DuplicateCode_NotInList_Errors(t *testing.T) {
	h := restHandler{
		"imbot.register": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{
				"error":"ERROR_REGISTER_BOT",
				"error_description":"duplicate bot code"
			}`))
		},
		"imbot.bot.list": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			// None of these match "support_bot" → fallback should fail with a
			// clear "no bot with CODE" error rather than returning 0 success.
			_, _ = w.Write([]byte(`{"result":[{"BOT_ID":1,"CODE":"nope"}]}`))
		},
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	ch := newRegisterTestChannel(t, srv, store.BitrixPortalState{
		RefreshToken: "RT", AccessToken: "AT",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	defer resetWebhookRouterForTest()

	_, err := ch.registerBot(context.Background())
	if err == nil {
		t.Fatal("expected error when duplicate-code fallback yields no match")
	}
	if !strings.Contains(err.Error(), "no bot with CODE") {
		t.Errorf("error message = %v; want 'no bot with CODE' phrasing", err)
	}
}

func TestRegisterBot_Path3_BothListEndpointsFail_JoinsErrors(t *testing.T) {
	h := restHandler{
		"imbot.register": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{
				"error":"ERROR_ARGUMENT",
				"error_description":"bot code already exists"
			}`))
		},
		"imbot.bot.list": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"LIST_OUTAGE","error_description":"primary endpoint down"}`))
		},
		"imbot.list": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"ALT_OUTAGE","error_description":"alt endpoint also down"}`))
		},
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	ch := newRegisterTestChannel(t, srv, store.BitrixPortalState{
		RefreshToken: "RT", AccessToken: "AT",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	defer resetWebhookRouterForTest()

	_, err := ch.registerBot(context.Background())
	if err == nil {
		t.Fatal("expected error when both list endpoints fail")
	}
	msg := err.Error()
	// Both underlying error codes should be visible in the joined error so
	// operators can see we tried the fallback and both sides failed.
	if !strings.Contains(msg, "LIST_OUTAGE") {
		t.Errorf("primary error not surfaced: %s", msg)
	}
	if !strings.Contains(msg, "ALT_OUTAGE") {
		t.Errorf("alt error not surfaced (errors.Join missing): %s", msg)
	}
}

// ---------- Edge case: missing public_url aborts before imbot.register ----------

func TestRegisterBot_NoPublicURL_FailsFast(t *testing.T) {
	h := restHandler{
		"imbot.register": func(w http.ResponseWriter, r *http.Request) {
			t.Error("imbot.register must NOT be called when public_url is empty")
		},
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	ch := newRegisterTestChannel(t, srv, store.BitrixPortalState{
		RefreshToken: "RT", AccessToken: "AT",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	defer resetWebhookRouterForTest()
	// Override the per-instance config to clear PublicURL.
	ch.cfg.PublicURL = ""

	_, err := ch.registerBot(context.Background())
	if err == nil || !strings.Contains(err.Error(), "public_url") {
		t.Errorf("want public_url error, got %v", err)
	}
}

// ---------- eventHandlerURL preference: portal > legacy config ----------

// TestEventHandlerURL_PrefersPortalCapture verifies that when the portal has
// captured a PublicURL (Phase 01 install-handler capture), eventHandlerURL
// uses it and ignores the legacy per-channel config value.
func TestEventHandlerURL_PrefersPortalCapture(t *testing.T) {
	srv := httptest.NewServer(restHandler{})
	defer srv.Close()

	ch := newRegisterTestChannel(t, srv, store.BitrixPortalState{
		RefreshToken: "RT", AccessToken: "AT",
		ExpiresAt: time.Now().Add(time.Hour),
		PublicURL: "https://portal-captured.example.com",
	})
	defer resetWebhookRouterForTest()
	// Reload portal from store so the freshly-seeded state.PublicURL takes effect.
	// (newRegisterTestChannel sets bc.portal before this test can swap the
	// state — but newPortal already loaded the seeded state on construction.)

	// Even though config has the legacy URL, the portal-captured value wins.
	got := ch.eventHandlerURL()
	want := "https://portal-captured.example.com" + eventsPath
	if got != want {
		t.Errorf("eventHandlerURL = %q, want %q", got, want)
	}
}

// TestEventHandlerURL_FallsBackToLegacyConfig verifies that when the portal
// has NO captured URL (e.g. installed on a goclaw release predating Phase 01),
// eventHandlerURL falls back to config.public_url for backward compatibility.
func TestEventHandlerURL_FallsBackToLegacyConfig(t *testing.T) {
	srv := httptest.NewServer(restHandler{})
	defer srv.Close()

	// Portal state without PublicURL → forces fallback path.
	ch := newRegisterTestChannel(t, srv, store.BitrixPortalState{
		RefreshToken: "RT", AccessToken: "AT",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	defer resetWebhookRouterForTest()

	got := ch.eventHandlerURL()
	// newRegisterTestChannel seeds config with public_url=https://gw.test
	want := "https://gw.test" + eventsPath
	if got != want {
		t.Errorf("eventHandlerURL fallback = %q, want %q", got, want)
	}
}

// ---------- Phase D: unregister + destroy ----------

// TestUnregisterBot_Success verifies the happy path: imbot.unregister returns
// success → unregisterBot returns nil.
func TestUnregisterBot_Success(t *testing.T) {
	var calls int32
	h := restHandler{
		"imbot.unregister": func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&calls, 1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":true}`))
		},
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	ch := newRegisterTestChannel(t, srv, store.BitrixPortalState{
		RefreshToken: "RT", AccessToken: "AT", ExpiresAt: time.Now().Add(time.Hour),
	})
	defer resetWebhookRouterForTest()

	if err := ch.unregisterBot(context.Background(), 42); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("expected 1 call to imbot.unregister, got %d", got)
	}
}

// TestUnregisterBot_BotNotFound verifies idempotent behavior — Bitrix returning
// "bot not found" (because admin already deleted via UI) is treated as success.
func TestUnregisterBot_BotNotFound(t *testing.T) {
	h := restHandler{
		"imbot.unregister": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"ERROR_BOT_NOT_FOUND","error_description":"Bot not found on this portal"}`))
		},
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	ch := newRegisterTestChannel(t, srv, store.BitrixPortalState{
		RefreshToken: "RT", AccessToken: "AT", ExpiresAt: time.Now().Add(time.Hour),
	})
	defer resetWebhookRouterForTest()

	if err := ch.unregisterBot(context.Background(), 42); err != nil {
		t.Errorf("expected nil for bot-not-found (idempotent), got %v", err)
	}
}

// TestUnregisterBot_TransportError surfaces real errors (network/5xx) so the
// caller can log a warn and move on — must NOT be swallowed.
func TestUnregisterBot_TransportError(t *testing.T) {
	h := restHandler{
		"imbot.unregister": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"INTERNAL","error_description":"portal went away"}`))
		},
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	ch := newRegisterTestChannel(t, srv, store.BitrixPortalState{
		RefreshToken: "RT", AccessToken: "AT", ExpiresAt: time.Now().Add(time.Hour),
	})
	defer resetWebhookRouterForTest()

	if err := ch.unregisterBot(context.Background(), 42); err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

// TestUnregisterBot_ZeroBotID skips the network call entirely — channel that
// never successfully Start()-ed has botID == 0.
func TestUnregisterBot_ZeroBotID(t *testing.T) {
	var calls int32
	h := restHandler{
		"imbot.unregister": func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&calls, 1)
			_, _ = w.Write([]byte(`{"result":true}`))
		},
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	ch := newRegisterTestChannel(t, srv, store.BitrixPortalState{
		RefreshToken: "RT", AccessToken: "AT", ExpiresAt: time.Now().Add(time.Hour),
	})
	defer resetWebhookRouterForTest()

	if err := ch.unregisterBot(context.Background(), 0); err != nil {
		t.Errorf("expected nil for botID=0, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Errorf("expected zero calls when botID=0, got %d", got)
	}
}

// TestDestroy_FullFlow verifies all three steps run: imbot.unregister fires,
// the bot is removed from portal.state.RegisteredBots, and the channel stops.
func TestDestroy_FullFlow(t *testing.T) {
	var unregCalls int32
	h := restHandler{
		"imbot.unregister": func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&unregCalls, 1)
			_, _ = w.Write([]byte(`{"result":true}`))
		},
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	// Pre-state: bot 42 registered under code "support_bot" (matches factory cfg).
	ch := newRegisterTestChannel(t, srv, store.BitrixPortalState{
		RefreshToken:   "RT",
		AccessToken:    "AT",
		ExpiresAt:      time.Now().Add(time.Hour),
		RegisteredBots: map[string]int{"support_bot": 42},
	})
	defer resetWebhookRouterForTest()
	// Simulate post-Start state.
	ch.startMu.Lock()
	ch.botID = 42
	ch.startMu.Unlock()

	if err := ch.Destroy(context.Background()); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if got := atomic.LoadInt32(&unregCalls); got != 1 {
		t.Errorf("expected 1 imbot.unregister call, got %d", got)
	}
	if _, present := ch.Portal().LookupRegisteredBot("support_bot"); present {
		t.Error("expected RegisteredBots[support_bot] to be cleared")
	}
	if ch.IsRunning() {
		t.Error("expected channel to be stopped after Destroy")
	}
}

// TestDestroy_BotIDZero — channel that never started successfully still gets
// the local cleanup path; no Bitrix call.
func TestDestroy_BotIDZero(t *testing.T) {
	var unregCalls int32
	h := restHandler{
		"imbot.unregister": func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&unregCalls, 1)
		},
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	ch := newRegisterTestChannel(t, srv, store.BitrixPortalState{
		RefreshToken: "RT", AccessToken: "AT", ExpiresAt: time.Now().Add(time.Hour),
	})
	defer resetWebhookRouterForTest()
	// botID stays 0 — channel never claimed a bot.

	if err := ch.Destroy(context.Background()); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if got := atomic.LoadInt32(&unregCalls); got != 0 {
		t.Errorf("expected 0 imbot.unregister calls when botID=0, got %d", got)
	}
}

// TestDestroy_UnregisterFailureProceedsToCleanup verifies the best-effort
// contract: a Bitrix-side 5xx is logged but Destroy still returns nil and the
// local channel is stopped — DB delete upstream must not be blocked.
func TestDestroy_UnregisterFailureProceedsToCleanup(t *testing.T) {
	h := restHandler{
		"imbot.unregister": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"INTERNAL","error_description":"portal 5xx"}`))
		},
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	ch := newRegisterTestChannel(t, srv, store.BitrixPortalState{
		RefreshToken:   "RT",
		AccessToken:    "AT",
		ExpiresAt:      time.Now().Add(time.Hour),
		RegisteredBots: map[string]int{"support_bot": 42},
	})
	defer resetWebhookRouterForTest()
	ch.startMu.Lock()
	ch.botID = 42
	ch.startMu.Unlock()

	// Destroy must NOT propagate the unregister failure — it only returns
	// the error from Stop(), which is nil under normal conditions.
	if err := ch.Destroy(context.Background()); err != nil {
		t.Errorf("Destroy should not surface unregister failures: %v", err)
	}
	// ForgetRegisteredBot still ran (it's independent of the API call).
	if _, present := ch.Portal().LookupRegisteredBot("support_bot"); present {
		t.Error("ForgetRegisteredBot should still run despite unregister failure")
	}
	if ch.IsRunning() {
		t.Error("channel should be stopped despite unregister failure")
	}
}

// TestForgetRegisteredBot_Success — happy path: map entry removed + persisted.
func TestForgetRegisteredBot_Success(t *testing.T) {
	fs := newFakeStore()
	tid := store.GenNewID()
	creds, _ := json.Marshal(store.BitrixPortalCredentials{ClientID: "cid", ClientSecret: "secret"})
	state, _ := json.Marshal(store.BitrixPortalState{
		RegisteredBots: map[string]int{"alpha": 1, "beta": 2},
	})
	fs.seed(tid, "p", "p.bitrix24.com", creds, state)
	p, err := NewPortal(context.Background(), tid, "p", fs, "")
	if err != nil {
		t.Fatalf("NewPortal: %v", err)
	}

	if err := p.ForgetRegisteredBot(context.Background(), "alpha"); err != nil {
		t.Fatalf("ForgetRegisteredBot: %v", err)
	}
	if _, ok := p.LookupRegisteredBot("alpha"); ok {
		t.Error("alpha should be gone after Forget")
	}
	if id, ok := p.LookupRegisteredBot("beta"); !ok || id != 2 {
		t.Errorf("beta should remain (id=2), got id=%d ok=%v", id, ok)
	}
}

// TestForgetRegisteredBot_IdempotentAbsent — no-op when code wasn't there.
func TestForgetRegisteredBot_IdempotentAbsent(t *testing.T) {
	fs := newFakeStore()
	tid := store.GenNewID()
	creds, _ := json.Marshal(store.BitrixPortalCredentials{ClientID: "cid", ClientSecret: "secret"})
	fs.seed(tid, "p", "p.bitrix24.com", creds, nil)
	p, err := NewPortal(context.Background(), tid, "p", fs, "")
	if err != nil {
		t.Fatalf("NewPortal: %v", err)
	}
	if err := p.ForgetRegisteredBot(context.Background(), "nothing"); err != nil {
		t.Errorf("expected nil on absent code, got %v", err)
	}
}

// TestForgetRegisteredBot_EmptyCode — guard against accidental clear-all.
func TestForgetRegisteredBot_EmptyCode(t *testing.T) {
	fs := newFakeStore()
	tid := store.GenNewID()
	creds, _ := json.Marshal(store.BitrixPortalCredentials{ClientID: "cid", ClientSecret: "secret"})
	fs.seed(tid, "p", "p.bitrix24.com", creds, nil)
	p, err := NewPortal(context.Background(), tid, "p", fs, "")
	if err != nil {
		t.Fatalf("NewPortal: %v", err)
	}
	if err := p.ForgetRegisteredBot(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty code")
	}
}

// TestIsBotNotFoundError_Variants ensures the substring matcher catches all
// Bitrix24 error shapes for "bot doesn't exist".
func TestIsBotNotFoundError_Variants(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"code ERROR_BOT_NOT_FOUND", &APIError{Code: "ERROR_BOT_NOT_FOUND"}, true},
		{"code BOT_NOT_FOUND", &APIError{Code: "BOT_NOT_FOUND"}, true},
		{"description bot not found", &APIError{Code: "BAD", Description: "bot not found"}, true},
		{"description not registered", &APIError{Code: "BAD", Description: "Bot is not registered for this user"}, true},
		{"description no bot with", &APIError{Code: "BAD", Description: "no bot with id=42"}, true},
		{"unrelated error", &APIError{Code: "QUERY_LIMIT_EXCEEDED", Description: "Too many requests"}, false},
		{"plain error", errors.New("network refused"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isBotNotFoundError(tc.err); got != tc.want {
				t.Errorf("isBotNotFoundError = %v, want %v", got, tc.want)
			}
		})
	}
}

// ---------- Sanity: ensure our uuid/tenant helper types compile ----------
// (Compile-time reference so unused imports from the fake-store pattern
// don't trip `go vet`; no runtime check needed.)
var _ uuid.UUID
var _ = errors.New
