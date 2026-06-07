//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const browserCookieTestKey = "12345678901234567890123456789012"

func newBrowserCookieStoreFixture(t *testing.T) (*sql.DB, *SQLiteBrowserCookieStore, store.BrowserCookieScope) {
	t.Helper()

	db, err := OpenDB(filepath.Join(t.TempDir(), "browser-cookies.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	scope := store.BrowserCookieScope{
		TenantID: store.MasterTenantID,
		UserID:   "user-a",
		AgentID:  "agent-a",
	}
	return db, NewSQLiteBrowserCookieStore(db, browserCookieTestKey), scope
}

func TestSQLiteBrowserCookieStoreEncryptsAndListsWithinScope(t *testing.T) {
	db, cookies, scope := newBrowserCookieStoreFixture(t)
	expiresAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)

	count, err := cookies.Upsert(context.Background(), scope, []store.BrowserCookie{{
		Domain:    "Example.COM",
		Name:      "session",
		Path:      "",
		Value:     "secret-cookie",
		Secure:    true,
		HTTPOnly:  true,
		SameSite:  "Lax",
		ExpiresAt: &expiresAt,
		Source:    "chrome-extension",
	}})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if count != 1 {
		t.Fatalf("Upsert count = %d, want 1", count)
	}

	var raw string
	if err := db.QueryRow(`SELECT encrypted_value FROM browser_cookies WHERE tenant_id = ?`, scope.TenantID.String()).Scan(&raw); err != nil {
		t.Fatalf("read encrypted_value: %v", err)
	}
	if raw == "secret-cookie" || !crypto.IsEncrypted(raw) {
		t.Fatalf("cookie value not encrypted at rest: %q", raw)
	}

	got, err := cookies.List(context.Background(), scope, store.BrowserCookieFilter{Domain: "example.com"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("List len = %d, want 1", len(got))
	}
	if got[0].Domain != "example.com" || got[0].Path != "/" || got[0].Value != "secret-cookie" {
		t.Fatalf("cookie round trip mismatch: %+v", got[0])
	}
	if !got[0].Secure || !got[0].HTTPOnly || got[0].SameSite != "Lax" {
		t.Fatalf("cookie metadata mismatch: %+v", got[0])
	}
}

func TestSQLiteBrowserCookieStoreIsolatesByUserAndAgent(t *testing.T) {
	_, cookies, scope := newBrowserCookieStoreFixture(t)
	otherUser := scope
	otherUser.UserID = "user-b"
	otherAgent := scope
	otherAgent.AgentID = "agent-b"

	if _, err := cookies.Upsert(context.Background(), scope, []store.BrowserCookie{{
		Domain: "example.com",
		Name:   "session",
		Value:  "secret-cookie",
	}}); err != nil {
		t.Fatalf("Upsert owner: %v", err)
	}
	if _, err := cookies.Upsert(context.Background(), otherUser, []store.BrowserCookie{{
		Domain: "example.com",
		Name:   "session",
		Value:  "other-user-cookie",
	}}); err != nil {
		t.Fatalf("Upsert other user: %v", err)
	}
	if _, err := cookies.Upsert(context.Background(), otherAgent, []store.BrowserCookie{{
		Domain: "example.com",
		Name:   "session",
		Value:  "other-agent-cookie",
	}}); err != nil {
		t.Fatalf("Upsert other agent: %v", err)
	}

	got, err := cookies.List(context.Background(), scope, store.BrowserCookieFilter{})
	if err != nil {
		t.Fatalf("List owner: %v", err)
	}
	if len(got) != 1 || got[0].Value != "secret-cookie" {
		t.Fatalf("owner list leaked wrong cookies: %+v", got)
	}
}

func TestSQLiteBrowserCookieStoreDeleteAndExpiry(t *testing.T) {
	_, cookies, scope := newBrowserCookieStoreFixture(t)
	expiredAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	validAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)

	if _, err := cookies.Upsert(context.Background(), scope, []store.BrowserCookie{
		{Domain: "example.com", Name: "expired", Value: "expired", ExpiresAt: &expiredAt},
		{Domain: "example.com", Name: "valid", Value: "valid", ExpiresAt: &validAt},
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := cookies.List(context.Background(), scope, store.BrowserCookieFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Name != "valid" {
		t.Fatalf("expiry filter mismatch: %+v", got)
	}

	deleted, err := cookies.Delete(context.Background(), scope, store.BrowserCookieFilter{Name: "valid"})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("Delete count = %d, want 1", deleted)
	}
	got, err = cookies.List(context.Background(), scope, store.BrowserCookieFilter{})
	if err != nil {
		t.Fatalf("List after delete: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("List after delete = %+v, want empty", got)
	}
}

func TestSQLiteBrowserCookieStoreRequiresEncryptionKey(t *testing.T) {
	db, _, scope := newBrowserCookieStoreFixture(t)
	cookies := NewSQLiteBrowserCookieStore(db, "")

	_, err := cookies.Upsert(context.Background(), scope, []store.BrowserCookie{{
		Domain: "example.com",
		Name:   "session",
		Value:  "secret-cookie",
	}})
	if !errors.Is(err, store.ErrBrowserCookieEncryptionRequired) {
		t.Fatalf("Upsert err = %v, want ErrBrowserCookieEncryptionRequired", err)
	}

	_, err = cookies.List(context.Background(), scope, store.BrowserCookieFilter{})
	if !errors.Is(err, store.ErrBrowserCookieEncryptionRequired) {
		t.Fatalf("List err = %v, want ErrBrowserCookieEncryptionRequired", err)
	}
}
