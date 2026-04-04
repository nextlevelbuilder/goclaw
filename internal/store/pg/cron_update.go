package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (s *PGCronStore) UpdateJob(ctx context.Context, jobID string, patch store.CronJobPatch) (*store.CronJob, error) {
	id, err := uuid.Parse(jobID)
	if err != nil {
		return nil, fmt.Errorf("invalid job ID: %s", jobID)
	}

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

	if patch.Stateless != nil {
		updates["stateless"] = *patch.Stateless
	}
	if patch.Deliver != nil {
		updates["deliver"] = *patch.Deliver
	}
	if patch.DeliverChannel != nil {
		updates["deliver_channel"] = *patch.DeliverChannel
	}
	if patch.DeliverTo != nil {
		updates["deliver_to"] = *patch.DeliverTo
	}
	if patch.WakeHeartbeat != nil {
		updates["wake_heartbeat"] = *patch.WakeHeartbeat
	}

	if patch.Message != "" {
		payload := current.Payload
		payload.Message = patch.Message
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

func (s *PGCronStore) lockCronJobForMutation(ctx context.Context, tx *sql.Tx, id uuid.UUID, loadPayload bool) (*store.CronJobMutableState, error) {
	q := `SELECT enabled, schedule_kind, cron_expression, run_at, timezone, interval_ms, next_run_at, payload
		FROM cron_jobs WHERE id = $1`
	args := []any{id}

	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return nil, fmt.Errorf("tenant_id required")
		}
		q += fmt.Sprintf(" AND tenant_id = $%d", len(args)+1)
		args = append(args, tid)
	}
	q += " FOR UPDATE"

	var (
		state        store.CronJobMutableState
		scheduleKind string
		cronExpr     *string
		runAt        *time.Time
		tz           *string
		intervalMS   *int64
		nextRunAt    *time.Time
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
	if runAt != nil {
		ms := runAt.UnixMilli()
		state.Schedule.AtMS = &ms
	}
	if tz != nil {
		state.Schedule.TZ = *tz
	}
	if intervalMS != nil {
		state.Schedule.EveryMS = intervalMS
	}
	state.NextRunAt = nextRunAt

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
	for idx, col := range store.SortedUpdateColumns(updates) {
		if !validColumnName.MatchString(col) {
			return fmt.Errorf("invalid column name: %q", col)
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", col, idx+1))
		args = append(args, updates[col])
	}

	args = append(args, id)
	q := fmt.Sprintf("UPDATE cron_jobs SET %s WHERE id = $%d", strings.Join(setClauses, ", "), len(args))
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return fmt.Errorf("tenant_id required")
		}
		args = append(args, tid)
		q += fmt.Sprintf(" AND tenant_id = $%d", len(args))
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

