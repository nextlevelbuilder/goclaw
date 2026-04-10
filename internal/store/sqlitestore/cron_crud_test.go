//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestSQLiteCronStore_ReenableRestoresNextRun(t *testing.T) {
	cronStore, ctx, _ := newTestSQLiteCronStore(t)
	everyMS := int64(time.Minute / time.Millisecond)

	job, err := cronStore.AddJob(ctx, "job-reenable", store.CronSchedule{
		Kind:    "every",
		EveryMS: &everyMS,
	}, "hello", false, "", "", "", "user-1")
	if err != nil {
		t.Fatalf("AddJob error: %v", err)
	}
	if job == nil {
		job = mustOnlyJob(t, cronStore, ctx)
	}

	if err := cronStore.EnableJob(ctx, job.ID, false); err != nil {
		t.Fatalf("disable error: %v", err)
	}
	disabled, ok := cronStore.GetJob(ctx, job.ID)
	if !ok {
		t.Fatal("disabled job not found")
	}
	if disabled.Enabled {
		t.Fatal("expected job to be disabled")
	}
	if disabled.State.NextRunAtMS != nil {
		t.Fatalf("expected disabled job to clear next_run_at, got %v", *disabled.State.NextRunAtMS)
	}

	if err := cronStore.EnableJob(ctx, job.ID, true); err != nil {
		t.Fatalf("re-enable error: %v", err)
	}
	reenabled, ok := cronStore.GetJob(ctx, job.ID)
	if !ok {
		t.Fatal("re-enabled job not found")
	}
	if !reenabled.Enabled {
		t.Fatal("expected job to be enabled")
	}
	if reenabled.State.NextRunAtMS == nil {
		t.Fatal("expected re-enabled job to have next_run_at")
	}

	due := cronStore.GetDueJobs(time.UnixMilli(*reenabled.State.NextRunAtMS))
	if len(due) != 1 || due[0].ID != job.ID {
		t.Fatalf("expected job %s to be due after re-enable, got %#v", job.ID, due)
	}
}

func TestSQLiteCronStore_EnableAlreadyEnabledPreservesNextRun(t *testing.T) {
	cronStore, ctx, _ := newTestSQLiteCronStore(t)
	everyMS := int64(time.Hour / time.Millisecond)

	job, err := cronStore.AddJob(ctx, "job-idempotent", store.CronSchedule{
		Kind:    "every",
		EveryMS: &everyMS,
	}, "hello", false, "", "", "", "user-1")
	if err != nil {
		t.Fatalf("AddJob error: %v", err)
	}
	if job == nil {
		job = mustOnlyJob(t, cronStore, ctx)
	}
	if job.State.NextRunAtMS == nil {
		t.Fatal("expected new job to have next_run_at")
	}

	originalNextRun := *job.State.NextRunAtMS
	time.Sleep(20 * time.Millisecond)

	if err := cronStore.EnableJob(ctx, job.ID, true); err != nil {
		t.Fatalf("enable error: %v", err)
	}

	current, ok := cronStore.GetJob(ctx, job.ID)
	if !ok {
		t.Fatal("job not found after enable")
	}
	if current.State.NextRunAtMS == nil {
		t.Fatal("expected enabled job to keep next_run_at")
	}
	if got := *current.State.NextRunAtMS; got != originalNextRun {
		t.Fatalf("got next_run_at %d, want preserved %d", got, originalNextRun)
	}
}

func TestSQLiteCronStore_EnableExpiredAtReturnsError(t *testing.T) {
	cronStore, ctx, _ := newTestSQLiteCronStore(t)
	future := time.Now().Add(time.Hour).UnixMilli()

	job, err := cronStore.AddJob(ctx, "job-expired-at", store.CronSchedule{
		Kind: "at",
		AtMS: &future,
	}, "hello", false, "", "", "", "user-1")
	if err != nil {
		t.Fatalf("AddJob error: %v", err)
	}
	if job == nil {
		job = mustOnlyJob(t, cronStore, ctx)
	}

	if err := cronStore.EnableJob(ctx, job.ID, false); err != nil {
		t.Fatalf("disable error: %v", err)
	}

	past := time.Now().Add(-time.Hour).UnixMilli()
	if _, err := cronStore.UpdateJob(ctx, job.ID, store.CronJobPatch{
		Schedule: &store.CronSchedule{Kind: "at", AtMS: &past},
	}); err != nil {
		t.Fatalf("UpdateJob error: %v", err)
	}

	err = cronStore.EnableJob(ctx, job.ID, true)
	if !errors.Is(err, store.ErrCronJobNoFutureRun) {
		t.Fatalf("got %v, want ErrCronJobNoFutureRun", err)
	}

	current, ok := cronStore.GetJob(ctx, job.ID)
	if !ok {
		t.Fatal("job not found after failed enable")
	}
	if current.Enabled {
		t.Fatal("expected failed enable to leave job disabled")
	}
	if current.State.NextRunAtMS != nil {
		t.Fatalf("expected failed enable to leave next_run_at nil, got %v", *current.State.NextRunAtMS)
	}
}

func TestSQLiteCronStore_SchedulerSkipsDisabledCachedJob(t *testing.T) {
	cronStore, ctx, db := newTestSQLiteCronStore(t)
	everyMS := int64(time.Minute / time.Millisecond)

	job, err := cronStore.AddJob(ctx, "job-stale-cache", store.CronSchedule{
		Kind:    "every",
		EveryMS: &everyMS,
	}, "hello", false, "", "", "", "user-1")
	if err != nil {
		t.Fatalf("AddJob error: %v", err)
	}
	if job == nil {
		job = mustOnlyJob(t, cronStore, ctx)
	}

	jobUUID := uuid.MustParse(job.ID)
	pastRun := time.Now().Add(-time.Minute)
	if _, err := db.ExecContext(ctx,
		"UPDATE cron_jobs SET next_run_at = ?, updated_at = ? WHERE id = ? AND tenant_id = ?",
		pastRun, time.Now(), jobUUID, store.MasterTenantID,
	); err != nil {
		t.Fatalf("mark due error: %v", err)
	}

	cached, ok := cronStore.GetJob(ctx, job.ID)
	if !ok {
		t.Fatal("cached job not found")
	}
	pastMS := pastRun.UnixMilli()
	cached.Enabled = true
	cached.State.NextRunAtMS = &pastMS
	cronStore.jobCache = []store.CronJob{*cached}
	cronStore.cacheLoaded = true
	cronStore.cacheTime = time.Now()
	cronStore.cacheTTL = time.Hour
	cronStore.baseCtx = context.Background()

	var runs atomic.Int32
	cronStore.onJob = func(job *store.CronJob) (*store.CronJobResult, error) {
		runs.Add(1)
		return &store.CronJobResult{Content: "ok"}, nil
	}

	if _, err := db.ExecContext(ctx,
		"UPDATE cron_jobs SET enabled = 0, next_run_at = NULL, updated_at = ? WHERE id = ? AND tenant_id = ?",
		time.Now(), jobUUID, store.MasterTenantID,
	); err != nil {
		t.Fatalf("disable error: %v", err)
	}

	cronStore.checkAndRunDueJobs()

	if got := runs.Load(); got != 0 {
		t.Fatalf("expected claimed disabled job to be skipped, got %d executions", got)
	}
}

func TestSQLiteCronStore_ExecuteOneJob_DoesNotRestoreNextRunAfterDisable(t *testing.T) {
	cronStore, ctx, db := newTestSQLiteCronStore(t)
	everyMS := int64(time.Minute / time.Millisecond)

	job, err := cronStore.AddJob(ctx, "job-midrun-disable", store.CronSchedule{
		Kind:    "every",
		EveryMS: &everyMS,
	}, "hello", false, "", "", "", "user-1")
	if err != nil {
		t.Fatalf("AddJob error: %v", err)
	}
	if job == nil {
		job = mustOnlyJob(t, cronStore, ctx)
	}

	jobUUID := uuid.MustParse(job.ID)
	if _, err := db.ExecContext(ctx,
		"UPDATE cron_jobs SET next_run_at = NULL, updated_at = ? WHERE id = ? AND tenant_id = ?",
		time.Now(), jobUUID, store.MasterTenantID,
	); err != nil {
		t.Fatalf("claim setup error: %v", err)
	}

	cronStore.executeOneJob(*job, func(job *store.CronJob) (*store.CronJobResult, error) {
		if err := cronStore.EnableJob(ctx, job.ID, false); err != nil {
			t.Fatalf("disable during run error: %v", err)
		}
		return &store.CronJobResult{Content: "ok"}, nil
	})

	current := mustRawJob(t, db, jobUUID)
	if current.enabled {
		t.Fatal("expected job to remain disabled after run")
	}
	if current.nextRunAt != nil {
		t.Fatalf("expected disabled job to keep next_run_at nil, got %v", current.nextRunAt)
	}
}

func TestSQLiteCronStore_EnableJob_IgnoresMalformedPayload(t *testing.T) {
	cronStore, ctx, db := newTestSQLiteCronStore(t)
	everyMS := int64(time.Minute / time.Millisecond)

	job, err := cronStore.AddJob(ctx, "job-bad-payload", store.CronSchedule{
		Kind:    "every",
		EveryMS: &everyMS,
	}, "hello", false, "", "", "", "user-1")
	if err != nil {
		t.Fatalf("AddJob error: %v", err)
	}
	if job == nil {
		job = mustOnlyJob(t, cronStore, ctx)
	}

	jobUUID := uuid.MustParse(job.ID)
	if _, err := db.ExecContext(ctx,
		"UPDATE cron_jobs SET payload = ? WHERE id = ? AND tenant_id = ?",
		"{", jobUUID, store.MasterTenantID,
	); err != nil {
		t.Fatalf("corrupt payload error: %v", err)
	}

	if err := cronStore.EnableJob(ctx, job.ID, false); err != nil {
		t.Fatalf("toggle with malformed payload error: %v", err)
	}

	current := mustRawJob(t, db, jobUUID)
	if current.enabled {
		t.Fatal("expected malformed-payload job to be disabled")
	}
	if current.nextRunAt != nil {
		t.Fatalf("expected malformed-payload job to clear next_run_at, got %v", current.nextRunAt)
	}
}

func newTestSQLiteCronStore(t *testing.T) (*SQLiteCronStore, context.Context, *sql.DB) {
	t.Helper()

	db, err := OpenDB(filepath.Join(t.TempDir(), "cron.db"))
	if err != nil {
		t.Fatalf("OpenDB error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema error: %v", err)
	}

	ctx := store.WithCrossTenant(store.WithTenantID(context.Background(), store.MasterTenantID))
	cronStore := NewSQLiteCronStore(db)
	cronStore.baseCtx = context.Background()
	cronStore.cacheTTL = time.Hour
	return cronStore, ctx, db
}

func mustOnlyJob(t *testing.T, cronStore *SQLiteCronStore, ctx context.Context) *store.CronJob {
	t.Helper()

	jobs := cronStore.ListJobs(ctx, true, "", "")
	if len(jobs) != 1 {
		t.Fatalf("expected exactly 1 job, got %#v", jobs)
	}

	job := jobs[0]
	return &job
}

type rawCronJobState struct {
	enabled   bool
	nextRunAt *time.Time
}

func mustRawJob(t *testing.T, db *sql.DB, id uuid.UUID) rawCronJobState {
	t.Helper()

	var (
		enabled   bool
		nextRunAt nullSqliteTime
	)
	if err := db.QueryRow(
		"SELECT enabled, next_run_at FROM cron_jobs WHERE id = ?",
		id,
	).Scan(&enabled, &nextRunAt); err != nil {
		t.Fatalf("raw cron job query error: %v", err)
	}

	state := rawCronJobState{enabled: enabled}
	if nextRunAt.Valid {
		next := nextRunAt.Time
		state.nextRunAt = &next
	}
	return state
}
