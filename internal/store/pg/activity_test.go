package pg

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// insertPGRowAt inserts an activity_logs row with an explicit created_at and
// tenant (Log always stamps now, so retention tests need direct inserts to
// back-date rows). tenantID must reference an existing tenants row.
func insertPGRowAt(t *testing.T, st *PGActivityStore, tenantID uuid.UUID, createdAt time.Time) {
	t.Helper()
	_, err := st.db.ExecContext(context.Background(),
		`INSERT INTO activity_logs (actor_type, actor_id, action, entity_type, entity_id, details, ip_address, tenant_id, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		"user", "tester", "prune.test", "agent", "agent-1", nil, "127.0.0.1", tenantID, createdAt,
	)
	if err != nil {
		t.Fatalf("insertPGRowAt: %v", err)
	}
}

func countPGRows(t *testing.T, st *PGActivityStore) int {
	t.Helper()
	var n int
	err := st.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM activity_logs`).Scan(&n)
	if err != nil {
		t.Fatalf("count activity_logs: %v", err)
	}
	return n
}

func TestPGActivityStorePrune_DeletesOnlyOldRows(t *testing.T) {
	db := hooksTestDB(t)
	st := NewPGActivityStore(db)
	tenantID, _ := seedTenantAndAgent(t, db)

	now := time.Now()
	insertPGRowAt(t, st, tenantID, now.Add(-48*time.Hour))
	insertPGRowAt(t, st, tenantID, now.Add(-1*time.Hour))
	if got := countPGRows(t, st); got < 2 {
		t.Fatalf("count before prune = %d, want at least 2", got)
	}

	cutoff := now.Add(-24 * time.Hour)
	deleted, err := st.Prune(context.Background(), cutoff)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if deleted == 0 {
		t.Error("deleted = 0, want at least the back-dated old row removed")
	}
}

// TestPGActivityStoreLogAndList verifies the PG store persists Log entries.
func TestPGActivityStoreLogAndList(t *testing.T) {
	db := hooksTestDB(t)
	st := NewPGActivityStore(db)
	tenantID, _ := seedTenantAndAgent(t, db)

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
}