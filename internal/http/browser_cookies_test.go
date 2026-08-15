package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type fakeBrowserCookieStore struct {
	upsertScope store.BrowserCookieScope
	upsertItems []store.BrowserCookie
	listItems   []store.BrowserCookie
	deleteScope store.BrowserCookieScope
	deleteCount int
	err         error
}

func (f *fakeBrowserCookieStore) Upsert(_ context.Context, scope store.BrowserCookieScope, cookies []store.BrowserCookie) (int, error) {
	f.upsertScope = scope
	f.upsertItems = append([]store.BrowserCookie(nil), cookies...)
	if f.err != nil {
		return 0, f.err
	}
	return len(cookies), nil
}

func (f *fakeBrowserCookieStore) List(_ context.Context, scope store.BrowserCookieScope, _ store.BrowserCookieFilter) ([]store.BrowserCookie, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]store.BrowserCookie, 0, len(f.listItems))
	for _, c := range f.listItems {
		if c.TenantID == scope.TenantID && c.UserID == scope.UserID && c.AgentID == scope.AgentID {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeBrowserCookieStore) Delete(_ context.Context, scope store.BrowserCookieScope, _ store.BrowserCookieFilter) (int, error) {
	f.deleteScope = scope
	if f.err != nil {
		return 0, f.err
	}
	return f.deleteCount, nil
}

func browserCookieRequestContext() context.Context {
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	ctx = store.WithUserID(ctx, "user-a")
	ctx = store.WithRole(ctx, "operator")
	return ctx
}

func TestBrowserCookiesHandlerSyncUsesAuthScopeAndNormalizesPayload(t *testing.T) {
	fake := &fakeBrowserCookieStore{}
	handler := NewBrowserCookiesHandler(fake)
	body := strings.NewReader(`{
		"agent_id":"agent-a",
		"user_id":"malicious-user",
		"cookies":[{
			"url":"https://example.com/app",
			"name":"session",
			"value":"secret-cookie",
			"httpOnly":true,
			"sameSite":"Lax"
		}]
	}`)
	req := httptest.NewRequest(nethttp.MethodPost, "/v1/browser/cookies/sync", body).WithContext(browserCookieRequestContext())
	rec := httptest.NewRecorder()

	handler.handleSync(rec, req)

	if rec.Code != nethttp.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if fake.upsertScope.UserID != "user-a" || fake.upsertScope.AgentID != "agent-a" {
		t.Fatalf("scope = %+v, want auth user + request agent", fake.upsertScope)
	}
	if len(fake.upsertItems) != 1 {
		t.Fatalf("upsert items = %d, want 1", len(fake.upsertItems))
	}
	got := fake.upsertItems[0]
	if got.Domain != "example.com" || got.Path != "/" || got.Value != "secret-cookie" || !got.HTTPOnly {
		t.Fatalf("normalized cookie mismatch: %+v", got)
	}
}

func TestBrowserCookiesHandlerListNeverReturnsCookieValue(t *testing.T) {
	updatedAt := time.Now().UTC()
	fake := &fakeBrowserCookieStore{
		listItems: []store.BrowserCookie{{
			TenantID:  store.MasterTenantID,
			UserID:    "user-a",
			AgentID:   "agent-a",
			Domain:    "example.com",
			Name:      "session",
			Path:      "/",
			Value:     "secret-cookie",
			HTTPOnly:  true,
			UpdatedAt: updatedAt,
		}},
	}
	handler := NewBrowserCookiesHandler(fake)
	req := httptest.NewRequest(nethttp.MethodGet, "/v1/browser/cookies?agent_id=agent-a", nil).WithContext(browserCookieRequestContext())
	rec := httptest.NewRecorder()

	handler.handleList(rec, req)

	if rec.Code != nethttp.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]json.RawMessage
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if bytes.Contains(body["items"], []byte("secret-cookie")) || bytes.Contains(body["items"], []byte("value")) {
		t.Fatalf("list leaked cookie value: %s", body["items"])
	}
}

func TestBrowserCookiesHandlerRequiresUserAndAgentScope(t *testing.T) {
	handler := NewBrowserCookiesHandler(&fakeBrowserCookieStore{})
	req := httptest.NewRequest(nethttp.MethodPost, "/v1/browser/cookies/sync", strings.NewReader(`{"cookies":[{"domain":"example.com","name":"session","value":"v"}]}`))
	req = req.WithContext(store.WithTenantID(context.Background(), store.MasterTenantID))
	rec := httptest.NewRecorder()

	handler.handleSync(rec, req)

	if rec.Code != nethttp.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestBrowserCookiesHandlerEncryptionMisconfigIsUnavailable(t *testing.T) {
	handler := NewBrowserCookiesHandler(&fakeBrowserCookieStore{err: store.ErrBrowserCookieEncryptionRequired})
	req := httptest.NewRequest(nethttp.MethodPost, "/v1/browser/cookies/sync", strings.NewReader(`{
		"agent_id":"agent-a",
		"cookies":[{"domain":"example.com","name":"session","value":"v"}]
	}`)).WithContext(browserCookieRequestContext())
	rec := httptest.NewRecorder()

	handler.handleSync(rec, req)

	if rec.Code != nethttp.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestBrowserCookiesHandlerRejectsOversizedCookieValue(t *testing.T) {
	handler := NewBrowserCookiesHandler(&fakeBrowserCookieStore{})
	tooLarge := strings.Repeat("x", maxBrowserCookieValueBytes+1)
	body, _ := json.Marshal(map[string]any{
		"agent_id": "agent-a",
		"cookies": []map[string]any{{
			"domain": "example.com",
			"name":   "session",
			"value":  tooLarge,
		}},
	})
	req := httptest.NewRequest(nethttp.MethodPost, "/v1/browser/cookies/sync", bytes.NewReader(body)).WithContext(browserCookieRequestContext())
	rec := httptest.NewRecorder()

	handler.handleSync(rec, req)

	if rec.Code != nethttp.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "cookie value too large") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestBrowserCookiesHandlerDeleteUsesScopedStore(t *testing.T) {
	fake := &fakeBrowserCookieStore{deleteCount: 1}
	handler := NewBrowserCookiesHandler(fake)
	req := httptest.NewRequest(nethttp.MethodDelete, "/v1/browser/cookies?agent_id=agent-a&domain=example.com&name=session", nil)
	req = req.WithContext(browserCookieRequestContext())
	rec := httptest.NewRecorder()

	handler.handleDelete(rec, req)

	if rec.Code != nethttp.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if fake.deleteScope.UserID != "user-a" || fake.deleteScope.AgentID != "agent-a" {
		t.Fatalf("delete scope = %+v", fake.deleteScope)
	}
}

func TestBrowserCookiesHandlerGenericStoreErrorsAreInternal(t *testing.T) {
	handler := NewBrowserCookiesHandler(&fakeBrowserCookieStore{err: errors.New("db down")})
	req := httptest.NewRequest(nethttp.MethodGet, "/v1/browser/cookies?agent_id=agent-a", nil).WithContext(browserCookieRequestContext())
	rec := httptest.NewRecorder()

	handler.handleList(rec, req)

	if rec.Code != nethttp.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
