package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/browser"
)

type fakeStoreCookieProviderStore struct {
	items []store.BrowserCookie
	scope store.BrowserCookieScope
}

func (f *fakeStoreCookieProviderStore) Upsert(context.Context, store.BrowserCookieScope, []store.BrowserCookie) (int, error) {
	return 0, nil
}

func (f *fakeStoreCookieProviderStore) List(_ context.Context, scope store.BrowserCookieScope, _ store.BrowserCookieFilter) ([]store.BrowserCookie, error) {
	f.scope = scope
	return f.items, nil
}

func (f *fakeStoreCookieProviderStore) Delete(context.Context, store.BrowserCookieScope, store.BrowserCookieFilter) (int, error) {
	return 0, nil
}

func TestStoreBrowserCookieProviderFiltersAndConvertsCookies(t *testing.T) {
	tenantID := uuid.New()
	expiresAt := time.Now().UTC().Add(time.Hour)
	fake := &fakeStoreCookieProviderStore{items: []store.BrowserCookie{
		{Domain: ".example.com", Name: "parent", Path: "/", Value: "parent-secret", Secure: true, HTTPOnly: true, SameSite: "Lax", ExpiresAt: &expiresAt},
		{Domain: "app.example.com", Name: "host", Path: "/app", Value: "host-secret"},
		{Domain: "other.example.com", Name: "skip", Path: "/", Value: "skip"},
	}}
	provider := newStoreBrowserCookieProvider(fake)

	got, err := provider.CookiesForURL(context.Background(), browser.BrowserScope{
		TenantID: tenantID.String(),
		UserID:   "user-a",
		AgentID:  "agent-a",
	}, "https://app.example.com/app/page")
	if err != nil {
		t.Fatalf("CookiesForURL: %v", err)
	}
	if fake.scope.TenantID != tenantID || fake.scope.UserID != "user-a" || fake.scope.AgentID != "agent-a" {
		t.Fatalf("scope = %+v", fake.scope)
	}
	if len(got) != 2 {
		t.Fatalf("cookies len = %d, want 2: %+v", len(got), got)
	}
	if got[0].Name != "parent" || got[0].Domain != ".example.com" || !got[0].HTTPOnly {
		t.Fatalf("parent cookie mismatch: %+v", got[0])
	}
	if got[1].Name != "host" || got[1].Domain != "" || got[1].URL == "" {
		t.Fatalf("host cookie should be URL-scoped: %+v", got[1])
	}
}

func TestBrowserCookieMatchesURLRejectsExpiredAndWrongPath(t *testing.T) {
	now := time.Now().UTC()
	expiredAt := now.Add(-time.Minute)
	if browserCookieMatchesURL(store.BrowserCookie{Domain: "example.com", Name: "expired", Path: "/", ExpiresAt: &expiredAt}, "example.com", "/", now) {
		t.Fatal("expired cookie matched")
	}
	if browserCookieMatchesURL(store.BrowserCookie{Domain: "example.com", Name: "wrong-path", Path: "/admin"}, "example.com", "/app", now) {
		t.Fatal("wrong-path cookie matched")
	}
	if !browserCookieMatchesURL(store.BrowserCookie{Domain: ".example.com", Name: "sub", Path: "/"}, "app.example.com", "/app", now) {
		t.Fatal("parent-domain cookie did not match subdomain")
	}
}
