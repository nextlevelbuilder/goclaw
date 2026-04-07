package pg

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (s *PGCronStore) RunJob(ctx context.Context, jobID string, force bool) (bool, string, error) {
	job, ok := s.GetJob(ctx, jobID)
	if !ok {
		return false, "", fmt.Errorf("job %s not found", jobID)
	}

	s.mu.Lock()
	handler := s.onJob
	s.mu.Unlock()

	if handler == nil {
		return false, "", fmt.Errorf("no job handler configured")
	}

	// Claim the job for forced execution by clearing next_run_at.
	// executeOneJob calls loadClaimedJob which requires next_run_at IS NULL — same
	// invariant that claimDueJob establishes for scheduled runs.
	id, parseErr := uuid.Parse(jobID)
	if parseErr != nil {
		return false, "", fmt.Errorf("invalid job id %q: %w", jobID, parseErr)
	}
	res, err := s.db.ExecContext(ctx, "UPDATE cron_jobs SET last_status = 'running', next_run_at = NULL, updated_at = $1 WHERE id = $2 AND last_status IS DISTINCT FROM 'running'", time.Now(), id)
	if err != nil {
		slog.Warn("cron: failed to claim job for forced run", "jobId", jobID, "error", err)
		return false, "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, "", fmt.Errorf("job %s is already running", jobID)
	}
	s.mu.Lock()
	s.cacheLoaded = false
	s.mu.Unlock()

	s.emitEvent(store.CronEvent{Action: "running", JobID: job.ID, JobName: job.Name, UserID: job.UserID})

	// Run directly without reload — job already loaded and claimed above.
	// reloadClaimed=false skips loadClaimedJob (which requires enabled=true),
	// allowing manual runs on disabled jobs.
	s.executeOneJob(*job, handler, false)
	s.mu.Lock()
	s.cacheLoaded = false
	s.mu.Unlock()
	return true, "", nil
}

func (s *PGCronStore) GetRunLog(ctx context.Context, jobID string, limit, offset int) ([]store.CronRunLogEntry, int) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	// Aliased cols so sqlx StructScan maps to cronRunLogRow db tags.
	const cols = "r.job_id, r.status, r.error, r.summary, r.ran_at," +
		" COALESCE(r.duration_ms, 0) AS duration_ms," +
		" COALESCE(r.input_tokens, 0) AS input_tokens," +
		" COALESCE(r.output_tokens, 0) AS output_tokens"

	// Build tenant-aware WHERE clause via JOIN with cron_jobs.
	var tenantJoin, tenantWhere string
	var tenantArgs []any
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid != uuid.Nil {
			tenantJoin = " JOIN cron_jobs j ON r.job_id = j.id"
			tenantWhere = " AND j.tenant_id = $1"
			tenantArgs = append(tenantArgs, tid)
		}
	}

	var total int
	var dataQ string
	var dataArgs []any

	if jobID != "" {
		id, parseErr := uuid.Parse(jobID)
		if parseErr != nil {
			return nil, 0
		}
		argIdx := len(tenantArgs) + 1
		countQ := fmt.Sprintf("SELECT COUNT(*) FROM cron_run_logs r%s WHERE r.job_id = $%d%s", tenantJoin, argIdx, tenantWhere)
		s.db.QueryRowContext(ctx, countQ, append(tenantArgs, id)...).Scan(&total) //nolint:errcheck

		dataQ = fmt.Sprintf("SELECT %s FROM cron_run_logs r%s WHERE r.job_id = $%d%s ORDER BY r.ran_at DESC LIMIT $%d OFFSET $%d",
			cols, tenantJoin, argIdx, tenantWhere, argIdx+1, argIdx+2)
		dataArgs = append(tenantArgs, id, limit, offset)
	} else {
		argIdx := len(tenantArgs) + 1
		countQ := fmt.Sprintf("SELECT COUNT(*) FROM cron_run_logs r%s WHERE 1=1%s", tenantJoin, tenantWhere)
		s.db.QueryRowContext(ctx, countQ, tenantArgs...).Scan(&total) //nolint:errcheck

		dataQ = fmt.Sprintf("SELECT %s FROM cron_run_logs r%s WHERE 1=1%s ORDER BY r.ran_at DESC LIMIT $%d OFFSET $%d",
			cols, tenantJoin, tenantWhere, argIdx, argIdx+1)
		dataArgs = append(tenantArgs, limit, offset)
	}

	var scanned []cronRunLogRow
	if err := pkgSqlxDB.SelectContext(ctx, &scanned, dataQ, dataArgs...); err != nil {
		return nil, total
	}
	result := make([]store.CronRunLogEntry, 0, len(scanned))
	for i := range scanned {
		result = append(result, scanned[i].toCronRunLogEntry())
	}
	return result, total
}

func (s *PGCronStore) Status() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var count int64
	s.db.QueryRow("SELECT COUNT(*) FROM cron_jobs WHERE enabled = true").Scan(&count)
	return map[string]any{
		"enabled": s.running,
		"jobs":    count,
	}
}
