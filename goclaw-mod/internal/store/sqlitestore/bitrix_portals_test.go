//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const bitrixTestEncKey = "0123456789abcdef0123456789abcdef" // 32 bytes for AES-256

func newTestSQLiteBitrixPortalStore(t *testing.T, encKey string) (*SQLiteBitrixPortalStore, *sql.DB, uuid.UUID) {
	t.Helper()

	db, err := OpenDB(filepath.Join(t.TempDir(), "bitrix.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	// Create a tenant row so FK constraint is satisfied.
	tenantID := store.GenNewID()
	if _, err := db.Exec(
		`INSERT INTO tenants (id, name, slug, status, settings, created_at, updated_at)
		 VALUES (?, 'test-tenant', 'test-tenant', 'active', '{}', datetime('now'), datetime('now'))`,
		tenantID,
	); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}

	return NewSQLiteBitrixPortalStore(db, encKey), db, tenantID
}

func TestSQLiteBitrixPortalStore_CreateAndGet(t *testing.T) {
	ps, _, tenantID := newTestSQLiteBitrixPortalStore(t, bitrixTestEncKey)
	ctx := context.Background()

	p := &store.BitrixPortalData{
		TenantID:    tenantID,
		Name:        "prod",
		Domain:      "example.bitrix24.com",
		Credentials: []byte(`{"client_id":"abc","client_secret":"shh"}`),
		State:       []byte(`{"access_token":"tkn"}`),
	}
	if err := ps.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if p.ID == uuid.Nil {
		t.Fatal("expected ID to be assigned")
	}

	got, err := ps.GetByName(ctx, tenantID, "prod")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got.ID != p.ID {
		t.Fatalf("id mismatch: got %v, want %v", got.ID, p.ID)
	}
	if got.Domain != "example.bitrix24.com" {
		t.Fatalf("domain mismatch: got %q", got.Domain)
	}
	if string(got.Credentials) != `{"client_id":"abc","client_secret":"shh"}` {
		t.Fatalf("credentials decrypt mismatch: got %q", got.Credentials)
	}
	if string(got.State) != `{"access_token":"tkn"}` {
		t.Fatalf("state decrypt mismatch: got %q", got.State)
	}
}

func TestSQLiteBitrixPortalStore_EncryptsOnDisk(t *testing.T) {
	ps, db, tenantID := newTestSQLiteBitrixPortalStore(t, bitrixTestEncKey)
	ctx := context.Background()

	plaintext := `{"client_id":"visible"}`
	p := &store.BitrixPortalData{
		TenantID:    tenantID,
		Name:        "enc",
		Domain:      "enc.bitrix24.com",
		Credentials: []byte(plaintext),
	}
	if err := ps.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Read raw bytes directly — should NOT contain plaintext.
	var raw []byte
	if err := db.QueryRowContext(ctx,
		`SELECT credentials FROM bitrix_portals WHERE tenant_id = ? AND name = ?`,
		tenantID.String(), "enc",
	).Scan(&raw); err != nil {
		t.Fatalf("raw query: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("expected non-empty credentials bytes")
	}
	if string(raw) == plaintext {
		t.Fatal("credentials stored as plaintext on disk; expected AES-GCM ciphertext")
	}
}

func TestSQLiteBitrixPortalStore_EmptyKeyPassThrough(t *testing.T) {
	ps, db, tenantID := newTestSQLiteBitrixPortalStore(t, "") // empty key — no encryption
	ctx := context.Background()

	plaintext := `{"client_id":"pt"}`
	if err := ps.Create(ctx, &store.BitrixPortalData{
		TenantID:    tenantID,
		Name:        "pt",
		Domain:      "pt.bitrix24.com",
		Credentials: []byte(plaintext),
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var raw []byte
	if err := db.QueryRowContext(ctx,
		`SELECT credentials FROM bitrix_portals WHERE tenant_id = ? AND name = ?`,
		tenantID.String(), "pt",
	).Scan(&raw); err != nil {
		t.Fatalf("raw query: %v", err)
	}
	if string(raw) != plaintext {
		t.Fatalf("empty-key mode should pass-through; got %q", raw)
	}
}

func TestSQLiteBitrixPortalStore_UpdateCredentialsAndState(t *testing.T) {
	ps, _, tenantID := newTestSQLiteBitrixPortalStore(t, bitrixTestEncKey)
	ctx := context.Background()

	if err := ps.Create(ctx, &store.BitrixPortalData{
		TenantID:    tenantID,
		Name:        "u",
		Domain:      "u.bitrix24.com",
		Credentials: []byte(`{"v":1}`),
		State:       []byte(`{"s":1}`),
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := ps.UpdateCredentials(ctx, tenantID, "u", []byte(`{"v":2}`)); err != nil {
		t.Fatalf("UpdateCredentials: %v", err)
	}
	if err := ps.UpdateState(ctx, tenantID, "u", []byte(`{"s":2}`)); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}

	got, err := ps.GetByName(ctx, tenantID, "u")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if string(got.Credentials) != `{"v":2}` {
		t.Fatalf("credentials not updated: %q", got.Credentials)
	}
	if string(got.State) != `{"s":2}` {
		t.Fatalf("state not updated: %q", got.State)
	}
}

func TestSQLiteBitrixPortalStore_ListByTenantAndAll(t *testing.T) {
	ps, db, tenantA := newTestSQLiteBitrixPortalStore(t, bitrixTestEncKey)
	ctx := context.Background()

	// Second tenant for isolation check.
	tenantB := store.GenNewID()
	if _, err := db.Exec(
		`INSERT INTO tenants (id, name, slug, status, settings, created_at, updated_at)
		 VALUES (?, 'tenant-b', 'tenant-b', 'active', '{}', datetime('now'), datetime('now'))`,
		tenantB,
	); err != nil {
		t.Fatalf("insert tenant B: %v", err)
	}

	for _, rec := range []struct {
		tid  uuid.UUID
		name string
	}{
		{tenantA, "alpha"},
		{tenantA, "beta"},
		{tenantB, "gamma"},
	} {
		if err := ps.Create(ctx, &store.BitrixPortalData{
			TenantID:    rec.tid,
			Name:        rec.name,
			Domain:      rec.name + ".bitrix24.com",
			Credentials: []byte(`{}`),
		}); err != nil {
			t.Fatalf("Create %s: %v", rec.name, err)
		}
	}

	listA, err := ps.ListByTenant(ctx, tenantA)
	if err != nil {
		t.Fatalf("ListByTenant A: %v", err)
	}
	if len(listA) != 2 {
		t.Fatalf("expected 2 portals for tenant A, got %d", len(listA))
	}
	// Sorted by name.
	if listA[0].Name != "alpha" || listA[1].Name != "beta" {
		t.Fatalf("unexpected order: %s, %s", listA[0].Name, listA[1].Name)
	}

	// Tenant isolation — B must not see A's rows.
	listB, err := ps.ListByTenant(ctx, tenantB)
	if err != nil {
		t.Fatalf("ListByTenant B: %v", err)
	}
	if len(listB) != 1 || listB[0].Name != "gamma" {
		t.Fatalf("tenant isolation broken; B sees %d rows", len(listB))
	}

	all, err := ps.ListAllForLoader(ctx)
	if err != nil {
		t.Fatalf("ListAllForLoader: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 rows across tenants, got %d", len(all))
	}
}

func TestSQLiteBitrixPortalStore_Delete(t *testing.T) {
	ps, _, tenantID := newTestSQLiteBitrixPortalStore(t, bitrixTestEncKey)
	ctx := context.Background()

	if err := ps.Create(ctx, &store.BitrixPortalData{
		TenantID:    tenantID,
		Name:        "gone",
		Domain:      "gone.bitrix24.com",
		Credentials: []byte(`{}`),
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := ps.Delete(ctx, tenantID, "gone"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := ps.GetByName(ctx, tenantID, "gone")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows after delete, got %v", err)
	}
}

func TestSQLiteBitrixPortalStore_NilGuards(t *testing.T) {
	ps, _, tenantID := newTestSQLiteBitrixPortalStore(t, bitrixTestEncKey)
	ctx := context.Background()

	if err := ps.Create(ctx, nil); err == nil {
		t.Fatal("expected error on nil portal")
	}
	if err := ps.Create(ctx, &store.BitrixPortalData{Name: "x", Domain: "x"}); err == nil {
		t.Fatal("expected error on nil tenant_id")
	}
	if err := ps.Create(ctx, &store.BitrixPortalData{TenantID: tenantID}); err == nil {
		t.Fatal("expected error on empty name/domain")
	}
	if err := ps.UpdateCredentials(ctx, uuid.Nil, "x", []byte("v")); err == nil {
		t.Fatal("expected error on nil tenant_id UpdateCredentials")
	}
	if err := ps.UpdateState(ctx, uuid.Nil, "x", []byte("v")); err == nil {
		t.Fatal("expected error on nil tenant_id UpdateState")
	}
	if err := ps.Delete(ctx, uuid.Nil, "x"); err == nil {
		t.Fatal("expected error on nil tenant_id Delete")
	}
	if _, err := ps.GetByName(ctx, uuid.Nil, "x"); err == nil {
		t.Fatal("expected error on nil tenant_id GetByName")
	}
}
