//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// testEncKey is a 32-byte AES key used for SQLite secure-cli tests.
// Its actual value is irrelevant for IsRegisteredBinary (metadata-only query).
const testEncKey = "test-key-32-bytes-aaaaaaaaaaaaaaa"

func newTestSQLiteSecureCLI(t *testing.T) (*SQLiteSecureCLIStore, *sql.DB) {
	t.Helper()
	db, err := OpenDB(filepath.Join(t.TempDir(), "secure_cli.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	return NewSQLiteSecureCLIStore(db, testEncKey), db
}

// seedTenant inserts a tenant row and returns its ID.
func seedTenant(t *testing.T, db *sql.DB, slug string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.Exec(
		`INSERT INTO tenants (id, name, slug, status) VALUES (?, ?, ?, 'active')`,
		id, "Tenant-"+slug, slug,
	)
	if err != nil {
		t.Fatalf("seed tenant %s: %v", slug, err)
	}
	return id
}

// seedBinary inserts a secure_cli_binaries row with the given fields.
func seedBinary(t *testing.T, db *sql.DB, tenantID uuid.UUID, name string, enabled, isGlobal bool) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO secure_cli_binaries
		  (id, binary_name, encrypted_env, is_global, enabled, tenant_id)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		uuid.New(), name, []byte("{}"), isGlobal, enabled, tenantID,
	)
	if err != nil {
		t.Fatalf("seed binary %s: %v", name, err)
	}
}

func TestSQLite_IsRegisteredBinary_ReturnsTrueForEnabledNonGlobal(t *testing.T) {
	s, db := newTestSQLiteSecureCLI(t)
	tid := seedTenant(t, db, "t-true")
	seedBinary(t, db, tid, "gh", true, false)

	ctx := store.WithTenantID(context.Background(), tid)
	got, err := s.IsRegisteredBinary(ctx, "gh")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !got {
		t.Fatalf("expected true for enabled non-global binary")
	}
}

// Red Team F2 regression guard: is_global=true must NOT be reported as
// gate-needing — those binaries are open to all agents without a grant.
func TestSQLite_IsRegisteredBinary_FalseForGlobalBinary(t *testing.T) {
	s, db := newTestSQLiteSecureCLI(t)
	tid := seedTenant(t, db, "t-global")
	seedBinary(t, db, tid, "ls", true, true)

	ctx := store.WithTenantID(context.Background(), tid)
	got, err := s.IsRegisteredBinary(ctx, "ls")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got {
		t.Fatalf("expected false for is_global=true binary (would deny access otherwise)")
	}
}

func TestSQLite_IsRegisteredBinary_FalseForDisabled(t *testing.T) {
	s, db := newTestSQLiteSecureCLI(t)
	tid := seedTenant(t, db, "t-disabled")
	seedBinary(t, db, tid, "gh", false, false)

	ctx := store.WithTenantID(context.Background(), tid)
	got, err := s.IsRegisteredBinary(ctx, "gh")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got {
		t.Fatalf("expected false for disabled binary")
	}
}

func TestSQLite_IsRegisteredBinary_FalseForWrongTenant(t *testing.T) {
	s, db := newTestSQLiteSecureCLI(t)
	tidA := seedTenant(t, db, "t-a")
	tidB := seedTenant(t, db, "t-b")
	seedBinary(t, db, tidA, "gh", true, false)

	ctx := store.WithTenantID(context.Background(), tidB)
	got, err := s.IsRegisteredBinary(ctx, "gh")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got {
		t.Fatalf("expected false across tenants")
	}
}

func TestSQLite_IsRegisteredBinary_FalseForUnknownName(t *testing.T) {
	s, db := newTestSQLiteSecureCLI(t)
	tid := seedTenant(t, db, "t-unk")
	ctx := store.WithTenantID(context.Background(), tid)
	got, err := s.IsRegisteredBinary(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got {
		t.Fatalf("expected false for unknown name")
	}
}

func TestSQLite_IsRegisteredBinary_RespectsCrossTenant(t *testing.T) {
	s, db := newTestSQLiteSecureCLI(t)
	tid := seedTenant(t, db, "t-xtenant")
	seedBinary(t, db, tid, "gh", true, false)

	ctx := store.WithCrossTenant(context.Background())
	got, err := s.IsRegisteredBinary(ctx, "gh")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !got {
		t.Fatalf("expected true under cross-tenant ctx")
	}
}

func TestSQLite_IsRegisteredBinary_EmptyNameReturnsFalse(t *testing.T) {
	s, _ := newTestSQLiteSecureCLI(t)
	ctx := store.WithCrossTenant(context.Background())
	got, err := s.IsRegisteredBinary(ctx, "")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got {
		t.Fatalf("expected false for empty name")
	}
}

func TestSQLite_IsRegisteredBinary_NilTenantNotCrossTenant(t *testing.T) {
	s, _ := newTestSQLiteSecureCLI(t)
	got, err := s.IsRegisteredBinary(context.Background(), "gh")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got {
		t.Fatalf("expected false when tenant unset and not cross-tenant")
	}
}

// Red Team F8: case-insensitive match — macOS/APFS resolves GH → gh.
func TestSQLite_IsRegisteredBinary_CaseInsensitive(t *testing.T) {
	s, db := newTestSQLiteSecureCLI(t)
	tid := seedTenant(t, db, "t-case")
	seedBinary(t, db, tid, "gh", true, false)

	ctx := store.WithTenantID(context.Background(), tid)
	for _, q := range []string{"gh", "GH", "Gh", "  gh  "} {
		got, err := s.IsRegisteredBinary(ctx, q)
		if err != nil {
			t.Fatalf("unexpected err for %q: %v", q, err)
		}
		if !got {
			t.Fatalf("expected true for %q (case-insensitive)", q)
		}
	}
}
