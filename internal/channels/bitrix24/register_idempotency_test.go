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

// ---------- Sanity: ensure our uuid/tenant helper types compile ----------
// (Compile-time reference so unused imports from the fake-store pattern
// don't trip `go vet`; no runtime check needed.)
var _ uuid.UUID
var _ = errors.New
