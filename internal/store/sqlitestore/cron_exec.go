//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (s *SQLiteCronStore) RunJob(ctx context.Context, jobID string, force bool) (bool, string, error) {
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

	if id, parseErr := uuid.Parse(jobID); parseErr == nil {
		if _, execErr := s.db.ExecContext(ctx, "UPDATE cron_jobs SET last_status = 'running', updated_at = ? WHERE id = ?", time.Now(), id); execErr != nil {
			slog.Warn("cron: failed to mark job running", "job", jobID, "error", execErr)
		}
	}
	s.InvalidateCache()

	s.emitEvent(store.CronEvent{Action: "running", JobID: job.ID, JobName: job.Name, UserID: job.UserID})
	s.executeOneJob(*job, handler)

	s.mu.Lock()
	s.cacheLoaded = false
	s.mu.Unlock()
	return true, "", nil
}

func (s *SQLiteCronStore) GetRunLog(ctx context.Context, jobID string, limit, offset int) ([]store.CronRunLogEntry, int) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	const cols = "r.job_id, r.status, r.error, r.summary, r.ran_at, COALESCE(r.duration_ms, 0), COALESCE(r.input_tokens, 0), COALESCE(r.output_tokens, 0)"

	// Tenant isolation via JOIN with cron_jobs.
	var tenantJoin, tenantWhere string
	var tenantArgs []any
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid != uuid.Nil {
			tenantJoin = " JOIN cron_jobs j ON r.job_id = j.id"
			tenantWhere = " AND j.tenant_id = ?"
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
		countQ := fmt.Sprintf("SELECT COUNT(*) FROM cron_run_logs r%s WHERE r.job_id = ?%s", tenantJoin, tenantWhere)
		countArgs := append(tenantArgs, id)
		s.db.QueryRowContext(ctx, countQ, countArgs...).Scan(&total)

		dataQ := fmt.Sprintf("SELECT %s FROM cron_run_logs r%s WHERE r.job_id = ?%s ORDER BY r.ran_at DESC LIMIT ? OFFSET ?",
			cols, tenantJoin, tenantWhere)
		dataArgs := append(tenantArgs, id, limit, offset)
		rows, err = s.db.QueryContext(ctx, dataQ, dataArgs...)
	} else {
		countQ := fmt.Sprintf("SELECT COUNT(*) FROM cron_run_logs r%s WHERE 1=1%s", tenantJoin, tenantWhere)
		s.db.QueryRowContext(ctx, countQ, tenantArgs...).Scan(&total)

		dataQ := fmt.Sprintf("SELECT %s FROM cron_run_logs r%s WHERE 1=1%s ORDER BY r.ran_at DESC LIMIT ? OFFSET ?",
			cols, tenantJoin, tenantWhere)
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
	if rErr := rows.Err(); rErr != nil {
		slog.Warn("cron: run log iteration error", "error", rErr)
	}
	return result, total
}

func (s *SQLiteCronStore) Status() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var count int64
	s.db.QueryRow("SELECT COUNT(*) FROM cron_jobs WHERE enabled = 1").Scan(&count)
	return map[string]any{
		"enabled": s.running,
		"jobs":    count,
	}
}
