package pg

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (s *PGCronStore) AddJob(ctx context.Context, name string, schedule store.CronSchedule, message string, deliver bool, channel, to, agentID, userID string) (*store.CronJob, error) {
	// Apply default timezone for cron expressions when not set per-job.
	if schedule.TZ == "" && schedule.Kind == "cron" && s.defaultTZ != "" {
		schedule.TZ = s.defaultTZ
	}
	if schedule.TZ != "" {
		if _, err := time.LoadLocation(schedule.TZ); err != nil {
			return nil, fmt.Errorf("invalid timezone: %s", schedule.TZ)
		}
	}

	payload := store.CronPayload{
		Kind: "agent_turn", Message: message,
	}
	payloadJSON, _ := json.Marshal(payload)

	id := uuid.Must(uuid.NewV7())
	now := time.Now()
	scheduleKind := schedule.Kind
	deleteAfterRun := schedule.Kind == "at"

	var cronExpr, tz *string
	var runAt *time.Time
	if schedule.Expr != "" {
		cronExpr = &schedule.Expr
	}
	if schedule.AtMS != nil {
		t := time.UnixMilli(*schedule.AtMS)
		runAt = &t
	}
	if schedule.TZ != "" {
		tz = &schedule.TZ
	}

	var agentUUID *uuid.UUID
	if agentID != "" {
		aid, err := uuid.Parse(agentID)
		if err == nil {
			agentUUID = &aid
		}
	}

	var userIDPtr *string
	if userID != "" {
		userIDPtr = &userID
	}

	var intervalMS *int64
	if schedule.EveryMS != nil {
		intervalMS = schedule.EveryMS
	}

	nextRun := computeNextRun(&schedule, now, s.defaultTZ)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO cron_jobs (id, tenant_id, agent_id, user_id, name, enabled, schedule_kind, cron_expression, run_at, timezone,
		 interval_ms, payload, delete_after_run, deliver, deliver_channel, deliver_to, wake_heartbeat, next_run_at, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, true, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)`,
		id, tenantIDForInsert(ctx), agentUUID, userIDPtr, name, scheduleKind, cronExpr, runAt, tz,
		intervalMS, payloadJSON, deleteAfterRun, deliver, channel, to, false, nextRun, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("create cron job: %w", err)
	}

	s.cacheLoaded = false // invalidate cache

	job, _ := s.GetJob(ctx, id.String())
	return job, nil
}

func (s *PGCronStore) GetJob(ctx context.Context, jobID string) (*store.CronJob, bool) {
	id, err := uuid.Parse(jobID)
	if err != nil {
		return nil, false
	}
	job, err := s.scanJob(ctx, id)
	if err != nil {
		return nil, false
	}
	return job, true
}

func (s *PGCronStore) ListJobs(ctx context.Context, includeDisabled bool, agentID, userID string) []store.CronJob {
	q := `SELECT id, tenant_id, agent_id, user_id, name, enabled, schedule_kind, cron_expression, run_at, timezone,
		 interval_ms, payload, delete_after_run, stateless, deliver, deliver_channel, deliver_to, wake_heartbeat,
		 next_run_at, last_run_at, last_status, last_error,
		 created_at, updated_at FROM cron_jobs WHERE 1=1`

	var args []any
	argIdx := 1

	if !includeDisabled {
		q += fmt.Sprintf(" AND enabled = $%d", argIdx)
		args = append(args, true)
		argIdx++
	}
	if agentID != "" {
		if aid, err := uuid.Parse(agentID); err == nil {
			q += fmt.Sprintf(" AND agent_id = $%d", argIdx)
			args = append(args, aid)
			argIdx++
		}
	}
	if userID != "" {
		q += fmt.Sprintf(" AND user_id = $%d", argIdx)
		args = append(args, userID)
		argIdx++
	}
	clause, targs, _, tErr := scopeClause(ctx, argIdx)
	if tErr != nil {
		slog.Warn("cron.ListJobs: tenant context missing, returning empty (fail-closed)", "error", tErr)
		return nil
	}
	if clause != "" {
		q += clause
		args = append(args, targs...)
		argIdx++
	}

	q += " ORDER BY created_at DESC"

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []store.CronJob
	for rows.Next() {
		job, err := scanCronRow(rows)
		if err != nil {
			continue
		}
		result = append(result, *job)
	}
	return result
}

func (s *PGCronStore) RemoveJob(ctx context.Context, jobID string) error {
	id, err := uuid.Parse(jobID)
	if err != nil {
		return fmt.Errorf("invalid job ID: %s", jobID)
	}

	q := "DELETE FROM cron_jobs WHERE id = $1"
	args := []any{id}

	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return fmt.Errorf("tenant_id required")
		}
		q += fmt.Sprintf(" AND tenant_id = $%d", len(args)+1)
		args = append(args, tid)
	}

	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("job not found")
	}
	s.cacheLoaded = false
	return nil
}

func (s *PGCronStore) EnableJob(ctx context.Context, jobID string, enabled bool) error {
	id, err := uuid.Parse(jobID)
	if err != nil {
		return fmt.Errorf("invalid job ID: %s", jobID)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	current, err := s.lockCronJobForMutation(ctx, tx, id, false)
	if err != nil {
		return err
	}

	now := time.Now()
	nextRun, err := store.NextRunForToggle(&current.Schedule, enabled, current.Enabled, current.NextRunAt, now, s.defaultTZ)
	if err != nil {
		return err
	}

	if err := execCronJobUpdateTx(ctx, tx, id, map[string]any{
		"enabled":     enabled,
		"next_run_at": nextRun,
		"updated_at":  now,
	}); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	s.InvalidateCache()
	return nil
}
