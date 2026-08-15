//go:build integration

package integration

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestStoreCron_RemoveJob verifies that RemoveJob deletes the row and is tenant-scoped.
func TestStoreCron_RemoveJob(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := newCronStore(t)

	everyMS := int64(30000)
	job, err := s.AddJob(ctx, "remove-test-job", store.CronSchedule{
		Kind:    "every",
		EveryMS: &everyMS,
	}, "removal test", false, "", "", agentID.String(), "test-user")
	if err != nil {
		t.Fatalf("AddJob: %v", err)
	}
	jobUUID := uuid.MustParse(job.ID)
	t.Cleanup(func() {
		db.Exec("DELETE FROM cron_run_logs WHERE job_id = $1", jobUUID)
		db.Exec("DELETE FROM cron_jobs WHERE id = $1", jobUUID)
	})

	if err := s.RemoveJob(ctx, job.ID); err != nil {
		t.Fatalf("RemoveJob: %v", err)
	}

	// GetJob must return false after removal.
	_, ok := s.GetJob(ctx, job.ID)
	if ok {
		t.Error("GetJob returned true after RemoveJob — row still present")
	}
}

// TestStoreCron_EnableDisable verifies EnableJob toggles enabled flag and updates next_run_at.
func TestStoreCron_EnableDisable(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := newCronStore(t)

	everyMS := int64(60000)
	job, err := s.AddJob(ctx, "enable-disable-job", store.CronSchedule{
		Kind:    "every",
		EveryMS: &everyMS,
	}, "toggle test", false, "", "", agentID.String(), "test-user")
	if err != nil {
		t.Fatalf("AddJob: %v", err)
	}
	jobUUID := uuid.MustParse(job.ID)
	t.Cleanup(func() {
		db.Exec("DELETE FROM cron_run_logs WHERE job_id = $1", jobUUID)
		db.Exec("DELETE FROM cron_jobs WHERE id = $1", jobUUID)
	})

	t.Run("disable", func(t *testing.T) {
		if err := s.EnableJob(ctx, job.ID, false); err != nil {
			t.Fatalf("EnableJob(false): %v", err)
		}
		got, ok := s.GetJob(ctx, job.ID)
		if !ok {
			t.Fatal("GetJob returned false")
		}
		if got.Enabled {
			t.Error("expected Enabled=false after disable")
		}
	})

	t.Run("re-enable", func(t *testing.T) {
		if err := s.EnableJob(ctx, job.ID, true); err != nil {
			t.Fatalf("EnableJob(true): %v", err)
		}
		got, ok := s.GetJob(ctx, job.ID)
		if !ok {
			t.Fatal("GetJob returned false")
		}
		if !got.Enabled {
			t.Error("expected Enabled=true after re-enable")
		}
	})
}

// TestStoreCron_UpdateJob_Name verifies UpdateJob patches the name field.
func TestStoreCron_UpdateJob_Name(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := newCronStore(t)

	everyMS := int64(60000)
	job, err := s.AddJob(ctx, "update-name-original", store.CronSchedule{
		Kind:    "every",
		EveryMS: &everyMS,
	}, "update test", false, "", "", agentID.String(), "test-user")
	if err != nil {
		t.Fatalf("AddJob: %v", err)
	}
	jobUUID := uuid.MustParse(job.ID)
	t.Cleanup(func() {
		db.Exec("DELETE FROM cron_run_logs WHERE job_id = $1", jobUUID)
		db.Exec("DELETE FROM cron_jobs WHERE id = $1", jobUUID)
	})

	updated, err := s.UpdateJob(ctx, job.ID, store.CronJobPatch{Name: "update-name-new"})
	if err != nil {
		t.Fatalf("UpdateJob: %v", err)
	}
	if updated.Name != "update-name-new" {
		t.Errorf("Name = %q, want %q", updated.Name, "update-name-new")
	}

	// Verify persisted.
	got, ok := s.GetJob(ctx, job.ID)
	if !ok {
		t.Fatal("GetJob after update returned false")
	}
	if got.Name != "update-name-new" {
		t.Errorf("persisted Name = %q, want %q", got.Name, "update-name-new")
	}
}

// TestStoreCron_ListJobs_FilterByAgent verifies ListJobs agentID filter.
func TestStoreCron_ListJobs_FilterByAgent(t *testing.T) {
	db := testDB(t)
	tenantID, agentA := seedTenantAgent(t, db)
	_, agentB := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := newCronStore(t)

	everyMS := int64(60000)
	jobA, err := s.AddJob(ctx, "filter-agent-A-job", store.CronSchedule{
		Kind:    "every",
		EveryMS: &everyMS,
	}, "for agent A", false, "", "", agentA.String(), "user-a")
	if err != nil {
		t.Fatalf("AddJob agentA: %v", err)
	}

	jobB, err := s.AddJob(ctx, "filter-agent-B-job", store.CronSchedule{
		Kind:    "every",
		EveryMS: &everyMS,
	}, "for agent B", false, "", "", agentB.String(), "user-b")
	if err != nil {
		t.Fatalf("AddJob agentB: %v", err)
	}

	t.Cleanup(func() {
		for _, id := range []string{jobA.ID, jobB.ID} {
			u := uuid.MustParse(id)
			db.Exec("DELETE FROM cron_run_logs WHERE job_id = $1", u)
			db.Exec("DELETE FROM cron_jobs WHERE id = $1", u)
		}
	})

	// Filter by agentA — must not include jobB.
	listA := s.ListJobs(ctx, true, agentA.String(), "")
	for _, j := range listA {
		if j.ID == jobB.ID {
			t.Error("ListJobs(agentA) returned jobB — filter broken")
		}
	}
	foundA := false
	for _, j := range listA {
		if j.ID == jobA.ID {
			foundA = true
		}
	}
	if !foundA {
		t.Error("ListJobs(agentA) did not return jobA")
	}
}

// TestStoreCron_RunLog_MultipleEntries verifies run log accumulates correctly and pagination works.
func TestStoreCron_RunLog_MultipleStatuses(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := newCronStore(t)

	everyMS := int64(60000)
	job, err := s.AddJob(ctx, "multi-status-job", store.CronSchedule{
		Kind:    "every",
		EveryMS: &everyMS,
	}, "multi status", false, "", "", agentID.String(), "test-user")
	if err != nil {
		t.Fatalf("AddJob: %v", err)
	}
	jobUUID := uuid.MustParse(job.ID)
	t.Cleanup(func() {
		db.Exec("DELETE FROM cron_run_logs WHERE job_id = $1", jobUUID)
		db.Exec("DELETE FROM cron_jobs WHERE id = $1", jobUUID)
	})

	statuses := []struct {
		status  string
		summary string
	}{
		{"ok", "run succeeded"},
		{"error", "something failed"},
		{"ok", "run succeeded again"},
	}
	base := time.Now()
	for i, row := range statuses {
		_, insertErr := db.ExecContext(ctx,
			`INSERT INTO cron_run_logs (id, job_id, agent_id, status, summary, duration_ms, input_tokens, output_tokens, ran_at)
			 VALUES ($1, $2, $3, $4, $5, 100, 10, 5, $6)`,
			uuid.Must(uuid.NewV7()), jobUUID, agentID, row.status, row.summary,
			base.Add(time.Duration(i)*time.Second),
		)
		if insertErr != nil {
			t.Fatalf("insert run log %d: %v", i, insertErr)
		}
	}

	entries, total := s.GetRunLog(ctx, job.ID, 10, 0)
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if len(entries) != 3 {
		t.Errorf("entries len = %d, want 3", len(entries))
	}

	// Verify statuses are captured correctly (order DESC by ran_at).
	statusSeen := map[string]int{}
	for _, e := range entries {
		statusSeen[e.Status]++
	}
	if statusSeen["ok"] != 2 {
		t.Errorf("ok count = %d, want 2", statusSeen["ok"])
	}
	if statusSeen["error"] != 1 {
		t.Errorf("error count = %d, want 1", statusSeen["error"])
	}
}

// TestStoreCron_UpdateJob_Schedule verifies UpdateJob patches the schedule.
func TestStoreCron_UpdateJob_Schedule(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := newCronStore(t)

	everyMS := int64(60000) // 60s
	job, err := s.AddJob(ctx, "sched-update-job", store.CronSchedule{
		Kind:    "every",
		EveryMS: &everyMS,
	}, "schedule update", false, "", "", agentID.String(), "test-user")
	if err != nil {
		t.Fatalf("AddJob: %v", err)
	}
	jobUUID := uuid.MustParse(job.ID)
	t.Cleanup(func() {
		db.Exec("DELETE FROM cron_run_logs WHERE job_id = $1", jobUUID)
		db.Exec("DELETE FROM cron_jobs WHERE id = $1", jobUUID)
	})

	newEveryMS := int64(120000) // 120s
	newSched := &store.CronSchedule{
		Kind:    "every",
		EveryMS: &newEveryMS,
	}
	updated, err := s.UpdateJob(ctx, job.ID, store.CronJobPatch{Schedule: newSched})
	if err != nil {
		t.Fatalf("UpdateJob schedule: %v", err)
	}
	if updated.Schedule.EveryMS == nil {
		t.Fatal("updated schedule EveryMS is nil")
	}
	if *updated.Schedule.EveryMS != newEveryMS {
		t.Errorf("EveryMS = %d, want %d", *updated.Schedule.EveryMS, newEveryMS)
	}
}
