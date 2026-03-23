package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/adhocore/gronx"
	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// scanJob fetches a single cron job by ID with tenant filtering.
func (s *PGCronStore) scanJob(ctx context.Context, id uuid.UUID) (*store.CronJob, error) {
	q := `SELECT id, tenant_id, agent_id, user_id, name, enabled, schedule_kind, cron_expression, run_at, timezone,
		 interval_ms, payload, delete_after_run, next_run_at, last_run_at, last_status, last_error,
		 created_at, updated_at FROM cron_jobs WHERE id = $1`
	args := []any{id}

	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return nil, fmt.Errorf("tenant_id required")
		}
		q += fmt.Sprintf(" AND tenant_id = $%d", len(args)+1)
		args = append(args, tid)
	}

	row := s.db.QueryRowContext(ctx, q, args...)
	return scanCronSingleRow(row)
}

// --- Scan helpers ---

type cronRowScanner interface {
	Scan(dest ...any) error
}

func scanCronRow(row cronRowScanner) (*store.CronJob, error) {
	var id uuid.UUID
	var tenantID uuid.UUID
	var agentID *uuid.UUID
	var userID *string
	var name, scheduleKind string
	var enabled, deleteAfterRun bool
	var cronExpr, tz, lastStatus, lastError *string
	var runAt, nextRunAt, lastRunAt *time.Time
	var intervalMS *int64
	var payloadJSON []byte
	var createdAt, updatedAt time.Time

	err := row.Scan(&id, &tenantID, &agentID, &userID, &name, &enabled, &scheduleKind, &cronExpr, &runAt, &tz,
		&intervalMS, &payloadJSON, &deleteAfterRun, &nextRunAt, &lastRunAt, &lastStatus, &lastError,
		&createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}

	var payload store.CronPayload
	json.Unmarshal(payloadJSON, &payload)

	job := &store.CronJob{
		ID:       id.String(),
		TenantID: tenantID,
		Name:     name,
		Enabled:  enabled,
		Schedule: store.CronSchedule{
			Kind: scheduleKind,
		},
		Payload:        payload,
		CreatedAtMS:    createdAt.UnixMilli(),
		UpdatedAtMS:    updatedAt.UnixMilli(),
		DeleteAfterRun: deleteAfterRun,
	}

	if agentID != nil {
		job.AgentID = agentID.String()
	}
	if userID != nil {
		job.UserID = *userID
	}
	if cronExpr != nil {
		job.Schedule.Expr = *cronExpr
	}
	if runAt != nil {
		ms := runAt.UnixMilli()
		job.Schedule.AtMS = &ms
	}
	if intervalMS != nil {
		job.Schedule.EveryMS = intervalMS
	}
	if tz != nil {
		job.Schedule.TZ = *tz
	}
	if nextRunAt != nil {
		ms := nextRunAt.UnixMilli()
		job.State.NextRunAtMS = &ms
	}
	if lastRunAt != nil {
		ms := lastRunAt.UnixMilli()
		job.State.LastRunAtMS = &ms
	}
	if lastStatus != nil {
		job.State.LastStatus = *lastStatus
	}
	if lastError != nil {
		job.State.LastError = *lastError
	}

	return job, nil
}

func scanCronSingleRow(row *sql.Row) (*store.CronJob, error) {
	return scanCronRow(row)
}

// --- Helpers ---

// computeNextRun calculates the next run time for a schedule.
// defaultTZ is the gateway-level fallback IANA timezone used when the
// schedule itself does not specify a timezone (existing jobs with TZ = NULL).
func computeNextRun(schedule *store.CronSchedule, now time.Time, defaultTZ string) *time.Time {
	switch schedule.Kind {
	case "at":
		if schedule.AtMS != nil {
			t := time.UnixMilli(*schedule.AtMS)
			if t.After(now) {
				return &t
			}
		}
		return nil
	case "every":
		if schedule.EveryMS != nil && *schedule.EveryMS > 0 {
			t := now.Add(time.Duration(*schedule.EveryMS) * time.Millisecond)
			return &t
		}
		return nil
	case "cron":
		if schedule.Expr == "" {
			return nil
		}
		tz := schedule.TZ
		if tz == "" {
			tz = defaultTZ
		}
		evalTime := now
		if tz != "" {
			if loc, err := time.LoadLocation(tz); err == nil {
				evalTime = now.In(loc)
			}
		}
		nextTime, err := gronx.NextTickAfter(schedule.Expr, evalTime, false)
		if err != nil {
			return nil
		}
		utcNext := nextTime.UTC()
		return &utcNext
	default:
		return nil
	}
}
