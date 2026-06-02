package bitrix24

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// fakeDispatcher is an in-memory BotDispatcher that records each delivered
// event onto a channel so tests can assert on dispatch order & payload.
type fakeDispatcher struct {
	botID  int
	tid    uuid.UUID
	name   string
	events chan *Event
}

func newFakeDispatcher(botID int, tid uuid.UUID, portalName string) *fakeDispatcher {
	return &fakeDispatcher{
		botID:  botID,
		tid:    tid,
		name:   portalName,
		events: make(chan *Event, 16),
	}
}

func (d *fakeDispatcher) BotID() int          { return d.botID }
func (d *fakeDispatcher) TenantID() uuid.UUID { return d.tid }
func (d *fakeDispatcher) PortalName() string  { return d.name }
func (d *fakeDispatcher) DispatchEvent(_ context.Context, evt *Event) {
	d.events <- evt
}

// newInstalledPortal returns a portal with pre-populated state so AppToken()
// is non-empty. Uses the existing fakeBitrixStore from portal_test.go.
func newInstalledPortal(t *testing.T, fs *fakeBitrixStore, tid uuid.UUID, name, domain, appToken string) *Portal {
	t.Helper()
	creds, _ := json.Marshal(store.BitrixPortalCredentials{ClientID: "cid", ClientSecret: "secret"})
	st := store.BitrixPortalState{
		AppToken:     appToken,
		AccessToken:  "AT",
		RefreshToken: "RT",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
		MemberID:     "mem1",
	}
	stateBytes, _ := json.Marshal(st)
	fs.seed(tid, name, domain, creds, stateBytes)

	p, err := NewPortal(context.Background(), tid, name, fs, "")
	if err != nil {
		t.Fatalf("NewPortal: %v", err)
	}
	return p
}

// buildEventBody returns a form-urlencoded body for a well-formed
// ONIMBOTMESSAGEADD event, parameterised so tests can vary critical fields.
func buildEventBody(domain, appToken string, botID int, messageID string) io.Reader {
	v := url.Values{}
	v.Set("event", "ONIMBOTMESSAGEADD")
	v.Set("ts", "1713564321")
	v.Set("auth[domain]", domain)
	v.Set("auth[application_token]", appToken)
	v.Set("auth[access_token]", "AT")
	v.Set("auth[member_id]", "mem1")
	v.Set("auth[expires_in]", "3600")
	v.Set("data[PARAMS][MESSAGE_ID]", messageID)
	v.Set("data[PARAMS][DIALOG_ID]", "chat1234")
	v.Set("data[PARAMS][FROM_USER_ID]", "7")
	v.Set("data[PARAMS][MESSAGE]", "hi")
	v.Set("data[BOT][914][BOT_ID]", strconvItoa(botID))
	return strings.NewReader(v.Encode())
}

// strconvItoa avoids importing strconv in every helper site.
func strconvItoa(i int) string {
	return formatInt(int64(i))
}

func formatInt(i int64) string {
	// tiny local helper to keep the test helper minimal
	if i == 0 {
		return "0"
	}
	negative := i < 0
	if negative {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if negative {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

func newRouterForTest() *Router {
	return NewRouter(newFakeStore(), "", RouterConfig{
		DedupMaxSize:     100,
		DedupTTL:         time.Minute,
		DedupSweepPeriod: 0, // disable sweeper in tests — no background goroutine
	})
}

// ---------------------------------------------------------------------------
// ClaimWebhookRoute — mount exclusivity
// ---------------------------------------------------------------------------

func TestRouter_ClaimWebhookRoute_ExactlyOnce(t *testing.T) {
	r := newRouterForTest()
	defer r.Stop()

	path1, h1 := r.ClaimWebhookRoute()
	if path1 != WebhookPathPrefix || h1 == nil {
		t.Fatalf("first claim must return (%q, non-nil); got (%q, %v)", WebhookPathPrefix, path1, h1)
	}
	path2, h2 := r.ClaimWebhookRoute()
	if path2 != "" || h2 != nil {
		t.Fatalf("subsequent claims must return ('', nil); got (%q, %v)", path2, h2)
	}
}

// ---------------------------------------------------------------------------
// handleInstall
// ---------------------------------------------------------------------------

func TestRouter_HandleInstall_Success(t *testing.T) {
	fs := newFakeStore()
	tid := uuid.New()

	// Build an OAuth test server that returns a valid token response.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"access_token":"AT","refresh_token":"RT","expires_in":3600,
			"domain":"portal.bitrix24.com","member_id":"mem1",
			"application_token":"APP","client_endpoint":"https://portal.bitrix24.com/rest/"
		}`))
	}))
	defer srv.Close()

	portal := newTestPortal(t, srv, fs, tid, "myportal", store.BitrixPortalState{})
	r := newRouterForTest()
	defer r.Stop()
	r.RegisterPortal(portal)

	form := url.Values{}
	form.Set("code", "abc123")
	form.Set("state", tid.String()+":myportal")
	form.Set("domain", "portal.bitrix24.com")
	req := httptest.NewRequest(http.MethodPost, "/bitrix24/install", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("content-type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), "Installation successful") {
		t.Errorf("body missing success marker: %s", rec.Body.String())
	}

	// Portal state must now be marked installed.
	if !portal.Installed() {
		t.Error("portal should be installed after Exchange")
	}
	if portal.AppToken() != "APP" {
		t.Errorf("AppToken = %q", portal.AppToken())
	}
}

// TestRouter_HandleInstall_CapturesPublicURL verifies the OAuth install path
// derives the gateway URL from the request and persists it on the portal.
// This is what removes the need for per-channel config.public_url.
func TestRouter_HandleInstall_CapturesPublicURL(t *testing.T) {
	fs := newFakeStore()
	tid := uuid.New()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"access_token":"AT","refresh_token":"RT","expires_in":3600,
			"domain":"portal.bitrix24.com","member_id":"mem1",
			"application_token":"APP"
		}`))
	}))
	defer srv.Close()

	portal := newTestPortal(t, srv, fs, tid, "myportal", store.BitrixPortalState{})
	r := newRouterForTest()
	defer r.Stop()
	r.RegisterPortal(portal)

	form := url.Values{}
	form.Set("code", "abc123")
	form.Set("state", tid.String()+":myportal")
	form.Set("domain", "portal.bitrix24.com")
	req := httptest.NewRequest(http.MethodPost, "/bitrix24/install", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// Simulate Cloudflare Tunnel forwarding the original public host + scheme.
	req.Host = "internal-lb"
	req.Header.Set("X-Forwarded-Host", "goclaw.tamgiac.com")
	req.Header.Set("X-Forwarded-Proto", "https")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := portal.PublicURL(); got != "https://goclaw.tamgiac.com" {
		t.Fatalf("PublicURL = %q, want %q", got, "https://goclaw.tamgiac.com")
	}
}

// TestRouter_HandleInstall_CaptureFailsSilently_OnPrivateHost ensures capture
// doesn't abort install when the URL is private/loopback — admin still gets
// a working portal, just no captured URL. Install must succeed.
func TestRouter_HandleInstall_CaptureFailsSilently_OnPrivateHost(t *testing.T) {
	fs := newFakeStore()
	tid := uuid.New()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"access_token":"AT","refresh_token":"RT","expires_in":3600,
			"domain":"portal.bitrix24.com","member_id":"mem1"
		}`))
	}))
	defer srv.Close()

	portal := newTestPortal(t, srv, fs, tid, "myportal", store.BitrixPortalState{})
	r := newRouterForTest()
	defer r.Stop()
	r.RegisterPortal(portal)

	form := url.Values{}
	form.Set("code", "abc123")
	form.Set("state", tid.String()+":myportal")
	form.Set("domain", "portal.bitrix24.com")
	req := httptest.NewRequest(http.MethodPost, "/bitrix24/install", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Host = "localhost:8080" // private, capture will skip

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (install succeeds even when capture skipped)", rec.Code)
	}
	if got := portal.PublicURL(); got != "" {
		t.Errorf("PublicURL should be empty after private-host capture, got %q", got)
	}
	if !portal.Installed() {
		t.Error("portal should still be marked installed")
	}
}

func TestRouter_HandleInstall_MissingCodeGetReturnsPlaceholder(t *testing.T) {
	r := newRouterForTest()
	defer r.Stop()

	req := httptest.NewRequest(http.MethodGet, "/bitrix24/install?state="+uuid.NewString()+":p", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Bitrix24 Install Endpoint") {
		t.Fatalf("body missing placeholder marker: %s", rec.Body.String())
	}
}

func TestRouter_HandleInstall_MissingCodePostRejects(t *testing.T) {
	r := newRouterForTest()
	defer r.Stop()

	req := httptest.NewRequest(http.MethodPost, "/bitrix24/install?state="+uuid.NewString()+":p", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestRouter_HandleInstall_InvalidState(t *testing.T) {
	r := newRouterForTest()
	defer r.Stop()

	req := httptest.NewRequest(http.MethodGet, "/bitrix24/install?code=c&state=notauuid", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestRouter_HandleInstall_UnknownPortal(t *testing.T) {
	r := newRouterForTest()
	defer r.Stop()

	req := httptest.NewRequest(http.MethodGet,
		"/bitrix24/install?code=c&state="+uuid.NewString()+":ghost", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestRouter_HandleInstall_DomainMismatch(t *testing.T) {
	fs := newFakeStore()
	tid := uuid.New()
	p := newInstalledPortal(t, fs, tid, "p", "portal.bitrix24.com", "APP")
	r := newRouterForTest()
	defer r.Stop()
	r.RegisterPortal(p)

	req := httptest.NewRequest(http.MethodGet,
		"/bitrix24/install?code=c&domain=other.bitrix24.com&state="+tid.String()+":p", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestRouter_HandleInstallLocalApp_RejectsUnvalidatedToken(t *testing.T) {
	fs := newFakeStore()
	tid := uuid.New()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/profile.json" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"INVALID_TOKEN","error_description":"bad auth"}`))
	}))
	defer srv.Close()

	portal := newTestPortal(t, srv, fs, tid, "p", store.BitrixPortalState{})
	r := newRouterForTest()
	defer r.Stop()
	r.RegisterPortal(portal)

	form := url.Values{}
	form.Set("AUTH_ID", "forged-access-token")
	form.Set("REFRESH_ID", "forged-refresh-token")
	form.Set("AUTH_EXPIRES", "3600")
	form.Set("DOMAIN", "portal.bitrix24.com")
	form.Set("member_id", "mem1")
	req := httptest.NewRequest(http.MethodPost, "/bitrix24/install", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", rec.Code, rec.Body.String())
	}
	if portal.Installed() {
		t.Fatal("portal should not install with unvalidated Local App tokens")
	}
}

// ---------------------------------------------------------------------------
// handleEvent — security
// ---------------------------------------------------------------------------

func TestRouter_HandleEvent_UnknownDomain_404(t *testing.T) {
	r := newRouterForTest()
	defer r.Stop()

	req := httptest.NewRequest(http.MethodPost, "/bitrix24/events",
		buildEventBody("ghost.bitrix24.com", "APP", 914, "m1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "unknown portal") {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestRouter_HandleEvent_SpoofAppToken_401(t *testing.T) {
	fs := newFakeStore()
	tid := uuid.New()
	p := newInstalledPortal(t, fs, tid, "p", "portal.bitrix24.com", "REAL_APP")
	r := newRouterForTest()
	defer r.Stop()
	r.RegisterPortal(p)

	// Register a dispatcher so we can assert it doesn't fire on spoof.
	disp := newFakeDispatcher(914, tid, "p")
	r.RegisterBot(914, disp)

	req := httptest.NewRequest(http.MethodPost, "/bitrix24/events",
		buildEventBody("portal.bitrix24.com", "WRONG_APP", 914, "m1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}

	// Dispatcher must NOT receive the event.
	select {
	case e := <-disp.events:
		t.Fatalf("dispatcher received spoofed event: %+v", e)
	case <-time.After(50 * time.Millisecond):
		// ok
	}
}

// TestRouter_HandleEvent_BootstrapAppToken_200 covers the Local App install
// flow where the install POST did not carry application_token — the first
// event seeds portal.AppToken() from evt.Auth.AppToken provided the event's
// member_id matches what install persisted. Second event should now auth
// normally against the seeded value.
func TestRouter_HandleEvent_BootstrapAppToken_200(t *testing.T) {
	fs := newFakeStore()
	tid := uuid.New()
	p := newInstalledPortal(t, fs, tid, "p", "portal.bitrix24.com", "") // empty AppToken, MemberID="mem1"
	r := newRouterForTest()
	defer r.Stop()
	r.RegisterPortal(p)
	disp := newFakeDispatcher(914, tid, "p")
	r.RegisterBot(914, disp)

	req := httptest.NewRequest(http.MethodPost, "/bitrix24/events",
		buildEventBody("portal.bitrix24.com", "SEEDED_APP", 914, "m1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := p.AppToken(); got != "SEEDED_APP" {
		t.Fatalf("AppToken after bootstrap = %q, want SEEDED_APP", got)
	}
	select {
	case <-disp.events:
		// ok — dispatcher ran
	case <-time.After(500 * time.Millisecond):
		t.Fatal("dispatcher did not receive bootstrapped event")
	}
}

// TestRouter_HandleEvent_BootstrapAppToken_MemberIDMismatch_401 asserts that
// the TOFU path refuses to seed an app_token when the event's member_id does
// not match what install stored. This is the critical guard against a spoofed
// first event poisoning the portal's stored token.
func TestRouter_HandleEvent_BootstrapAppToken_MemberIDMismatch_401(t *testing.T) {
	fs := newFakeStore()
	tid := uuid.New()
	p := newInstalledPortal(t, fs, tid, "p", "portal.bitrix24.com", "") // MemberID="mem1"
	r := newRouterForTest()
	defer r.Stop()
	r.RegisterPortal(p)

	v := url.Values{}
	v.Set("event", "ONIMBOTMESSAGEADD")
	v.Set("auth[domain]", "portal.bitrix24.com")
	v.Set("auth[application_token]", "SPOOF")
	v.Set("auth[member_id]", "mem-attacker") // wrong
	v.Set("data[PARAMS][MESSAGE_ID]", "m1")
	v.Set("data[BOT][914][BOT_ID]", "914")
	req := httptest.NewRequest(http.MethodPost, "/bitrix24/events", strings.NewReader(v.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := p.AppToken(); got != "" {
		t.Fatalf("AppToken after rejected bootstrap = %q, want empty", got)
	}
}

// TestRouter_HandleEvent_BootstrapAppToken_NoStoredMemberID_401 asserts the
// tightened guard: a portal row with empty stored MemberID (legacy / bad
// install) must NOT be auto-healed by the first inbound event. Prior to the
// fix, BootstrapAppToken fell through to "seed MemberID from event body" —
// that opened a spoof window where any attacker who knew DOMAIN could pin
// both MemberID and AppToken from their own event. The only safe recovery
// for a MemberID-less row is a fresh /bitrix24/install round-trip.
func TestRouter_HandleEvent_BootstrapAppToken_NoStoredMemberID_401(t *testing.T) {
	fs := newFakeStore()
	tid := uuid.New()
	// Seed portal manually so MemberID stays empty — newInstalledPortal
	// always writes "mem1", which would bypass the guard we're testing.
	creds, _ := json.Marshal(store.BitrixPortalCredentials{ClientID: "cid", ClientSecret: "secret"})
	st := store.BitrixPortalState{
		AccessToken:  "AT",
		RefreshToken: "RT",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
		// AppToken + MemberID both empty
	}
	stateBytes, _ := json.Marshal(st)
	fs.seed(tid, "p", "portal.bitrix24.com", creds, stateBytes)
	p, err := NewPortal(context.Background(), tid, "p", fs, "")
	if err != nil {
		t.Fatalf("NewPortal: %v", err)
	}
	r := newRouterForTest()
	defer r.Stop()
	r.RegisterPortal(p)

	req := httptest.NewRequest(http.MethodPost, "/bitrix24/events",
		buildEventBody("portal.bitrix24.com", "SPOOF_APP", 914, "m1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := p.AppToken(); got != "" {
		t.Fatalf("AppToken after rejected bootstrap = %q, want empty", got)
	}
}

// TestRouter_HandleEvent_PortalNotInstalled_401 keeps the classic rejection
// path: no stored app_token AND the event carries no app_token either — we
// have nothing to seed or compare, so 401 is the only safe outcome.
func TestRouter_HandleEvent_PortalNotInstalled_401(t *testing.T) {
	fs := newFakeStore()
	tid := uuid.New()
	p := newInstalledPortal(t, fs, tid, "p", "portal.bitrix24.com", "") // empty AppToken
	r := newRouterForTest()
	defer r.Stop()
	r.RegisterPortal(p)

	// Event with EMPTY auth[application_token] — no seed material available.
	req := httptest.NewRequest(http.MethodPost, "/bitrix24/events",
		buildEventBody("portal.bitrix24.com", "", 914, "m1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// handleEvent — dedup + dispatch
// ---------------------------------------------------------------------------

func TestRouter_HandleEvent_DispatchesToBot(t *testing.T) {
	fs := newFakeStore()
	tid := uuid.New()
	p := newInstalledPortal(t, fs, tid, "p", "portal.bitrix24.com", "APP")
	r := newRouterForTest()
	defer r.Stop()
	r.RegisterPortal(p)

	disp := newFakeDispatcher(914, tid, "p")
	r.RegisterBot(914, disp)

	req := httptest.NewRequest(http.MethodPost, "/bitrix24/events",
		buildEventBody("portal.bitrix24.com", "APP", 914, "m1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	select {
	case evt := <-disp.events:
		if evt.Params.MessageID != "m1" {
			t.Errorf("wrong MessageID: %q", evt.Params.MessageID)
		}
		if evt.Params.BotID != 914 {
			t.Errorf("wrong BotID: %d", evt.Params.BotID)
		}
		if evt.Auth.Domain != "portal.bitrix24.com" {
			t.Errorf("wrong domain: %q", evt.Auth.Domain)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("dispatcher never received event")
	}
}

func TestRouter_HandleEvent_DuplicateReturns2xx(t *testing.T) {
	fs := newFakeStore()
	tid := uuid.New()
	p := newInstalledPortal(t, fs, tid, "p", "portal.bitrix24.com", "APP")
	r := newRouterForTest()
	defer r.Stop()
	r.RegisterPortal(p)

	disp := newFakeDispatcher(914, tid, "p")
	r.RegisterBot(914, disp)

	// First post — should dispatch.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/bitrix24/events",
			buildEventBody("portal.bitrix24.com", "APP", 914, "m-dup"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("attempt=%d status=%d want 200 (body=%s)", i, rec.Code, rec.Body.String())
		}
		if i == 1 {
			if !strings.Contains(rec.Body.String(), `"duplicate":true`) {
				t.Errorf("second post body should include duplicate=true; got %s", rec.Body.String())
			}
		}
	}

	// Dispatcher must have received the event exactly once.
	received := 0
	for done := false; !done; {
		select {
		case <-disp.events:
			received++
		case <-time.After(100 * time.Millisecond):
			done = true
		}
	}
	if received != 1 {
		t.Fatalf("dispatcher received %d events; want 1", received)
	}
}

func TestRouter_HandleEvent_UnknownBot_404(t *testing.T) {
	fs := newFakeStore()
	tid := uuid.New()
	p := newInstalledPortal(t, fs, tid, "p", "portal.bitrix24.com", "APP")
	r := newRouterForTest()
	defer r.Stop()
	r.RegisterPortal(p)

	req := httptest.NewRequest(http.MethodPost, "/bitrix24/events",
		buildEventBody("portal.bitrix24.com", "APP", 999, "m1")) // bot 999 not registered
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestRouter_HandleEvent_MethodNotAllowed(t *testing.T) {
	r := newRouterForTest()
	defer r.Stop()
	req := httptest.NewRequest(http.MethodGet, "/bitrix24/events", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestRouter_ServeHTTP_NotFoundOnOtherPaths(t *testing.T) {
	r := newRouterForTest()
	defer r.Stop()
	req := httptest.NewRequest(http.MethodGet, "/bitrix24/random", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestRouter_HandleEvent_AppUninstall_UnregistersBot(t *testing.T) {
	fs := newFakeStore()
	tid := uuid.New()
	p := newInstalledPortal(t, fs, tid, "p", "portal.bitrix24.com", "APP")
	r := newRouterForTest()
	defer r.Stop()
	r.RegisterPortal(p)

	disp := newFakeDispatcher(914, tid, "p")
	r.RegisterBot(914, disp)

	v := url.Values{}
	v.Set("event", "ONAPPUNINSTALL")
	v.Set("auth[domain]", "portal.bitrix24.com")
	v.Set("auth[application_token]", "APP")

	req := httptest.NewRequest(http.MethodPost, "/bitrix24/events", strings.NewReader(v.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", rec.Code, rec.Body.String())
	}
	// Dispatcher must be unregistered.
	r.mu.RLock()
	_, exists := r.byBotID[914]
	r.mu.RUnlock()
	if exists {
		t.Errorf("bot 914 should have been unregistered")
	}
}

func TestRouter_HandleEvent_BotDelete_UnregistersBot(t *testing.T) {
	fs := newFakeStore()
	tid := uuid.New()
	p := newInstalledPortal(t, fs, tid, "p", "portal.bitrix24.com", "APP")
	r := newRouterForTest()
	defer r.Stop()
	r.RegisterPortal(p)

	disp := newFakeDispatcher(914, tid, "p")
	r.RegisterBot(914, disp)

	v := url.Values{}
	v.Set("event", "ONIMBOTDELETE")
	v.Set("auth[domain]", "portal.bitrix24.com")
	v.Set("auth[application_token]", "APP")
	v.Set("data[BOT][914][BOT_ID]", "914")
	// No MESSAGE_ID — bypasses dedup.

	req := httptest.NewRequest(http.MethodPost, "/bitrix24/events", strings.NewReader(v.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", rec.Code, rec.Body.String())
	}
	// Give async dispatch a beat, then check unregister.
	select {
	case <-disp.events:
	case <-time.After(100 * time.Millisecond):
	}
	r.mu.RLock()
	_, exists := r.byBotID[914]
	r.mu.RUnlock()
	if exists {
		t.Errorf("bot 914 should be unregistered after ONIMBOTDELETE")
	}
}

// ---------------------------------------------------------------------------
// Register / Unregister bookkeeping
// ---------------------------------------------------------------------------

func TestRouter_PortalByDomainAndKey(t *testing.T) {
	fs := newFakeStore()
	tid := uuid.New()
	p := newInstalledPortal(t, fs, tid, "p", "Customer.bitrix24.com", "APP")
	r := newRouterForTest()
	defer r.Stop()
	r.RegisterPortal(p)

	// Lookup case-insensitive on domain.
	if got, ok := r.PortalByDomain("customer.bitrix24.com"); !ok || got != p {
		t.Errorf("PortalByDomain lower-case miss: ok=%v", ok)
	}
	if got, ok := r.PortalByDomain("CUSTOMER.BITRIX24.COM"); !ok || got != p {
		t.Errorf("PortalByDomain upper-case miss: ok=%v", ok)
	}

	if got, ok := r.PortalByKey(tid, "p"); !ok || got != p {
		t.Errorf("PortalByKey miss: ok=%v", ok)
	}

	r.UnregisterPortal(tid, "p")
	if _, ok := r.PortalByKey(tid, "p"); ok {
		t.Error("portal should be gone after Unregister")
	}
	if _, ok := r.PortalByDomain("customer.bitrix24.com"); ok {
		t.Error("domain index should be gone after Unregister")
	}
}

func TestRouter_PortalByDomain_DuplicateDomainFailsClosed(t *testing.T) {
	fs := newFakeStore()
	tid1 := uuid.New()
	tid2 := uuid.New()
	p1 := newInstalledPortal(t, fs, tid1, "p1", "customer.bitrix24.com", "APP1")
	p2 := newInstalledPortal(t, fs, tid2, "p2", "CUSTOMER.bitrix24.com", "APP2")
	r := newRouterForTest()
	defer r.Stop()

	r.RegisterPortal(p1)
	r.RegisterPortal(p2)

	if got, ok := r.PortalByDomain("customer.bitrix24.com"); ok || got != nil {
		t.Fatalf("duplicate domain should fail closed, got ok=%v portal=%v", ok, got)
	}
}

func TestRouter_RegisterBot_IgnoresInvalidInputs(t *testing.T) {
	r := newRouterForTest()
	defer r.Stop()
	r.RegisterBot(0, &fakeDispatcher{})
	r.RegisterBot(-1, &fakeDispatcher{})
	r.RegisterBot(1, nil)
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.byBotID) != 0 {
		t.Fatalf("invalid inputs should be ignored; got %d", len(r.byBotID))
	}
}

// ---------------------------------------------------------------------------
// InitWebhookRouter singleton
// ---------------------------------------------------------------------------

func TestInitWebhookRouter_Singleton(t *testing.T) {
	resetWebhookRouterForTest()
	defer resetWebhookRouterForTest()

	fs := newFakeStore()
	r1, err := InitWebhookRouter(fs, "k", RouterConfig{DedupSweepPeriod: 0})
	if err != nil {
		t.Fatalf("InitWebhookRouter: %v", err)
	}
	r2, _ := InitWebhookRouter(newFakeStore(), "other", RouterConfig{DedupSweepPeriod: 0})
	if r1 != r2 {
		t.Fatal("InitWebhookRouter should return the same instance on repeated calls")
	}
	if WebhookRouter() != r1 {
		t.Fatal("WebhookRouter() should return the singleton")
	}
	r1.Stop()
}

func TestInitWebhookRouter_NilStore(t *testing.T) {
	resetWebhookRouterForTest()
	defer resetWebhookRouterForTest()

	_, err := InitWebhookRouter(nil, "", RouterConfig{})
	if err == nil {
		t.Fatal("expected error for nil store")
	}
}

// ---------------------------------------------------------------------------
// Dispatch panic isolation
// ---------------------------------------------------------------------------

type panickingDispatcher struct {
	tid  uuid.UUID
	name string
}

func (d *panickingDispatcher) BotID() int                                { return 777 }
func (d *panickingDispatcher) TenantID() uuid.UUID                       { return d.tid }
func (d *panickingDispatcher) PortalName() string                        { return d.name }
func (d *panickingDispatcher) DispatchEvent(_ context.Context, _ *Event) { panic("boom") }

func TestRouter_DispatcherPanicIsIsolated(t *testing.T) {
	fs := newFakeStore()
	tid := uuid.New()
	p := newInstalledPortal(t, fs, tid, "p", "portal.bitrix24.com", "APP")
	r := newRouterForTest()
	defer r.Stop()
	r.RegisterPortal(p)
	r.RegisterBot(777, &panickingDispatcher{tid: tid, name: "p"})

	req := httptest.NewRequest(http.MethodPost, "/bitrix24/events",
		buildEventBody("portal.bitrix24.com", "APP", 777, "mX"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	// Yield scheduler to let the goroutine run & recover.
	time.Sleep(50 * time.Millisecond)
}

// ---------------------------------------------------------------------------
// Concurrent dispatch
// ---------------------------------------------------------------------------

func TestRouter_ConcurrentEvents_ProcessedIndependently(t *testing.T) {
	fs := newFakeStore()
	tid := uuid.New()
	p := newInstalledPortal(t, fs, tid, "p", "portal.bitrix24.com", "APP")
	r := newRouterForTest()
	defer r.Stop()
	r.RegisterPortal(p)

	disp := newFakeDispatcher(914, tid, "p")
	disp.events = make(chan *Event, 100)
	r.RegisterBot(914, disp)

	const N = 50
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/bitrix24/events",
				buildEventBody("portal.bitrix24.com", "APP", 914, "m"+strconvItoa(i)))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("req %d: status=%d (body=%s)", i, rec.Code, rec.Body.String())
			}
		}(i)
	}
	wg.Wait()

	// Collect all dispatched events (should equal N).
	timeout := time.After(2 * time.Second)
	received := 0
loop:
	for received < N {
		select {
		case <-disp.events:
			received++
		case <-timeout:
			break loop
		}
	}
	if received != N {
		t.Fatalf("concurrent dispatch count = %d, want %d", received, N)
	}

	// Every message_id should appear in the dedup cache.
	if got := r.dedup.Len(); got < N {
		t.Errorf("dedup len = %d, want >= %d", got, N)
	}

	// Latency sanity check — handler returned within the event loop iteration;
	// since each request ran in <50ms the whole WaitGroup should close quickly.
	_ = atomic.LoadInt32 // silence unused import if ever empty
}
