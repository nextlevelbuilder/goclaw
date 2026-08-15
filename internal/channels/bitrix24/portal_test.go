package bitrix24

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// fakeBitrixStore is an in-memory BitrixPortalStore for unit tests.
// Mirrors the real store contract closely enough for portal-runtime
// behaviour: keyed by (tenant_id, name); blobs stored verbatim (no encrypt).
type fakeBitrixStore struct {
	mu   sync.Mutex
	rows map[string]*store.BitrixPortalData // key: tenant.String()+":"+name

	updateStateErr error // injected error for negative tests
	stateUpdates   int32 // atomic counter for assertions
}

func newFakeStore() *fakeBitrixStore {
	return &fakeBitrixStore{rows: map[string]*store.BitrixPortalData{}}
}

func (f *fakeBitrixStore) key(tid uuid.UUID, name string) string {
	return tid.String() + ":" + name
}

func (f *fakeBitrixStore) seed(tid uuid.UUID, name, domain string, creds, state []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	row := &store.BitrixPortalData{
		TenantID:    tid,
		Name:        name,
		Domain:      domain,
		Credentials: creds,
		State:       state,
	}
	row.ID = store.GenNewID()
	row.CreatedAt = time.Now()
	row.UpdatedAt = time.Now()
	f.rows[f.key(tid, name)] = row
}

func (f *fakeBitrixStore) Create(_ context.Context, p *store.BitrixPortalData) error {
	if p == nil {
		return errors.New("nil portal")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if p.ID == uuid.Nil {
		p.ID = store.GenNewID()
	}
	now := time.Now()
	p.CreatedAt = now
	p.UpdatedAt = now
	f.rows[f.key(p.TenantID, p.Name)] = p
	return nil
}

func (f *fakeBitrixStore) GetByName(_ context.Context, tid uuid.UUID, name string) (*store.BitrixPortalData, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.rows[f.key(tid, name)]
	if !ok {
		return nil, errors.New("not found")
	}
	cp := *row
	return &cp, nil
}

func (f *fakeBitrixStore) ListByTenant(_ context.Context, tid uuid.UUID) ([]store.BitrixPortalData, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.BitrixPortalData
	prefix := tid.String() + ":"
	for k, v := range f.rows {
		if strings.HasPrefix(k, prefix) {
			out = append(out, *v)
		}
	}
	return out, nil
}

func (f *fakeBitrixStore) ListAllForLoader(_ context.Context) ([]store.BitrixPortalData, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.BitrixPortalData
	for _, v := range f.rows {
		out = append(out, *v)
	}
	return out, nil
}

func (f *fakeBitrixStore) UpdateCredentials(_ context.Context, tid uuid.UUID, name string, creds []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.rows[f.key(tid, name)]
	if !ok {
		return errors.New("not found")
	}
	row.Credentials = append(row.Credentials[:0], creds...)
	return nil
}

func (f *fakeBitrixStore) UpdateState(_ context.Context, tid uuid.UUID, name string, state []byte) error {
	if f.updateStateErr != nil {
		return f.updateStateErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.rows[f.key(tid, name)]
	if !ok {
		return errors.New("not found")
	}
	row.State = append(row.State[:0], state...)
	atomic.AddInt32(&f.stateUpdates, 1)
	return nil
}

func (f *fakeBitrixStore) Delete(_ context.Context, tid uuid.UUID, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rows, f.key(tid, name))
	return nil
}

// makeRefreshHandler builds an OAuth handler that returns the given access
// token + refreshExpiry, and counts hits via *int32 atomic.
func makeRefreshHandler(t *testing.T, hits *int32, accessToken string, expiresIn int64, fail bool) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		if fail {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"expired"}`))
			return
		}
		body, _ := json.Marshal(TokenResponse{
			AccessToken:    accessToken,
			RefreshToken:   "RT-rotated",
			ExpiresIn:      expiresIn,
			Domain:         "portal.bitrix24.com",
			MemberID:       "mem1",
			ClientEndpoint: "https://portal.bitrix24.com/rest/",
		})
		_, _ = w.Write(body)
	}
}

// newTestPortal builds a Portal whose internal client routes OAuth calls
// to the supplied httptest server.
func newTestPortal(t *testing.T, srv *httptest.Server, fs *fakeBitrixStore, tid uuid.UUID, name string, initialState store.BitrixPortalState) *Portal {
	t.Helper()
	creds, _ := json.Marshal(store.BitrixPortalCredentials{ClientID: "cid", ClientSecret: "secret"})
	stateBytes, _ := json.Marshal(initialState)
	fs.seed(tid, name, "portal.bitrix24.com", creds, stateBytes)

	p, err := NewPortal(context.Background(), tid, name, fs, "")
	if err != nil {
		t.Fatalf("NewPortal: %v", err)
	}
	// Swap the client's transport so OAuth calls hit the test server.
	p.client.http = &http.Client{Transport: &rewriteRT{target: srv.URL, base: http.DefaultTransport}}
	return p
}

func TestNewPortal_ValidatesInputs(t *testing.T) {
	fs := newFakeStore()
	tid := store.GenNewID()
	ctx := context.Background()

	if _, err := NewPortal(ctx, tid, "p", nil, ""); err == nil {
		t.Fatal("expected error on nil store")
	}
	if _, err := NewPortal(ctx, uuid.Nil, "p", fs, ""); err == nil {
		t.Fatal("expected error on nil tenant_id")
	}
	if _, err := NewPortal(ctx, tid, "", fs, ""); err == nil {
		t.Fatal("expected error on empty name")
	}
}

func TestNewPortal_RequiresCredentials(t *testing.T) {
	fs := newFakeStore()
	tid := store.GenNewID()
	// Seed row with empty credentials.
	fs.seed(tid, "p", "portal.bitrix24.com", []byte("{}"), nil)

	_, err := NewPortal(context.Background(), tid, "p", fs, "")
	if err == nil || !strings.Contains(err.Error(), "client_id/client_secret") {
		t.Fatalf("expected credentials-missing error, got %v", err)
	}
}

func TestPortal_Exchange_PersistsTokens(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(makeRefreshHandler(t, &hits, "AT-fresh", 3600, false))
	defer srv.Close()

	fs := newFakeStore()
	tid := store.GenNewID()
	p := newTestPortal(t, srv, fs, tid, "p", store.BitrixPortalState{})

	if err := p.Exchange(context.Background(), "code-xyz"); err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if !p.Installed() {
		t.Fatal("expected portal installed after Exchange")
	}
	if got := p.MemberID(); got != "mem1" {
		t.Fatalf("MemberID: %q", got)
	}

	// State should have been persisted with the fresh tokens.
	row, _ := fs.GetByName(context.Background(), tid, "p")
	var st store.BitrixPortalState
	if err := json.Unmarshal(row.State, &st); err != nil {
		t.Fatalf("decode persisted state: %v", err)
	}
	if st.AccessToken != "AT-fresh" || st.RefreshToken != "RT-rotated" {
		t.Fatalf("persisted tokens wrong: %+v", st)
	}
	if st.ExpiresAt.IsZero() {
		t.Fatal("ExpiresAt not set")
	}
}

func TestPortal_Exchange_RejectsEmptyCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("server should not be called for empty code")
		_ = w
	}))
	defer srv.Close()

	fs := newFakeStore()
	tid := store.GenNewID()
	p := newTestPortal(t, srv, fs, tid, "p", store.BitrixPortalState{})

	if err := p.Exchange(context.Background(), ""); err == nil {
		t.Fatal("expected error on empty code")
	}
}

func TestPortal_Exchange_RejectsDomainMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"access_token":"AT","refresh_token":"RT","expires_in":3600,
			"domain":"attacker.bitrix24.com","member_id":"mem1"
		}`))
	}))
	defer srv.Close()

	fs := newFakeStore()
	tid := store.GenNewID()
	p := newTestPortal(t, srv, fs, tid, "p", store.BitrixPortalState{})

	err := p.Exchange(context.Background(), "code-xyz")
	if err == nil || !strings.Contains(err.Error(), "domain mismatch") {
		t.Fatalf("expected domain mismatch, got %v", err)
	}
	if p.Installed() {
		t.Fatal("portal must not persist tokens from another domain")
	}
	if atomic.LoadInt32(&fs.stateUpdates) != 0 {
		t.Fatalf("state updates = %d, want 0", fs.stateUpdates)
	}
}

func TestPortal_InstallFromTokens_ValidatesAccessTokenBeforePersist(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/profile.json" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"INVALID_TOKEN","error_description":"bad auth"}`))
	}))
	defer srv.Close()

	fs := newFakeStore()
	tid := store.GenNewID()
	p := newTestPortal(t, srv, fs, tid, "p", store.BitrixPortalState{})

	err := p.InstallFromTokens(context.Background(), &TokenResponse{
		AccessToken:  "forged",
		RefreshToken: "RT",
		Domain:       "portal.bitrix24.com",
		MemberID:     "mem1",
	})
	if err == nil || !strings.Contains(err.Error(), "validate access token") {
		t.Fatalf("expected validation error, got %v", err)
	}
	if p.Installed() {
		t.Fatal("portal must not persist unvalidated Local App tokens")
	}
	if atomic.LoadInt32(&fs.stateUpdates) != 0 {
		t.Fatalf("state updates = %d, want 0", fs.stateUpdates)
	}
}

func TestPortal_AccessToken_ReturnsCachedWhenFresh(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("refresh should not happen for fresh token")
		_ = w
	}))
	defer srv.Close()

	fs := newFakeStore()
	tid := store.GenNewID()
	// Token expires in 1h — well outside the 5-min buffer.
	p := newTestPortal(t, srv, fs, tid, "p", store.BitrixPortalState{
		AccessToken:  "STILL-FRESH",
		RefreshToken: "RT",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
	})

	tok, err := p.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if tok != "STILL-FRESH" {
		t.Fatalf("expected cached token, got %q", tok)
	}
}

func TestPortal_AccessToken_RequiresInstall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = w
	}))
	defer srv.Close()

	fs := newFakeStore()
	tid := store.GenNewID()
	p := newTestPortal(t, srv, fs, tid, "p", store.BitrixPortalState{}) // no refresh token

	_, err := p.AccessToken(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("expected not-installed error, got %v", err)
	}
}

func TestPortal_AccessToken_TriggersRefreshNearExpiry(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(makeRefreshHandler(t, &hits, "AT-refreshed", 3600, false))
	defer srv.Close()

	fs := newFakeStore()
	tid := store.GenNewID()
	// Token expires in 1 minute — inside 5-min buffer → must refresh.
	p := newTestPortal(t, srv, fs, tid, "p", store.BitrixPortalState{
		AccessToken:  "STALE",
		RefreshToken: "RT-old",
		ExpiresAt:    time.Now().Add(1 * time.Minute),
	})

	tok, err := p.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if tok != "AT-refreshed" {
		t.Fatalf("expected refreshed token, got %q", tok)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("expected exactly 1 OAuth hit, got %d", got)
	}
}

func TestPortal_AccessToken_SingleflightCoalescesConcurrent(t *testing.T) {
	var hits int32
	// Slow handler to widen the singleflight window.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		time.Sleep(80 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"AT-coalesced","refresh_token":"RT2","expires_in":3600,"domain":"portal.bitrix24.com"}`))
	}))
	defer srv.Close()

	fs := newFakeStore()
	tid := store.GenNewID()
	p := newTestPortal(t, srv, fs, tid, "p", store.BitrixPortalState{
		AccessToken:  "STALE",
		RefreshToken: "RT-old",
		ExpiresAt:    time.Now().Add(30 * time.Second),
	})

	const N = 20
	var wg sync.WaitGroup
	wg.Add(N)
	results := make([]string, N)
	errs := make([]error, N)
	for i := 0; i < N; i++ {
		go func(idx int) {
			defer wg.Done()
			tok, err := p.AccessToken(context.Background())
			results[idx], errs[idx] = tok, err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if results[i] != "AT-coalesced" {
			t.Fatalf("call %d: token %q", i, results[i])
		}
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("singleflight should have coalesced %d concurrent calls into 1 OAuth hit, got %d", N, got)
	}
}

func TestPortal_Refresh_FailureIncrementsCounter(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(makeRefreshHandler(t, &hits, "", 0, true))
	defer srv.Close()

	fs := newFakeStore()
	tid := store.GenNewID()
	p := newTestPortal(t, srv, fs, tid, "p", store.BitrixPortalState{
		AccessToken:  "STALE",
		RefreshToken: "RT-old",
		ExpiresAt:    time.Now().Add(30 * time.Second),
	})

	_, err := p.AccessToken(context.Background())
	if err == nil {
		t.Fatal("expected error from failing refresh")
	}

	row, _ := fs.GetByName(context.Background(), tid, "p")
	var st store.BitrixPortalState
	_ = json.Unmarshal(row.State, &st)
	if st.ConsecutiveFail != 1 {
		t.Fatalf("ConsecutiveFail = %d, want 1", st.ConsecutiveFail)
	}
	if st.LastRefreshError == "" {
		t.Fatal("LastRefreshError should be populated")
	}
}

func TestPortal_RecordRegisteredBot_Persists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()

	fs := newFakeStore()
	tid := store.GenNewID()
	p := newTestPortal(t, srv, fs, tid, "p", store.BitrixPortalState{
		RefreshToken: "RT", AccessToken: "AT", ExpiresAt: time.Now().Add(time.Hour),
	})

	if err := p.RecordRegisteredBot(context.Background(), "support_bot", 12345); err != nil {
		t.Fatalf("RecordRegisteredBot: %v", err)
	}
	id, ok := p.LookupRegisteredBot("support_bot")
	if !ok || id != 12345 {
		t.Fatalf("LookupRegisteredBot: ok=%v id=%d", ok, id)
	}

	// Check it was persisted to store.
	row, _ := fs.GetByName(context.Background(), tid, "p")
	var st store.BitrixPortalState
	_ = json.Unmarshal(row.State, &st)
	if st.RegisteredBots["support_bot"] != 12345 {
		t.Fatalf("bot not persisted: %v", st.RegisteredBots)
	}

	if err := p.RecordRegisteredBot(context.Background(), "", 1); err == nil {
		t.Fatal("expected error on empty code")
	}
}

func TestPortal_SaveMediaFolder_Persists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()

	fs := newFakeStore()
	tid := store.GenNewID()
	p := newTestPortal(t, srv, fs, tid, "p", store.BitrixPortalState{
		RefreshToken: "RT", AccessToken: "AT", ExpiresAt: time.Now().Add(time.Hour),
	})

	if err := p.SaveMediaFolder(context.Background(), "support_bot", "folder-99"); err != nil {
		t.Fatalf("SaveMediaFolder: %v", err)
	}
	if got := p.LookupMediaFolder("support_bot"); got != "folder-99" {
		t.Fatalf("LookupMediaFolder: %q", got)
	}

	row, _ := fs.GetByName(context.Background(), tid, "p")
	var st store.BitrixPortalState
	_ = json.Unmarshal(row.State, &st)
	if st.MediaFolders["support_bot"] != "folder-99" {
		t.Fatalf("folder not persisted: %v", st.MediaFolders)
	}
}

func TestPortal_HandleInstall_Success(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(makeRefreshHandler(t, &hits, "AT-installed", 3600, false))
	defer srv.Close()

	fs := newFakeStore()
	tid := store.GenNewID()
	p := newTestPortal(t, srv, fs, tid, "myportal", store.BitrixPortalState{})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, fmt.Sprintf(
		"/bitrix24/install?code=AUTHCODE&domain=portal.bitrix24.com&state=%s:myportal", tid),
		nil)
	p.HandleInstall(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: %d, body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Installation successful") {
		t.Fatal("missing success page")
	}
	if !p.Installed() {
		t.Fatal("portal should be installed after handler")
	}
}

func TestPortal_HandleInstall_RejectsBadState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()

	fs := newFakeStore()
	tid := store.GenNewID()
	p := newTestPortal(t, srv, fs, tid, "myportal", store.BitrixPortalState{})

	cases := []struct {
		name     string
		query    string
		wantCode int
	}{
		{"missing code", "state=" + tid.String() + ":myportal", http.StatusBadRequest},
		{"missing state", "code=X", http.StatusBadRequest},
		{"malformed state", "code=X&state=nocolon", http.StatusBadRequest},
		{"bad tenant uuid", "code=X&state=not-a-uuid:myportal", http.StatusForbidden},
		{"wrong tenant", "code=X&state=" + uuid.NewString() + ":myportal", http.StatusForbidden},
		{"wrong portal name", "code=X&state=" + tid.String() + ":otherportal", http.StatusForbidden},
		{"wrong domain", "code=X&domain=evil.bitrix24.com&state=" + tid.String() + ":myportal", http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/bitrix24/install?"+tc.query, nil)
			p.HandleInstall(w, r)
			if w.Code != tc.wantCode {
				t.Fatalf("got %d, want %d (body: %s)", w.Code, tc.wantCode, w.Body.String())
			}
		})
	}
}

func TestPortal_HandleInstall_ExchangeFailure(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(makeRefreshHandler(t, &hits, "", 0, true))
	defer srv.Close()

	fs := newFakeStore()
	tid := store.GenNewID()
	p := newTestPortal(t, srv, fs, tid, "myportal", store.BitrixPortalState{})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, fmt.Sprintf(
		"/bitrix24/install?code=AUTHCODE&state=%s:myportal", tid),
		nil)
	p.HandleInstall(w, r)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on exchange failure, got %d", w.Code)
	}
}

func TestPortal_Stop_Idempotent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()

	fs := newFakeStore()
	tid := store.GenNewID()
	p := newTestPortal(t, srv, fs, tid, "p", store.BitrixPortalState{
		RefreshToken: "RT", AccessToken: "AT", ExpiresAt: time.Now().Add(time.Hour),
	})

	// Should not panic on multiple Stop() calls.
	p.Stop()
	p.Stop()
	p.Stop()
}

// ---------------------------------------------------------------------------
// UpdatePublicURL / PublicURL — install-captured gateway URL
// ---------------------------------------------------------------------------

func TestPortal_UpdatePublicURL_FirstSet_PersistsState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	fs := newFakeStore()
	tid := store.GenNewID()
	p := newTestPortal(t, srv, fs, tid, "p", store.BitrixPortalState{})

	if got := p.PublicURL(); got != "" {
		t.Fatalf("expected empty initial PublicURL, got %q", got)
	}

	if err := p.UpdatePublicURL(context.Background(), "https://goclaw.tamgiac.com"); err != nil {
		t.Fatalf("UpdatePublicURL: %v", err)
	}
	if got := p.PublicURL(); got != "https://goclaw.tamgiac.com" {
		t.Fatalf("PublicURL = %q, want stored value", got)
	}
	if atomic.LoadInt32(&fs.stateUpdates) != 1 {
		t.Fatalf("expected 1 state write, got %d", fs.stateUpdates)
	}

	// Reload from store to verify the write made it past in-memory state.
	p2, err := NewPortal(context.Background(), tid, "p", fs, "")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := p2.PublicURL(); got != "https://goclaw.tamgiac.com" {
		t.Fatalf("reloaded PublicURL = %q", got)
	}
}

func TestPortal_UpdatePublicURL_Idempotent_NoOpWrite(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	fs := newFakeStore()
	tid := store.GenNewID()
	p := newTestPortal(t, srv, fs, tid, "p", store.BitrixPortalState{
		PublicURL: "https://goclaw.tamgiac.com",
	})

	// Same value → no write, no error.
	if err := p.UpdatePublicURL(context.Background(), "https://goclaw.tamgiac.com"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if writes := atomic.LoadInt32(&fs.stateUpdates); writes != 0 {
		t.Fatalf("expected 0 state writes on no-op, got %d", writes)
	}
}

func TestPortal_UpdatePublicURL_RejectsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	fs := newFakeStore()
	tid := store.GenNewID()
	p := newTestPortal(t, srv, fs, tid, "p", store.BitrixPortalState{})

	if err := p.UpdatePublicURL(context.Background(), ""); err == nil {
		t.Fatal("expected error on empty URL")
	}
}

func TestPortal_UpdatePublicURL_Changed_OverwritesAndPersists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	fs := newFakeStore()
	tid := store.GenNewID()
	p := newTestPortal(t, srv, fs, tid, "p", store.BitrixPortalState{
		PublicURL: "https://old.example.com",
	})

	if err := p.UpdatePublicURL(context.Background(), "https://new.example.com"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := p.PublicURL(); got != "https://new.example.com" {
		t.Fatalf("PublicURL = %q", got)
	}
	if atomic.LoadInt32(&fs.stateUpdates) != 1 {
		t.Fatalf("expected 1 state write on URL change, got %d", fs.stateUpdates)
	}
}

func TestPortal_UpdatePublicURL_StoreFailurePropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	fs := newFakeStore()
	fs.updateStateErr = errors.New("boom")
	tid := store.GenNewID()
	p := newTestPortal(t, srv, fs, tid, "p", store.BitrixPortalState{})

	if err := p.UpdatePublicURL(context.Background(), "https://goclaw.tamgiac.com"); err == nil {
		t.Fatal("expected store error to propagate")
	}
	// In-memory state still updated (acceptable — next write retry will sync).
	// We document this in the method docstring; assert behaviour.
	if got := p.PublicURL(); got != "https://goclaw.tamgiac.com" {
		t.Fatalf("expected in-memory update despite persist failure, got %q", got)
	}
}

// _ silence any unused warnings if reorganized later.
var _ = url.Parse
