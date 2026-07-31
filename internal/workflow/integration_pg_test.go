//go:build integration

package workflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// End-to-end against REAL Postgres stores: arm a workflow, assert a cron job
// exists; disarm, assert it is gone.
//
// The unit tests use a fake cron store, which proves the compiler's logic but
// cannot catch the wiring mistake that actually matters — a compiler writing to a
// different store than the scheduler reads, or a schedule shape cron rejects at
// insert time. Only real stores show that.
//
// Run: TEST_DATABASE_URL=... go test -tags integration ./internal/workflow/
func TestArmCreatesRealCronJob(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Skipf("no database: %v", err)
	}
	// The cron store scopes inserts AND reads by the tenant in context, so the
	// test must run in one — without it AddJob inserts a row it then cannot read
	// back, which is how the (nil, nil) contract violation surfaced.
	ctx := context.Background()

	var tenant uuid.UUID
	if err := db.QueryRow(
		`INSERT INTO tenants (id, name, slug)
		 VALUES (gen_random_uuid(), 'wf-test', 'wf-test-' || substr(gen_random_uuid()::text,1,8))
		 RETURNING id`).Scan(&tenant); err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	ctx = store.WithTenantID(ctx, tenant)

	// A real agent row: cron_jobs.agent_id has an FK, so the compiler's UUID is
	// not enough — the agent must exist. Worth having in the test because that FK
	// is what makes a workflow naming a deleted agent fail closed rather than
	// scheduling a run that cannot resolve one.
	var agentID uuid.UUID
	if err := db.QueryRow(
		`INSERT INTO agents (tenant_id, agent_key, owner_id, model)
		 VALUES ($1, 'wf-researcher-' || substr(gen_random_uuid()::text,1,8), 'wf-test-owner', 'default')
		 RETURNING id`, tenant).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM agents WHERE id = $1`, agentID) })

	wfStore := pg.NewPGWorkflowStore(db)
	cronStore := pg.NewPGCronStore(db)
	c := NewCompiler(wfStore, cronStore)
	if !c.Available() {
		t.Fatal("compiler reported itself unavailable with real stores")
	}

	w := &store.Workflow{
		TenantID: tenant,
		Name:     "Integration digest",
		Graph: json.RawMessage(`{
		  "nodes":[
		    {"id":"t1","type":"trigger","data":{"kind":"cron","expr":"0 8 * * 1","tz":"UTC"}},
		    {"id":"a1","type":"agent","data":{"agentId":"` + agentID.String() + `","prompt":"summarise competitors"}}
		  ],
		  "edges":[{"id":"e1","source":"t1","target":"a1"}]}`),
	}
	if err := wfStore.Create(ctx, w); err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	t.Cleanup(func() { _ = wfStore.Delete(ctx, tenant, w.ID) })

	// ARM
	w.Enabled = true
	if err := c.Apply(ctx, w); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if w.CompileError != nil {
		t.Fatalf("compile failed against real stores: %s", *w.CompileError)
	}

	var rec Compiled
	if err := json.Unmarshal(w.Compiled, &rec); err != nil {
		t.Fatalf("compile record: %v", err)
	}
	if len(rec.CronJobIDs) != 1 {
		t.Fatalf("recorded %d job ids, want 1", len(rec.CronJobIDs))
	}

	// The job must be REAL — readable back out of the cron store the scheduler
	// uses, with the schedule and prompt from the graph.
	job, found := cronStore.GetJob(ctx, rec.CronJobIDs[0])
	if !found {
		t.Fatalf("cron job %s was recorded but does not exist", rec.CronJobIDs[0])
	}
	t.Cleanup(func() { _ = cronStore.RemoveJob(ctx, job.ID) })

	if job.Schedule.Expr != "0 8 * * 1" {
		t.Errorf("schedule = %q, want the graph's expression", job.Schedule.Expr)
	}
	if job.AgentID != agentID.String() {
		t.Errorf("agent = %q, want %s", job.AgentID, agentID)
	}
	if job.Payload.Message != "summarise competitors" {
		t.Errorf("message = %q, want the graph's prompt", job.Payload.Message)
	}
	if !job.Enabled {
		t.Error("an armed workflow produced a disabled cron job — it would never fire")
	}
	// The scheduler only runs jobs it considers due; a job with no next run is
	// inert no matter how correct it looks.
	if job.State.NextRunAtMS == nil {
		t.Error("cron job has no next run time — it would never fire")
	}

	// The compile record must round-trip through the DB, not just live in memory:
	// the reconciler reads it from there after a restart.
	reloaded, err := wfStore.Get(ctx, tenant, w.ID)
	if err != nil || reloaded == nil {
		t.Fatalf("reload: %v", err)
	}
	var persisted Compiled
	if err := json.Unmarshal(reloaded.Compiled, &persisted); err != nil {
		t.Fatalf("persisted compile record: %v", err)
	}
	if len(persisted.CronJobIDs) != 1 || persisted.CronJobIDs[0] != job.ID {
		t.Errorf("persisted ids %v do not name the created job %s", persisted.CronJobIDs, job.ID)
	}

	// DISARM — the job must actually disappear.
	reloaded.Enabled = false
	if err := c.Apply(ctx, reloaded); err != nil {
		t.Fatalf("disarm: %v", err)
	}
	if _, stillThere := cronStore.GetJob(ctx, job.ID); stillThere {
		t.Error("the cron job survived disarming — it would keep firing")
	}
}
