//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (s *SQLiteCronStore) AddJob(ctx context.Context, name string, schedule store.CronSchedule, message string, deliver bool, channel, to, agentID, userID string) (*store.CronJob, error) {
	if schedule.TZ == "" && schedule.Kind == "cron" && s.defaultTZ != "" {
		schedule.TZ = s.defaultTZ
	}
	if schedule.TZ != "" {
		if _, err := time.LoadLocation(schedule.TZ); err != nil {
			return nil, fmt.Errorf("invalid timezone: %s", schedule.TZ)
		}
	}

	payload := store.CronPayload{
		Kind: "agent_turn", Message: message, Deliver: deliver, Channel: channel, To: to,
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
		if aid, err := uuid.Parse(agentID); err == nil {
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
		 interval_ms, payload, delete_after_run, next_run_at, created_at, updated_at)
		 VALUES (?,?,?,?,?,1,?,?,?,?,?,?,?,?,?,?)`,
		id, tenantIDForInsert(ctx), agentUUID, userIDPtr, name, scheduleKind, cronExpr, runAt, tz,
		intervalMS, payloadJSON, deleteAfterRun, nextRun, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("create cron job: %w", err)
	}

	s.InvalidateCache()
	job, _ := s.GetJob(ctx, id.String())
	return job, nil
}

func (s *SQLiteCronStore) GetJob(ctx context.Context, jobID string) (*store.CronJob, bool) {
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

func (s *SQLiteCronStore) ListJobs(ctx context.Context, includeDisabled bool, agentID, userID string) []store.CronJob {
	q := `SELECT id, tenant_id, agent_id, user_id, name, enabled, schedule_kind, cron_expression, run_at, timezone,
		 interval_ms, payload, delete_after_run, next_run_at, last_run_at, last_status, last_error,
		 created_at, updated_at FROM cron_jobs WHERE 1=1`

	var args []any

	if !includeDisabled {
		q += " AND enabled = ?"
		args = append(args, true)
	}
	if agentID != "" {
		if aid, err := uuid.Parse(agentID); err == nil {
			q += " AND agent_id = ?"
			args = append(args, aid)
		}
	}
	if userID != "" {
		q += " AND user_id = ?"
		args = append(args, userID)
	}

	clause, targs, tErr := scopeClause(ctx)
	if tErr != nil {
		slog.Warn("cron.ListJobs: tenant context missing, returning empty (fail-closed)", "error", tErr)
		return nil
	}
	if clause != "" {
		q += clause
		args = append(args, targs...)
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
	if err := rows.Err(); err != nil {
		slog.Warn("cron: list jobs iteration error", "error", err)
	}
	return result
}

func (s *SQLiteCronStore) RemoveJob(ctx context.Context, jobID string) error {
	id, err := uuid.Parse(jobID)
	if err != nil {
		return fmt.Errorf("invalid job ID: %s", jobID)
	}

	q := "DELETE FROM cron_jobs WHERE id = ?"
	args := []any{id}

	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return fmt.Errorf("tenant_id required")
		}
		q += " AND tenant_id = ?"
		args = append(args, tid)
	}

	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("job not found")
	}
	s.InvalidateCache()
	return nil
}

func (s *SQLiteCronStore) EnableJob(ctx context.Context, jobID string, enabled bool) error {
	id, err := uuid.Parse(jobID)
	if err != nil {
		return fmt.Errorf("invalid job ID: %s", jobID)
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

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

func (s *SQLiteCronStore) UpdateJob(ctx context.Context, jobID string, patch store.CronJobPatch) (*store.CronJob, error) {
	id, err := uuid.Parse(jobID)
	if err != nil {
		return nil, fmt.Errorf("invalid job ID: %s", jobID)
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	current, err := s.lockCronJobForMutation(ctx, tx, id, true)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	updates := make(map[string]any)
	effectiveEnabled := current.Enabled
	if patch.Enabled != nil {
		effectiveEnabled = *patch.Enabled
		updates["enabled"] = effectiveEnabled
	}

	if patch.Name != "" {
		updates["name"] = patch.Name
	}
	if patch.DeleteAfterRun != nil {
		updates["delete_after_run"] = *patch.DeleteAfterRun
	}
	if patch.AgentID != nil {
		if *patch.AgentID == "" {
			updates["agent_id"] = nil
		} else if aid, parseErr := uuid.Parse(*patch.AgentID); parseErr == nil {
			updates["agent_id"] = aid
		}
	}

	if patch.Schedule != nil {
		merged := store.MergeCronSchedule(current.Schedule, patch.Schedule)
		if err := store.ValidateCronSchedule(&merged); err != nil {
			return nil, err
		}

		store.ApplyCronScheduleUpdates(updates, merged)

		nextRun, err := store.NextRunForSchedule(&merged, effectiveEnabled, now, s.defaultTZ)
		if err != nil {
			return nil, err
		}
		updates["next_run_at"] = nextRun
	} else if patch.Enabled != nil {
		nextRun, err := store.NextRunForToggle(&current.Schedule, effectiveEnabled, current.Enabled, current.NextRunAt, now, s.defaultTZ)
		if err != nil {
			return nil, err
		}
		updates["next_run_at"] = nextRun
	}

	needsPayloadUpdate := patch.Message != "" || patch.Deliver != nil || patch.Channel != nil || patch.To != nil || patch.WakeHeartbeat != nil
	if needsPayloadUpdate {
		payload := current.Payload
		if patch.Message != "" {
			payload.Message = patch.Message
		}
		if patch.Deliver != nil {
			payload.Deliver = *patch.Deliver
		}
		if patch.Channel != nil {
			payload.Channel = *patch.Channel
		}
		if patch.To != nil {
			payload.To = *patch.To
		}
		if patch.WakeHeartbeat != nil {
			payload.WakeHeartbeat = *patch.WakeHeartbeat
		}

		mergedPayload, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal payload for job %s: %w", jobID, err)
		}
		updates["payload"] = mergedPayload
	}

	updates["updated_at"] = now
	if err := execCronJobUpdateTx(ctx, tx, id, updates); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	s.InvalidateCache()
	job, _ := s.scanJob(ctx, id)
	return job, nil
}

func (s *SQLiteCronStore) lockCronJobForMutation(ctx context.Context, tx *sql.Tx, id uuid.UUID, loadPayload bool) (*store.CronJobMutableState, error) {
	q := `SELECT enabled, schedule_kind, cron_expression, run_at, timezone, interval_ms, next_run_at, payload
		FROM cron_jobs WHERE id = ?`
	args := []any{id}

	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return nil, fmt.Errorf("tenant_id required")
		}
		q += " AND tenant_id = ?"
		args = append(args, tid)
	}

	var (
		state        store.CronJobMutableState
		scheduleKind string
		cronExpr     *string
		runAt        nullSqliteTime
		tz           *string
		intervalMS   *int64
		nextRunAt    nullSqliteTime
		payloadJSON  []byte
	)

	if err := tx.QueryRowContext(ctx, q, args...).Scan(
		&state.Enabled,
		&scheduleKind,
		&cronExpr,
		&runAt,
		&tz,
		&intervalMS,
		&nextRunAt,
		&payloadJSON,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrCronJobNotFound
		}
		return nil, err
	}

	state.Schedule = store.CronSchedule{Kind: scheduleKind}
	if cronExpr != nil {
		state.Schedule.Expr = *cronExpr
	}
	if runAt.Valid {
		ms := runAt.Time.UnixMilli()
		state.Schedule.AtMS = &ms
	}
	if tz != nil {
		state.Schedule.TZ = *tz
	}
	if intervalMS != nil {
		state.Schedule.EveryMS = intervalMS
	}
	if nextRunAt.Valid {
		next := nextRunAt.Time
		state.NextRunAt = &next
	}

	if loadPayload && len(payloadJSON) > 0 {
		if err := json.Unmarshal(payloadJSON, &state.Payload); err != nil {
			return nil, fmt.Errorf("failed to parse existing payload for job %s: %w", id, err)
		}
	}

	return &state, nil
}


func execCronJobUpdateTx(ctx context.Context, tx *sql.Tx, id uuid.UUID, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}

	var (
		setClauses []string
		args       []any
	)
	for _, col := range store.SortedUpdateColumns(updates) {
		if !validColumnName.MatchString(col) {
			return fmt.Errorf("invalid column name: %q", col)
		}
		setClauses = append(setClauses, col+" = ?")
		args = append(args, sqliteVal(updates[col]))
	}

	args = append(args, id)
	q := "UPDATE cron_jobs SET " + strings.Join(setClauses, ", ") + " WHERE id = ?"
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return fmt.Errorf("tenant_id required")
		}
		args = append(args, tid)
		q += " AND tenant_id = ?"
	}

	res, err := tx.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return store.ErrCronJobNotFound
	}
	return nil
}

