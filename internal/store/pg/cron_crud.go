package pg

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (s *PGCronStore) AddJob(name string, schedule store.CronSchedule, message string, deliver bool, channel, to, agentID, userID string) (*store.CronJob, error) {
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

	_, err := s.db.Exec(
		`INSERT INTO cron_jobs (id, agent_id, user_id, name, enabled, schedule_kind, cron_expression, run_at, timezone,
		 interval_ms, payload, delete_after_run, next_run_at, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, true, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		id, agentUUID, userIDPtr, name, scheduleKind, cronExpr, runAt, tz,
		intervalMS, payloadJSON, deleteAfterRun, nextRun, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("create cron job: %w", err)
	}

	s.cacheLoaded = false // invalidate cache

	job, _ := s.GetJob(id.String())
	return job, nil
}

func (s *PGCronStore) GetJob(jobID string) (*store.CronJob, bool) {
	id, err := uuid.Parse(jobID)
	if err != nil {
		return nil, false
	}
	job, err := s.scanJob(id)
	if err != nil {
		return nil, false
	}
	return job, true
}

func (s *PGCronStore) ListJobs(includeDisabled bool, agentID, userID string) []store.CronJob {
	q := `SELECT id, agent_id, user_id, name, enabled, schedule_kind, cron_expression, run_at, timezone,
		 interval_ms, payload, delete_after_run, next_run_at, last_run_at, last_status, last_error,
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

	q += " ORDER BY created_at DESC"

	rows, err := s.db.Query(q, args...)
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

func (s *PGCronStore) RemoveJob(jobID string) error {
	id, err := uuid.Parse(jobID)
	if err != nil {
		return fmt.Errorf("invalid job ID: %s", jobID)
	}
	_, err = s.db.Exec("DELETE FROM cron_jobs WHERE id = $1", id)
	if err != nil {
		return err
	}
	s.cacheLoaded = false
	return nil
}

func (s *PGCronStore) EnableJob(jobID string, enabled bool) error {
	id, err := uuid.Parse(jobID)
	if err != nil {
		return fmt.Errorf("invalid job ID: %s", jobID)
	}
	_, err = s.db.Exec("UPDATE cron_jobs SET enabled = $1, updated_at = $2 WHERE id = $3", enabled, time.Now(), id)
	if err != nil {
		return err
	}
	s.cacheLoaded = false
	return nil
}
