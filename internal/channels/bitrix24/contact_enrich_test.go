package bitrix24

import (
	"context"
	"encoding/json"
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

// newChannelWithBoundPortal builds a Channel whose client is bound to a
// Portal backed by the supplied httptest server. Unlike
// newProvisionerTestChannel, this wires up the REST path end-to-end so
// resolveContactName can actually make a user.get call through Client.Call.
//
// The portal is pre-seeded with a long-lived access token so Client.Call
// doesn't detour into OAuth refresh — tests can focus on the user.get
// handler behavior without juggling a token-refresh mock.
func newChannelWithBoundPortal(t *testing.T, srv *httptest.Server) *Channel {
	t.Helper()
	fs := newFakeStore()
	resetWebhookRouterForTest()
	t.Cleanup(resetWebhookRouterForTest)

	tid := store.GenNewID()
	// State with valid access token + far-future expiry so AccessToken()
	// returns the cached value instead of initiating a refresh.
	stateJSON, _ := json.Marshal(store.BitrixPortalState{
		AccessToken:    "at-live",
		RefreshToken:   "rt-live",
		ExpiresAt:      time.Now().Add(24 * time.Hour),
		AppToken:       "app-tok",
		MemberID:       "mem1",
		ClientEndpoint: "https://portal.bitrix24.com/rest/",
	})
	credsJSON, _ := json.Marshal(store.BitrixPortalCredentials{ClientID: "cid", ClientSecret: "secret"})
	fs.seed(tid, "p", "portal.bitrix24.com", credsJSON, stateJSON)

	p, err := NewPortal(context.Background(), tid, "p", fs, "")
	if err != nil {
		t.Fatalf("NewPortal: %v", err)
	}
	// Re-target the portal's client at the test server. REST calls go to
	// https://portal.bitrix24.com/rest/user.get.json — rewriteRT strips
	// the host and forwards to srv.URL verbatim, so the test handler sees
	// /rest/user.get.json as the path.
	p.client.http = &http.Client{Transport: &rewriteRT{target: srv.URL, base: http.DefaultTransport}}

	fn := FactoryWithPortalStore(fs, "")
	ch, err := fn("b1", nil,
		json.RawMessage(`{"portal":"p","bot_code":"c","bot_name":"n"}`),
		bus.New(), nil)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	bc := ch.(*Channel)
	bc.SetTenantID(tid)

	// Bypass Start() — wire the Portal + client directly so we don't need
	// imbot.register to succeed against the httptest server.
	bc.startMu.Lock()
	bc.portal = p
	bc.client = p.Client()
	bc.botID = 1
	bc.startMu.Unlock()
	return bc
}

// userGetHandler returns an http.HandlerFunc that serves Bitrix-style
// user.get responses. The handler counts calls via the supplied atomic
// counter so tests can assert cache hits/misses without probing internals.
//
// `users` is keyed by the string ID the test passes in (what the handler
// reads from the `ID=` form param). Missing keys → empty result array,
// which is Bitrix's "no such user" response.
func userGetHandler(t *testing.T, calls *atomic.Int32, users map[string]userGetRaw) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		// Any non-user.get path is a test setup bug — surface it loudly
		// so a misconfigured test doesn't silently pass.
		if !strings.HasSuffix(r.URL.Path, "/rest/user.get.json") {
			t.Errorf("unexpected REST path: %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		calls.Add(1)
		_ = r.ParseForm()
		id := r.Form.Get("ID")
		w.Header().Set("Content-Type", "application/json")
		if u, ok := users[id]; ok {
			_ = json.NewEncoder(w).Encode(map[string]any{"result": []userGetRaw{u}})
			return
		}
		// Unknown ID → empty result array, mirrors Bitrix's behavior
		// for Open Channel customer IDs that don't live in b_user.
		_ = json.NewEncoder(w).Encode(map[string]any{"result": []userGetRaw{}})
	}
}

// TestResolveContactName_HappyPath verifies first-call fetches + caches,
// subsequent calls hit the cache. This is the main invariant the
// Contacts page depends on — names appear after the first message and
// don't re-hit Bitrix on every follow-up.
func TestResolveContactName_HappyPath(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(userGetHandler(t, &calls, map[string]userGetRaw{
		"42": {ID: "42", Name: "Alice", LastName: "Anderson", Login: "alice"},
	}))
	defer srv.Close()

	bc := newChannelWithBoundPortal(t, srv)

	name, username := bc.resolveContactName(context.Background(), "42")
	if name != "Alice Anderson" {
		t.Errorf("name = %q; want %q", name, "Alice Anderson")
	}
	if username != "alice" {
		t.Errorf("username = %q; want %q", username, "alice")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("first call should trigger exactly one user.get; got %d", got)
	}

	// Second call within TTL → cache hit, no additional RPC.
	name2, username2 := bc.resolveContactName(context.Background(), "42")
	if name2 != name || username2 != username {
		t.Errorf("cached result differs: (%q,%q) vs (%q,%q)", name2, username2, name, username)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("cached call must not hit user.get; got %d total", got)
	}
}

// TestResolveContactName_EmptyUserID_IsNoop guards the cheap-exit branch
// so a webhook with a blank FromUserID (malformed payload, or
// pre-auth-gate test fixtures) doesn't make a pointless RPC.
func TestResolveContactName_EmptyUserID_IsNoop(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(userGetHandler(t, &calls, nil))
	defer srv.Close()

	bc := newChannelWithBoundPortal(t, srv)

	for _, uid := range []string{"", "   ", "\t"} {
		name, username := bc.resolveContactName(context.Background(), uid)
		if name != "" || username != "" {
			t.Errorf("empty uid %q should return empty, got (%q,%q)", uid, name, username)
		}
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("blank user ids must not trigger RPC; got %d calls", got)
	}
}

// TestResolveContactName_UnknownUser_NegativeCached verifies that when
// Bitrix returns an empty result array (happens for Open Channel
// customer IDs, or just a stale webhook referencing a deleted user),
// we cache a negative entry so a webhook retry burst doesn't spam
// user.get. Negative TTL is 5min, so the test asserts "no second RPC
// within a reasonable replay window" rather than wall-clocking a full
// TTL.
func TestResolveContactName_UnknownUser_NegativeCached(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(userGetHandler(t, &calls, nil)) // no users seeded
	defer srv.Close()

	bc := newChannelWithBoundPortal(t, srv)

	name, username := bc.resolveContactName(context.Background(), "ghost-user")
	if name != "" || username != "" {
		t.Errorf("unknown user should resolve to ('',''); got (%q,%q)", name, username)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("first unknown-user lookup should hit RPC once; got %d", got)
	}

	// Retry storm: simulate 5 follow-up webhook events for the same
	// unknown user. All must hit the negative cache.
	for i := 0; i < 5; i++ {
		_, _ = bc.resolveContactName(context.Background(), "ghost-user")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("negative cache must absorb retries; got %d total RPCs", got)
	}
}

// TestResolveContactName_NegativeCacheExpires checks that after
// nameCacheNegativeTTL elapses, a fresh lookup re-fetches. Important
// because config fixes (operator granting the missing `user` scope)
// should take effect within ~5min without needing a channel reload.
// We plant a stale negative entry directly to avoid a 5-minute test.
func TestResolveContactName_NegativeCacheExpires(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(userGetHandler(t, &calls, map[string]userGetRaw{
		"42": {ID: "42", Name: "Alice", Login: "alice"},
	}))
	defer srv.Close()

	bc := newChannelWithBoundPortal(t, srv)

	// Plant a stale negative entry: negative=true, fetchedAt safely
	// outside the negative TTL window.
	bc.nameCacheMu.Lock()
	bc.nameCache = map[string]nameCacheEntry{
		"42": {negative: true, fetchedAt: time.Now().Add(-(nameCacheNegativeTTL + time.Minute))},
	}
	bc.nameCacheMu.Unlock()

	name, _ := bc.resolveContactName(context.Background(), "42")
	if name != "Alice" {
		t.Errorf("expired negative entry should refetch; got name=%q", name)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("expected exactly one RPC after negative expiry; got %d", got)
	}
}

// TestResolveContactName_HTTPFailure_DegradesGracefully simulates a 500
// from Bitrix. The hot path must not propagate the error — EnsureContact
// still needs to run, and empty names are the documented fallback.
func TestResolveContactName_HTTPFailure_DegradesGracefully(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"INTERNAL","error_description":"boom"}`))
	}))
	defer srv.Close()

	bc := newChannelWithBoundPortal(t, srv)

	name, username := bc.resolveContactName(context.Background(), "42")
	if name != "" || username != "" {
		t.Errorf("HTTP 500 must degrade to empty; got (%q,%q)", name, username)
	}
	// Failure was cached negatively → second call doesn't re-hit the
	// dying backend (otherwise a sustained MCP-portal outage would
	// produce N RPC storms per webhook burst).
	_, _ = bc.resolveContactName(context.Background(), "42")
	if got := calls.Load(); got != 1 {
		t.Errorf("failed lookup should negative-cache; got %d total RPCs", got)
	}
}

// TestResolveContactName_NilClient_NoRPC protects against a race where
// resolveContactName is called before Start() has bound the portal's
// client. A panic here would crash the whole channel; returning empty
// silently is the right behavior.
func TestResolveContactName_NilClient_NoRPC(t *testing.T) {
	// Build a bare Channel with no client bound — mimics the state
	// right after the factory runs but before Start() completes.
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
	bc.SetTenantID(uuid.New())
	// Explicitly DON'T bind a client.

	name, username := bc.resolveContactName(context.Background(), "42")
	if name != "" || username != "" {
		t.Errorf("nil client should return empty, got (%q,%q)", name, username)
	}
}

// TestBuildDisplayName_PreferenceOrder documents the resolution policy
// in one place. If a future refactor changes the preference, this test
// flags it — the Contacts page's label consistency depends on it.
func TestBuildDisplayName_PreferenceOrder(t *testing.T) {
	cases := []struct {
		name    string
		profile bitrixUserProfile
		want    string
	}{
		{"full_name", bitrixUserProfile{Name: "Alice", LastName: "Anderson"}, "Alice Anderson"},
		{"last_only", bitrixUserProfile{LastName: "Anderson"}, "Anderson"},
		{"first_only", bitrixUserProfile{Name: "Alice"}, "Alice"},
		{"login_fallback", bitrixUserProfile{Login: "alice"}, "alice"},
		{"email_fallback", bitrixUserProfile{Email: "alice@example.com"}, "alice@example.com"},
		{"whitespace_trimmed", bitrixUserProfile{Name: "  Alice  ", LastName: "  Anderson  "}, "Alice Anderson"},
		{"all_empty", bitrixUserProfile{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildDisplayName(&tc.profile)
			if got != tc.want {
				t.Errorf("buildDisplayName(%+v) = %q; want %q", tc.profile, got, tc.want)
			}
		})
	}
}

// TestFetchBitrixUser_ArrayFormat and _ObjectFormat exercise the two
// shapes Bitrix has been observed to return from user.get. The array
// form is standard; the object form shows up on older portals. Both
// must decode cleanly so a portal upgrade doesn't break enrichment
// for deployments still on the legacy shape.
func TestFetchBitrixUser_ArrayFormat(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(userGetHandler(t, &calls, map[string]userGetRaw{
		"7": {ID: "7", Name: "Bob", LastName: "Brown", Login: "bbrown"},
	}))
	defer srv.Close()

	bc := newChannelWithBoundPortal(t, srv)
	p, err := fetchBitrixUser(context.Background(), bc.Client(), "7")
	if err != nil {
		t.Fatalf("fetchBitrixUser: %v", err)
	}
	if p.Name != "Bob" || p.LastName != "Brown" || p.Login != "bbrown" {
		t.Errorf("decoded profile wrong: %+v", p)
	}
}

func TestFetchBitrixUser_ObjectFormat(t *testing.T) {
	// Simulate a portal that returns a bare object (legacy shape) instead
	// of an array. fetchBitrixUser should transparently handle both.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"ID":"7","NAME":"Legacy","LAST_NAME":"Portal","LOGIN":"legacy"}}`))
	}))
	defer srv.Close()

	bc := newChannelWithBoundPortal(t, srv)
	p, err := fetchBitrixUser(context.Background(), bc.Client(), "7")
	if err != nil {
		t.Fatalf("fetchBitrixUser (object shape): %v", err)
	}
	if p.Name != "Legacy" || p.LastName != "Portal" || p.Login != "legacy" {
		t.Errorf("decoded profile wrong: %+v", p)
	}
}
