package pg

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// AddJob must return a usable job or an error — never (nil, nil), and never a row
// it cannot read back.
//
// This is the store-level regression test for the bug the workflow compiler hit on
// staging: insert fell back to the master tenant while the read-back filtered on
// the raw context, so a tenant-less caller created an orphaned schedule and got a
// nil-panic for its trouble.
func TestAddJobIsReadableInTheScopeItWasInserted(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	db := testDB(t) // skips unless the migrated schema is present
	s := NewPGCronStore(db)

	tenant := newTenant(t, db)
	var agentID uuid.UUID
	if err := db.QueryRow(
		`INSERT INTO agents (tenant_id, agent_key, owner_id, model)
		 VALUES ($1, 'cron-test-' || substr(gen_random_uuid()::text,1,8), 'cron-test', 'default')
		 RETURNING id`, tenant).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM agents WHERE id = $1`, agentID) })

	sched := store.CronSchedule{Kind: "cron", Expr: "0 8 * * 1", TZ: "UTC"}

	// The case that used to break: NO tenant in context, exactly as ReconcileAll
	// and any other system-initiated caller passes it.
	bare := context.Background()
	job, err := s.AddJob(bare, "cron-test-bare", sched, "hello", false, "", "", agentID.String(), "")
	if err != nil {
		t.Fatalf("AddJob with no tenant in context: %v", err)
	}
	if job == nil {
		t.Fatal("AddJob returned (nil, nil) — every caller dereferences after checking err")
	}
	t.Cleanup(func() { _ = s.RemoveJob(store.WithTenantID(bare, job.TenantID), job.ID) })
	if job.Schedule.Expr != "0 8 * * 1" || job.AgentID != agentID.String() {
		t.Errorf("job does not reflect what was asked for: %+v", job)
	}
	if job.State.NextRunAtMS == nil {
		t.Error("no next run computed — the scheduler would never fire it")
	}

	// And with a tenant, the row must belong to THAT tenant, not the fallback.
	scoped := store.WithTenantID(context.Background(), tenant)
	job2, err := s.AddJob(scoped, "cron-test-scoped", sched, "hello", false, "", "", agentID.String(), "")
	if err != nil || job2 == nil {
		t.Fatalf("AddJob with a tenant: job=%v err=%v", job2, err)
	}
	t.Cleanup(func() { _ = s.RemoveJob(scoped, job2.ID) })
	if job2.TenantID != tenant {
		t.Errorf("job tenant = %s, want %s — a workflow's job must belong to its own tenant", job2.TenantID, tenant)
	}
	if _, ok := s.GetJob(scoped, job2.ID); !ok {
		t.Error("a job inserted in a tenant scope is not readable in that scope")
	}
}
