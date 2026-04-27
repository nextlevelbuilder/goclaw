//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestListRunningAcrossTenants verifies the master-scope query that goclaw
// boot uses to find subagents left in `running` state by the previous
// process. The query MUST:
//
//   - return rows from every tenant (no tenant filter applied)
//   - exclude rows in any non-running status
//   - exclude archived rows (a stale `running` row that was already
//     archive-swept doesn't need re-recovery)
//   - respect the limit parameter
//   - default the limit when caller passes <=0
func TestListRunningAcrossTenants(t *testing.T) {
	store := newSubagentStoreForTest(t)
	ctx := context.Background()

	tenantA := seedTenant(t, store.db, "subrec-a")
	tenantB := seedTenant(t, store.db, "subrec-b")

	// Three running rows across two tenants. Stagger created_at so we
	// can assert ordering (older first).
	now := time.Now().UTC()
	insertRunningTask(t, store, tenantA, "alpha", now.Add(-3*time.Hour))
	insertRunningTask(t, store, tenantB, "beta", now.Add(-2*time.Hour))
	insertRunningTask(t, store, tenantA, "gamma", now.Add(-1*time.Hour))

	// Decoy rows that should NOT be returned: a completed row, an
	// archived running row.
	insertCompletedTask(t, store, tenantA, "completed-decoy")
	insertArchivedRunningTask(t, store, tenantB, "archived-running-decoy")

	got, err := store.ListRunningAcrossTenants(ctx, 100)
	if err != nil {
		t.Fatalf("ListRunningAcrossTenants: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 running rows, got %d (subjects: %v)", len(got), subjectsOf(got))
	}
	if got[0].Subject != "alpha" || got[1].Subject != "beta" || got[2].Subject != "gamma" {
		t.Errorf("expected ordering alpha → beta → gamma (oldest first), got %v", subjectsOf(got))
	}

	// Limit is respected.
	limited, err := store.ListRunningAcrossTenants(ctx, 2)
	if err != nil {
		t.Fatalf("limited query: %v", err)
	}
	if len(limited) != 2 {
		t.Errorf("limit=2 returned %d rows", len(limited))
	}

	// limit<=0 defaults (we don't care exactly to what, but it should
	// be enough to return all 3).
	all, err := store.ListRunningAcrossTenants(ctx, 0)
	if err != nil {
		t.Fatalf("default-limit query: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("default limit dropped rows: got %d, want 3", len(all))
	}
}

// --- helpers ---------------------------------------------------------

func newSubagentStoreForTest(t *testing.T) *SQLiteSubagentTaskStore {
	t.Helper()
	db, err := OpenDB(filepath.Join(t.TempDir(), "subagents.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	return NewSQLiteSubagentTaskStore(db)
}


// insertRunningTask uses the store's Create plus a direct `created_at`
// update so we can backdate the row deterministically. Create() always
// stamps NOW; the update lets us sequence rows in tests.
func insertRunningTask(t *testing.T, s *SQLiteSubagentTaskStore, tenantID uuid.UUID, subject string, createdAt time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	task := &store.SubagentTaskData{
		BaseModel:      store.BaseModel{ID: id},
		TenantID:       tenantID,
		ParentAgentKey: "test-parent",
		Subject:        subject,
		Status:         "running",
	}
	ctx := store.WithTenantID(context.Background(), tenantID)
	if err := s.Create(ctx, task); err != nil {
		t.Fatalf("Create(%s): %v", subject, err)
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE subagent_tasks SET created_at = ? WHERE id = ?`,
		createdAt.Format(time.RFC3339Nano), id,
	); err != nil {
		t.Fatalf("backdate created_at(%s): %v", subject, err)
	}
	return id
}

func insertCompletedTask(t *testing.T, s *SQLiteSubagentTaskStore, tenantID uuid.UUID, subject string) {
	t.Helper()
	id := insertRunningTask(t, s, tenantID, subject, time.Now().UTC())
	ctx := store.WithTenantID(context.Background(), tenantID)
	if err := s.UpdateStatus(ctx, id, "completed", nil, 1, 0, 0); err != nil {
		t.Fatalf("UpdateStatus(%s): %v", subject, err)
	}
}

func insertArchivedRunningTask(t *testing.T, s *SQLiteSubagentTaskStore, tenantID uuid.UUID, subject string) {
	t.Helper()
	id := insertRunningTask(t, s, tenantID, subject, time.Now().UTC())
	ctx := store.WithTenantID(context.Background(), tenantID)
	// Direct update — public API never archives a `running` row, but
	// stale rows in the DB can wind up archived by an out-of-band
	// cleanup. The query must still skip them.
	if _, err := s.db.ExecContext(ctx,
		`UPDATE subagent_tasks SET archived_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339Nano), id,
	); err != nil {
		t.Fatalf("archive(%s): %v", subject, err)
	}
}

func subjectsOf(tasks []store.SubagentTaskData) []string {
	out := make([]string, len(tasks))
	for i, t := range tasks {
		out[i] = t.Subject
	}
	return out
}
