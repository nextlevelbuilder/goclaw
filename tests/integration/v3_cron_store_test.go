//go:build integration

package integration

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

func newCronStore(t *testing.T) *pg.PGCronStore {
	t.Helper()
	db := testDB(t)
	pg.InitSqlx(db)
	return pg.NewPGCronStore(db)
}

func TestStoreCron_CreateJobAndScan(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := newCronStore(t)

	everyMS := int64(60000) // every 60s
	job, err := s.AddJob(ctx, "test-job", store.CronSchedule{
		Kind:    "every",
		EveryMS: &everyMS,
	}, "hello from cron", false, "", "", agentID.String(), "cron-user")
	if err != nil {
		t.Fatalf("AddJob: %v", err)
	}
	if job == nil {
		t.Fatal("AddJob returned nil job")
	}

	t.Cleanup(func() {
		db.Exec("DELETE FROM cron_run_logs WHERE job_id = $1", uuid.MustParse(job.ID))
		db.Exec("DELETE FROM cron_jobs WHERE id = $1", uuid.MustParse(job.ID))
	})

	if job.Name != "test-job" {
		t.Errorf("Name = %q, want %q", job.Name, "test-job")
	}
	if !job.Enabled {
		t.Error("expected Enabled=true")
	}
	if job.Schedule.Kind != "every" {
		t.Errorf("Schedule.Kind = %q, want %q", job.Schedule.Kind, "every")
	}

	// GetJob
	got, ok := s.GetJob(ctx, job.ID)
	if !ok {
		t.Fatal("GetJob returned false")
	}
	if got.Name != "test-job" {
		t.Errorf("GetJob Name = %q, want %q", got.Name, "test-job")
	}

	// ListJobs
	list := s.ListJobs(ctx, true, "", "")
	found := false
	for _, j := range list {
		if j.ID == job.ID {
			found = true
			break
		}
	}
	if !found {
		t.Error("job not found in ListJobs")
	}
}

func TestStoreCron_RecordRunAndGetLog(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := newCronStore(t)

	everyMS := int64(60000)
	job, err := s.AddJob(ctx, "log-test-job", store.CronSchedule{
		Kind:    "every",
		EveryMS: &everyMS,
	}, "test run log", false, "", "", agentID.String(), "cron-user")
	if err != nil {
		t.Fatalf("AddJob: %v", err)
	}

	jobUUID := uuid.MustParse(job.ID)
	t.Cleanup(func() {
		db.Exec("DELETE FROM cron_run_logs WHERE job_id = $1", jobUUID)
		db.Exec("DELETE FROM cron_jobs WHERE id = $1", jobUUID)
	})

	// Insert run log directly (RecordRun is internal to executeOneJob)
	now := time.Now()
	summary := "completed successfully"
	_, err = db.ExecContext(ctx,
		`INSERT INTO cron_run_logs (id, job_id, agent_id, status, error, summary, duration_ms, input_tokens, output_tokens, ran_at)
		 VALUES ($1, $2, $3, $4, NULL, $5, $6, $7, $8, $9)`,
		uuid.Must(uuid.NewV7()), jobUUID, agentID, "ok", summary, int64(150), 100, 50, now,
	)
	if err != nil {
		t.Fatalf("insert run log: %v", err)
	}

	// GetRunLog — exercises the sqlx scan path
	entries, total := s.GetRunLog(ctx, job.ID, 10, 0)
	if total != 1 {
		t.Fatalf("total = %d, want 1", total)
	}
	if len(entries) != 1 {
		t.Fatalf("entries len = %d, want 1", len(entries))
	}
	if entries[0].Status != "ok" {
		t.Errorf("Status = %q, want %q", entries[0].Status, "ok")
	}
	if entries[0].Summary != "completed successfully" {
		t.Errorf("Summary = %q, want %q", entries[0].Summary, "completed successfully")
	}
	if entries[0].DurationMS != 150 {
		t.Errorf("DurationMS = %d, want 150", entries[0].DurationMS)
	}
	if entries[0].InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", entries[0].InputTokens)
	}
	if entries[0].OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want 50", entries[0].OutputTokens)
	}
}

func TestStoreCron_GetRunLogPagination(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := newCronStore(t)

	everyMS := int64(60000)
	job, err := s.AddJob(ctx, "paginate-job", store.CronSchedule{
		Kind:    "every",
		EveryMS: &everyMS,
	}, "pagination test", false, "", "", agentID.String(), "cron-user")
	if err != nil {
		t.Fatalf("AddJob: %v", err)
	}

	jobUUID := uuid.MustParse(job.ID)
	t.Cleanup(func() {
		db.Exec("DELETE FROM cron_run_logs WHERE job_id = $1", jobUUID)
		db.Exec("DELETE FROM cron_jobs WHERE id = $1", jobUUID)
	})

	// Insert 5 run logs
	base := time.Now()
	for i := 0; i < 5; i++ {
		_, err = db.ExecContext(ctx,
			`INSERT INTO cron_run_logs (id, job_id, agent_id, status, summary, duration_ms, input_tokens, output_tokens, ran_at)
			 VALUES ($1, $2, $3, 'ok', $4, 100, 10, 5, $5)`,
			uuid.Must(uuid.NewV7()), jobUUID, agentID,
			"run "+string(rune('A'+i)),
			base.Add(time.Duration(i)*time.Minute),
		)
		if err != nil {
			t.Fatalf("insert run log %d: %v", i, err)
		}
	}

	// Page 1: limit=2, offset=0
	entries, total := s.GetRunLog(ctx, job.ID, 2, 0)
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if len(entries) != 2 {
		t.Errorf("page 1 len = %d, want 2", len(entries))
	}

	// Page 2: limit=2, offset=2
	entries2, total2 := s.GetRunLog(ctx, job.ID, 2, 2)
	if total2 != 5 {
		t.Errorf("total2 = %d, want 5", total2)
	}
	if len(entries2) != 2 {
		t.Errorf("page 2 len = %d, want 2", len(entries2))
	}

	// Page 3: limit=2, offset=4
	entries3, _ := s.GetRunLog(ctx, job.ID, 2, 4)
	if len(entries3) != 1 {
		t.Errorf("page 3 len = %d, want 1", len(entries3))
	}
}

func TestStoreCron_TenantIsolation(t *testing.T) {
	db := testDB(t)
	tenantA, agentA := seedTenantAgent(t, db)
	tenantB, _ := seedTenantAgent(t, db)
	ctxA := tenantCtx(tenantA)
	ctxB := tenantCtx(tenantB)
	s := newCronStore(t)

	everyMS := int64(60000)
	job, err := s.AddJob(ctxA, "tenant-a-job", store.CronSchedule{
		Kind:    "every",
		EveryMS: &everyMS,
	}, "tenant A msg", false, "", "", agentA.String(), "cron-user-a")
	if err != nil {
		t.Fatalf("AddJob: %v", err)
	}

	jobUUID := uuid.MustParse(job.ID)
	t.Cleanup(func() {
		db.Exec("DELETE FROM cron_run_logs WHERE job_id = $1", jobUUID)
		db.Exec("DELETE FROM cron_jobs WHERE id = $1", jobUUID)
	})

	// Insert a run log for tenant A's job
	_, err = db.ExecContext(ctxA,
		`INSERT INTO cron_run_logs (id, job_id, agent_id, status, summary, duration_ms, input_tokens, output_tokens, ran_at)
		 VALUES ($1, $2, $3, 'ok', 'tenant A run', 100, 10, 5, $4)`,
		uuid.Must(uuid.NewV7()), jobUUID, agentA, time.Now(),
	)
	if err != nil {
		t.Fatalf("insert run log: %v", err)
	}

	// Tenant B should NOT see tenant A's job
	_, ok := s.GetJob(ctxB, job.ID)
	if ok {
		t.Error("tenant B can see tenant A's job — isolation broken")
	}

	// Tenant B should NOT see tenant A's run logs
	entries, total := s.GetRunLog(ctxB, job.ID, 10, 0)
	if total != 0 || len(entries) != 0 {
		t.Errorf("tenant B sees run logs (total=%d, entries=%d) — isolation broken", total, len(entries))
	}

	// Tenant A should see its own
	entriesA, totalA := s.GetRunLog(ctxA, job.ID, 10, 0)
	if totalA != 1 {
		t.Errorf("tenant A total = %d, want 1", totalA)
	}
	if len(entriesA) != 1 {
		t.Errorf("tenant A entries = %d, want 1", len(entriesA))
	}
}
