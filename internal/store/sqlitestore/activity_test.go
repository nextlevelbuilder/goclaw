//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// seedActivityTenant inserts a tenant row so activity_logs FK (tenant_id → tenants.id)
// holds — PRAGMA foreign_keys is ON for every SQLite connection.
func seedActivityTenant(t *testing.T, db *sql.DB, tenantID uuid.UUID) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO tenants (id, name, slug, status) VALUES (?, 'T', ?, 'active')`,
		tenantID.String(), "t-"+tenantID.String()[:8])
	if err != nil {
		t.Fatalf("seedActivityTenant: %v", err)
	}
}

// insertActivityRowAt inserts an activity_logs row with an explicit created_at
// (Log always stamps now, so retention tests need direct inserts to back-date rows).
func insertActivityRowAt(t *testing.T, db *sql.DB, tenantID uuid.UUID, createdAt time.Time) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO activity_logs (id, actor_type, actor_id, action, entity_type, entity_id, details, ip_address, tenant_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), "user", "tester", "prune.test", "agent", "agent-1", nil, "127.0.0.1",
		tenantID.String(), createdAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		t.Fatalf("insertActivityRowAt: %v", err)
	}
}

func countActivityRows(t *testing.T, st *SQLiteActivityStore) int {
	t.Helper()
	var n int
	err := st.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM activity_logs`).Scan(&n)
	if err != nil {
		t.Fatalf("count activity_logs: %v", err)
	}
	return n
}

func TestSQLiteActivityStorePrune_DeletesOnlyOldRows(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	st := NewSQLiteActivityStore(db)
	tenantID := uuid.Must(uuid.NewV7())
	seedActivityTenant(t, db, tenantID)

	now := time.Now()
	oldRow := now.Add(-48 * time.Hour)
	newRow := now.Add(-1 * time.Hour)

	insertActivityRowAt(t, db, tenantID, oldRow)
	insertActivityRowAt(t, db, tenantID, newRow)
	if got := countActivityRows(t, st); got != 2 {
		t.Fatalf("count before prune = %d, want 2", got)
	}

	cutoff := now.Add(-24 * time.Hour)
	deleted, err := st.Prune(context.Background(), cutoff)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}
	if got := countActivityRows(t, st); got != 1 {
		t.Fatalf("count after prune = %d, want 1 (new row must survive)", got)
	}
}

func TestSQLiteActivityStorePrune_NoOldRows_DeletesNothing(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	st := NewSQLiteActivityStore(db)
	tenantID := uuid.Must(uuid.NewV7())
	seedActivityTenant(t, db, tenantID)

	// All rows are new (within the last hour).
	insertActivityRowAt(t, db, tenantID, time.Now().Add(-30*time.Minute))
	insertActivityRowAt(t, db, tenantID, time.Now().Add(-10*time.Minute))

	deleted, err := st.Prune(context.Background(), time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if deleted != 0 {
		t.Errorf("deleted = %d, want 0", deleted)
	}
	if got := countActivityRows(t, st); got != 2 {
		t.Fatalf("count after prune = %d, want 2", got)
	}
}

// TestSQLiteActivityStoreLog_PersistsTenantID verifies the SQLite persistence
// path: rows written with a tenant context land with that tenant; without one
// they fall back to master.
func TestSQLiteActivityStoreLog_PersistsTenantID(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	st := NewSQLiteActivityStore(db)

	tenantID := uuid.Must(uuid.NewV7())
	seedActivityTenant(t, db, tenantID)
	ctx := store.WithTenantID(context.Background(), tenantID)
	err := st.Log(ctx, &store.ActivityLog{
		ActorType: "user", ActorID: "user-x", Action: "auth.login",
		EntityType: "auth", EntityID: "bearer",
	})
	if err != nil {
		t.Fatalf("Log: %v", err)
	}

	rows, err := st.List(ctx, store.ActivityListOpts{Action: "auth.login"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("List len = %d, want 1", len(rows))
	}
	if rows[0].ActorID != "user-x" {
		t.Errorf("ActorID = %q, want user-x", rows[0].ActorID)
	}
	if rows[0].CreatedAt.IsZero() {
		t.Error("CreatedAt is zero, want DB default now")
	}

	// Tenant scoping on read: opts with the same tenant ctx returns the row;
	// a different tenant sees nothing.
	otherCtx := store.WithTenantID(context.Background(), uuid.Must(uuid.NewV7()))
	other, err := st.List(otherCtx, store.ActivityListOpts{Action: "auth.login"})
	if err != nil {
		t.Fatalf("List other tenant: %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("List other tenant len = %d, want 0 (tenant isolation)", len(other))
	}
}