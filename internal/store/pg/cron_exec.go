package pg

import (
	"context"
	"database/sql"
	"fmt"
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

	// Mark job as running before execution
	if id, parseErr := uuid.Parse(jobID); parseErr == nil {
		s.db.Exec("UPDATE cron_jobs SET last_status = 'running', updated_at = $1 WHERE id = $2", time.Now(), id)
	}
	s.mu.Lock()
	s.cacheLoaded = false
	s.mu.Unlock()

	s.emitEvent(store.CronEvent{Action: "running", JobID: job.ID, JobName: job.Name, UserID: job.UserID})

	// Use executeOneJob for proper state updates, run logging, and retry
	s.executeOneJob(*job, handler)
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

	const cols = "r.job_id, r.status, r.error, r.summary, r.ran_at, COALESCE(r.duration_ms, 0), COALESCE(r.input_tokens, 0), COALESCE(r.output_tokens, 0)"

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
	var rows *sql.Rows
	var err error

	if jobID != "" {
		id, parseErr := uuid.Parse(jobID)
		if parseErr != nil {
			return nil, 0
		}
		argIdx := len(tenantArgs) + 1
		countQ := fmt.Sprintf("SELECT COUNT(*) FROM cron_run_logs r%s WHERE r.job_id = $%d%s", tenantJoin, argIdx, tenantWhere)
		countArgs := append(tenantArgs, id)
		s.db.QueryRowContext(ctx, countQ, countArgs...).Scan(&total)

		dataQ := fmt.Sprintf("SELECT %s FROM cron_run_logs r%s WHERE r.job_id = $%d%s ORDER BY r.ran_at DESC LIMIT $%d OFFSET $%d",
			cols, tenantJoin, argIdx, tenantWhere, argIdx+1, argIdx+2)
		dataArgs := append(tenantArgs, id, limit, offset)
		rows, err = s.db.QueryContext(ctx, dataQ, dataArgs...)
	} else {
		argIdx := len(tenantArgs) + 1
		countQ := fmt.Sprintf("SELECT COUNT(*) FROM cron_run_logs r%s WHERE 1=1%s", tenantJoin, tenantWhere)
		s.db.QueryRowContext(ctx, countQ, tenantArgs...).Scan(&total)

		dataQ := fmt.Sprintf("SELECT %s FROM cron_run_logs r%s WHERE 1=1%s ORDER BY r.ran_at DESC LIMIT $%d OFFSET $%d",
			cols, tenantJoin, tenantWhere, argIdx, argIdx+1)
		dataArgs := append(tenantArgs, limit, offset)
		rows, err = s.db.QueryContext(ctx, dataQ, dataArgs...)
	}
	if err != nil {
		return nil, 0
	}
	defer rows.Close()

	var result []store.CronRunLogEntry
	for rows.Next() {
		var jobUUID uuid.UUID
		var status string
		var errStr, summary *string
		var ranAt time.Time
		var durationMS int64
		var inputTokens, outputTokens int
		if err := rows.Scan(&jobUUID, &status, &errStr, &summary, &ranAt, &durationMS, &inputTokens, &outputTokens); err != nil {
			continue
		}
		result = append(result, store.CronRunLogEntry{
			Ts:           ranAt.UnixMilli(),
			JobID:        jobUUID.String(),
			Status:       status,
			Error:        derefStr(errStr),
			Summary:      derefStr(summary),
			DurationMS:   durationMS,
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
		})
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
