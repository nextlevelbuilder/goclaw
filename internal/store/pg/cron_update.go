package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type cronJobMutableState struct {
	enabled   bool
	schedule  store.CronSchedule
	nextRunAt *time.Time
	payload   store.CronPayload
}

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
	effectiveEnabled := current.enabled
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
		merged := store.MergeCronSchedule(current.schedule, patch.Schedule)
		if err := store.ValidateCronSchedule(&merged); err != nil {
			return nil, err
		}

		applyCronScheduleUpdates(updates, merged)

		nextRun, err := store.NextRunForSchedule(&merged, effectiveEnabled, now, s.defaultTZ)
		if err != nil {
			return nil, err
		}
		updates["next_run_at"] = nextRun
	} else if patch.Enabled != nil {
		nextRun, err := store.NextRunForToggle(&current.schedule, effectiveEnabled, current.enabled, current.nextRunAt, now, s.defaultTZ)
		if err != nil {
			return nil, err
		}
		updates["next_run_at"] = nextRun
	}

	needsPayloadUpdate := patch.Message != "" || patch.Deliver != nil || patch.Channel != nil || patch.To != nil || patch.WakeHeartbeat != nil
	if needsPayloadUpdate {
		payload := current.payload
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

func (s *PGCronStore) lockCronJobForMutation(ctx context.Context, tx *sql.Tx, id uuid.UUID, loadPayload bool) (*cronJobMutableState, error) {
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
		state        cronJobMutableState
		scheduleKind string
		cronExpr     *string
		runAt        *time.Time
		tz           *string
		intervalMS   *int64
		nextRunAt    *time.Time
		payloadJSON  []byte
	)

	if err := tx.QueryRowContext(ctx, q, args...).Scan(
		&state.enabled,
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

	state.schedule = store.CronSchedule{Kind: scheduleKind}
	if cronExpr != nil {
		state.schedule.Expr = *cronExpr
	}
	if runAt != nil {
		ms := runAt.UnixMilli()
		state.schedule.AtMS = &ms
	}
	if tz != nil {
		state.schedule.TZ = *tz
	}
	if intervalMS != nil {
		state.schedule.EveryMS = intervalMS
	}
	state.nextRunAt = nextRunAt

	if loadPayload && len(payloadJSON) > 0 {
		if err := json.Unmarshal(payloadJSON, &state.payload); err != nil {
			return nil, fmt.Errorf("failed to parse existing payload for job %s: %w", id, err)
		}
	}

	return &state, nil
}

func applyCronScheduleUpdates(updates map[string]any, schedule store.CronSchedule) {
	updates["schedule_kind"] = schedule.Kind

	switch schedule.Kind {
	case "cron":
		updates["cron_expression"] = schedule.Expr
		if schedule.TZ != "" {
			updates["timezone"] = schedule.TZ
		} else {
			updates["timezone"] = nil
		}
		updates["interval_ms"] = nil
		updates["run_at"] = nil
	case "every":
		updates["cron_expression"] = nil
		updates["timezone"] = nil
		updates["interval_ms"] = *schedule.EveryMS
		updates["run_at"] = nil
	case "at":
		runAt := time.UnixMilli(*schedule.AtMS)
		updates["cron_expression"] = nil
		updates["timezone"] = nil
		updates["interval_ms"] = nil
		updates["run_at"] = runAt
	}
}

func execCronJobUpdateTx(ctx context.Context, tx *sql.Tx, id uuid.UUID, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}

	var (
		setClauses []string
		args       []any
	)
	for idx, col := range sortedUpdateColumns(updates) {
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

func sortedUpdateColumns(updates map[string]any) []string {
	cols := make([]string, 0, len(updates))
	for col := range updates {
		cols = append(cols, col)
	}
	// The exact order is not important functionally, but keeping it stable
	// simplifies tests and makes SQL generation deterministic.
	sort.Strings(cols)
	return cols
}
