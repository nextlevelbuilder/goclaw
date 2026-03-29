//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/adhocore/gronx"
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

	q := `SELECT schedule_kind, cron_expression, run_at, timezone, interval_ms
		FROM cron_jobs WHERE id = ?`
	args := []any{id}

	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return fmt.Errorf("tenant_id required")
		}
		q += " AND tenant_id = ?"
		args = append(args, tid)
	}

	var scheduleKind string
	var cronExpr, tz *string
	var runAt *time.Time
	var intervalMS *int64
	if err := s.db.QueryRowContext(ctx, q, args...).Scan(&scheduleKind, &cronExpr, &runAt, &tz, &intervalMS); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("job not found")
		}
		return err
	}

	schedule := store.CronSchedule{Kind: scheduleKind}
	if cronExpr != nil {
		schedule.Expr = *cronExpr
	}
	if runAt != nil {
		ms := runAt.UnixMilli()
		schedule.AtMS = &ms
	}
	if tz != nil {
		schedule.TZ = *tz
	}
	if intervalMS != nil {
		schedule.EveryMS = intervalMS
	}

	now := time.Now()
	nextRun := store.NextRunForToggle(&schedule, enabled, now, s.defaultTZ)

	q = "UPDATE cron_jobs SET enabled = ?, next_run_at = ?, updated_at = ? WHERE id = ?"
	args = []any{enabled, nextRun, now, id}

	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
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

func (s *SQLiteCronStore) UpdateJob(ctx context.Context, jobID string, patch store.CronJobPatch) (*store.CronJob, error) {
	id, err := uuid.Parse(jobID)
	if err != nil {
		return nil, fmt.Errorf("invalid job ID: %s", jobID)
	}

	updates := make(map[string]any)
	if patch.Name != "" {
		updates["name"] = patch.Name
	}
	if patch.Enabled != nil {
		updates["enabled"] = *patch.Enabled
	}

	// Build tenant WHERE suffix for schedule reads.
	tenantSuffix := ""
	var tenantArg []any
	if !store.IsCrossTenant(ctx) {
		if tid := store.TenantIDFromContext(ctx); tid != uuid.Nil {
			tenantSuffix = " AND tenant_id = ?"
			tenantArg = append(tenantArg, tid)
		}
	}

	if patch.Schedule != nil {
		var curKind string
		var curExpr, curTZ *string
		var curIntervalMS *int64
		var curRunAt *time.Time
		if err := s.db.QueryRowContext(ctx,
			"SELECT schedule_kind, cron_expression, timezone, interval_ms, run_at FROM cron_jobs WHERE id = ?"+tenantSuffix,
			append([]any{id}, tenantArg...)...,
		).Scan(&curKind, &curExpr, &curTZ, &curIntervalMS, &curRunAt); err != nil {
			return nil, fmt.Errorf("failed to fetch current schedule for job %s: %w", jobID, err)
		}

		newKind := patch.Schedule.Kind
		if newKind == "" {
			newKind = curKind
		}
		updates["schedule_kind"] = newKind

		switch newKind {
		case "cron":
			if patch.Schedule.Expr != "" {
				updates["cron_expression"] = patch.Schedule.Expr
			}
			if patch.Schedule.TZ != "" {
				if _, err := time.LoadLocation(patch.Schedule.TZ); err != nil {
					return nil, fmt.Errorf("invalid timezone: %s", patch.Schedule.TZ)
				}
				updates["timezone"] = patch.Schedule.TZ
			}
			if curKind != "cron" {
				updates["interval_ms"] = nil
				updates["run_at"] = nil
			}
		case "every":
			if patch.Schedule.EveryMS != nil {
				updates["interval_ms"] = *patch.Schedule.EveryMS
			}
			if curKind != "every" {
				updates["cron_expression"] = nil
				updates["timezone"] = nil
				updates["run_at"] = nil
			}
		case "at":
			if patch.Schedule.AtMS != nil {
				t := time.UnixMilli(*patch.Schedule.AtMS)
				updates["run_at"] = t
			}
			if curKind != "at" {
				updates["cron_expression"] = nil
				updates["timezone"] = nil
				updates["interval_ms"] = nil
			}
		}

		merged := buildMergedSchedule(newKind, curKind, patch.Schedule, curExpr, curTZ, curIntervalMS, curRunAt)
		if err := validateSchedule(&merged); err != nil {
			return nil, err
		}
		next := computeNextRun(&merged, time.Now(), s.defaultTZ)
		updates["next_run_at"] = next
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

	needsPayloadUpdate := patch.Message != "" || patch.Deliver != nil || patch.Channel != nil || patch.To != nil || patch.WakeHeartbeat != nil
	if needsPayloadUpdate {
		var payloadJSON []byte
		if scanErr := s.db.QueryRowContext(ctx, "SELECT payload FROM cron_jobs WHERE id = ?"+tenantSuffix, append([]any{id}, tenantArg...)...).Scan(&payloadJSON); scanErr == nil {
			var p store.CronPayload
			if len(payloadJSON) > 0 {
				if err := json.Unmarshal(payloadJSON, &p); err != nil {
					return nil, fmt.Errorf("failed to parse existing payload for job %s: %w", jobID, err)
				}
			}
			if patch.Message != "" {
				p.Message = patch.Message
			}
			if patch.Deliver != nil {
				p.Deliver = *patch.Deliver
			}
			if patch.Channel != nil {
				p.Channel = *patch.Channel
			}
			if patch.To != nil {
				p.To = *patch.To
			}
			if patch.WakeHeartbeat != nil {
				p.WakeHeartbeat = *patch.WakeHeartbeat
			}
			merged, err := json.Marshal(p)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal payload for job %s: %w", jobID, err)
			}
			updates["payload"] = merged
		}
	}

	updates["updated_at"] = time.Now()

	var execErr error
	if store.IsCrossTenant(ctx) {
		execErr = execMapUpdate(ctx, s.db, "cron_jobs", id, updates)
	} else {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			execErr = fmt.Errorf("tenant_id required for update")
		} else {
			execErr = execMapUpdateWhereTenant(ctx, s.db, "cron_jobs", updates, id, tid)
		}
	}
	if execErr != nil {
		return nil, execErr
	}

	s.InvalidateCache()
	job, _ := s.scanJob(ctx, id)
	return job, nil
}

func buildMergedSchedule(newKind, curKind string, patch *store.CronSchedule, curExpr, curTZ *string, curIntervalMS *int64, curRunAt *time.Time) store.CronSchedule {
	merged := store.CronSchedule{Kind: newKind}
	switch newKind {
	case "cron":
		if patch.Expr != "" {
			merged.Expr = patch.Expr
		} else if curExpr != nil {
			merged.Expr = *curExpr
		}
		if patch.TZ != "" {
			merged.TZ = patch.TZ
		} else if curTZ != nil && newKind == curKind {
			merged.TZ = *curTZ
		}
	case "every":
		if patch.EveryMS != nil {
			merged.EveryMS = patch.EveryMS
		} else if curIntervalMS != nil {
			merged.EveryMS = curIntervalMS
		}
	case "at":
		if patch.AtMS != nil {
			merged.AtMS = patch.AtMS
		} else if curRunAt != nil {
			ms := curRunAt.UnixMilli()
			merged.AtMS = &ms
		}
	}
	return merged
}

func validateSchedule(s *store.CronSchedule) error {
	switch s.Kind {
	case "cron":
		if s.Expr == "" {
			return fmt.Errorf("cron schedule requires expr")
		}
		gx := gronx.New()
		if !gx.IsValid(s.Expr) {
			return fmt.Errorf("invalid cron expression: %s", s.Expr)
		}
	case "every":
		if s.EveryMS == nil || *s.EveryMS <= 0 {
			return fmt.Errorf("every schedule requires positive everyMs")
		}
	case "at":
		if s.AtMS == nil {
			return fmt.Errorf("at schedule requires atMs")
		}
	}
	return nil
}
