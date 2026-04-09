//go:build sqlite || sqliteonly

package sqlitestore

import (
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/cron"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (s *SQLiteCronStore) GetDueJobs(now time.Time) []store.CronJob {
	s.mu.Lock()
	if !s.cacheLoaded || time.Since(s.cacheTime) > s.cacheTTL {
		s.refreshJobCache()
	}
	jobs := s.jobCache
	s.mu.Unlock()

	var due []store.CronJob
	for i := range jobs {
		if jobs[i].Enabled && jobs[i].State.NextRunAtMS != nil {
			nextRun := time.UnixMilli(*jobs[i].State.NextRunAtMS)
			if !nextRun.After(now) {
				due = append(due, jobs[i])
			}
		}
	}
	return due
}

// refreshJobCache reloads all enabled jobs from DB. Must be called with mu held.
func (s *SQLiteCronStore) refreshJobCache() {
	rows, err := s.db.QueryContext(s.baseCtx,
		`SELECT id, tenant_id, agent_id, user_id, name, enabled, schedule_kind, cron_expression, run_at, timezone,
		 interval_ms, payload, delete_after_run, stateless, deliver, deliver_channel, deliver_to, wake_heartbeat,
		 next_run_at, last_run_at, last_status, last_error,
		 created_at, updated_at FROM cron_jobs WHERE enabled = 1`)
	if err != nil {
		return
	}
	defer rows.Close()

	s.jobCache = nil
	for rows.Next() {
		job, err := scanCronRow(rows)
		if err != nil {
			continue
		}
		s.jobCache = append(s.jobCache, *job)
	}
	if err := rows.Err(); err != nil {
		slog.Warn("cron: cache refresh iteration error", "error", err)
	}
	s.cacheLoaded = true
	s.cacheTime = time.Now()
}

// InvalidateCache forces a cache refresh on the next GetDueJobs call.
func (s *SQLiteCronStore) InvalidateCache() {
	s.mu.Lock()
	s.cacheLoaded = false
	s.mu.Unlock()
}

// recomputeStaleJobs fixes enabled jobs on startup:
//   - Recomputes next_run_at for jobs where it is NULL (crashed mid-execution).
//   - Advances past-due jobs (next_run_at < now) to their next future run time,
//     preventing a flood of all missed jobs firing simultaneously after downtime.
//
// NOTE: After prolonged downtime, all past-due "every" jobs with the same interval
// will synchronize (all get next_run_at = now + interval). This is inherent because
// the original schedule anchor is not persisted. After the first execution cycle,
// anchor-based scheduling in executeOneJob preserves spacing going forward.
func (s *SQLiteCronStore) recomputeStaleJobs() {
	// Reset stale 'running' status — jobs that were mid-execution when the app
	// crashed will never self-recover, so mark them as interrupted on startup.
	if res, err := s.db.ExecContext(s.baseCtx,
		`UPDATE cron_jobs SET last_status = 'interrupted' WHERE last_status = 'running'`); err != nil {
		slog.Warn("cron: failed to reset stale running jobs on startup", "error", err)
	} else if n, _ := res.RowsAffected(); n > 0 {
		slog.Info("cron: reset stale running jobs to interrupted", "count", n)
	}

	now := time.Now()
	rows, err := s.db.QueryContext(s.baseCtx,
		`SELECT id, schedule_kind, cron_expression, run_at, timezone, interval_ms
		 FROM cron_jobs WHERE enabled = 1 AND (next_run_at IS NULL OR next_run_at < ?)`, now)
	if err != nil {
		slog.Warn("cron: failed to query stale jobs", "error", err)
		return
	}
	defer rows.Close()

	var fixed int
	for rows.Next() {
		var id uuid.UUID
		var scheduleKind string
		var cronExpr, tz *string
		var runAt *time.Time
		var intervalMS *int64

		if err := rows.Scan(&id, &scheduleKind, &cronExpr, &runAt, &tz, &intervalMS); err != nil {
			continue
		}

		schedule := store.CronSchedule{Kind: scheduleKind}
		if cronExpr != nil {
			schedule.Expr = *cronExpr
		}
		if runAt != nil {
			ms := runAt.UnixMilli()
			schedule.AtMS = &ms
		}
		if intervalMS != nil {
			schedule.EveryMS = intervalMS
		}
		if tz != nil {
			schedule.TZ = *tz
		}

		next := computeNextRun(&schedule, now, s.defaultTZ)
		if next == nil {
			if scheduleKind == "at" {
				s.db.ExecContext(s.baseCtx, "UPDATE cron_jobs SET enabled = 0, updated_at = ? WHERE id = ?", now, id)
			}
			continue
		}

		s.db.ExecContext(s.baseCtx, "UPDATE cron_jobs SET next_run_at = ?, updated_at = ? WHERE id = ?", *next, now, id)
		fixed++
	}
	if err := rows.Err(); err != nil {
		slog.Warn("cron: recompute stale iteration error", "error", err)
	}

	if fixed > 0 {
		slog.Info("cron: advanced stale/past-due jobs to next future run", "fixed", fixed)
	}
}

func (s *SQLiteCronStore) runLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.checkAndRunDueJobs()
		}
	}
}

func (s *SQLiteCronStore) checkAndRunDueJobs() {
	dueJobs := s.GetDueJobs(time.Now())
	if len(dueJobs) == 0 {
		return
	}

	s.mu.Lock()
	handler := s.onJob
	s.mu.Unlock()

	if handler == nil {
		return
	}

	now := time.Now()
	var claimedJobs []store.CronJob
	for _, job := range dueJobs {
		if id, parseErr := uuid.Parse(job.ID); parseErr == nil && s.claimDueJob(id, now) {
			claimedJobs = append(claimedJobs, job)
		}
	}
	if len(claimedJobs) == 0 {
		return
	}

	// Execute jobs in parallel — scheduler enforces per-session serialization.
	var wg sync.WaitGroup
	for _, job := range claimedJobs {
		wg.Add(1)
		go func(job store.CronJob) {
			defer wg.Done()
			s.executeOneJob(job, handler, true)
		}(job)
	}
	wg.Wait()

	s.mu.Lock()
	s.cacheLoaded = false
	s.mu.Unlock()
}

// executeOneJob runs a claimed job. When reloadClaimed is true (scheduler path),
// it re-reads the job from DB to verify claim invariants (enabled + next_run_at IS NULL).
// When false (manual RunJob path), it uses the already-loaded job directly.
func (s *SQLiteCronStore) executeOneJob(job store.CronJob, handler func(job *store.CronJob) (*store.CronJobResult, error), reloadClaimed bool) {
	// Preserve the original scheduled time before reload clears it.
	// For "every" jobs, this anchor is used to compute the next run from the
	// intended schedule time (not "now"), preventing drift and synchronization.
	scheduledAtMS := job.State.NextRunAtMS

	// For manual runs (reloadClaimed=false), don't use anchor — manual triggers
	// should reset the schedule to now + interval, not preserve the original offset.
	if !reloadClaimed {
		scheduledAtMS = nil
	}

	if reloadClaimed {
		if id, parseErr := uuid.Parse(job.ID); parseErr == nil {
			freshJob, ok := s.loadClaimedJob(id)
			if !ok {
				slog.Info("cron job skipped after claim state changed", "id", job.ID)
				return
			}
			job = *freshJob
		}
	}

	startTime := time.Now()

	var lastResult *store.CronJobResult
	resultStr, attempts, err := cron.ExecuteWithRetry(func() (string, error) {
		r, e := handler(&job)
		if e != nil {
			return "", e
		}
		lastResult = r
		if r != nil {
			return r.Content, nil
		}
		return "", nil
	}, s.retryCfg)

	durationMS := time.Since(startTime).Milliseconds()

	if attempts > 1 {
		slog.Info("cron job retried", "id", job.ID, "attempts", attempts, "success", err == nil)
	}

	now := time.Now()
	status := "ok"
	var lastError *string
	if err != nil {
		status = "error"
		errStr := err.Error()
		lastError = &errStr
	}

	var inputTokens, outputTokens int
	if lastResult != nil {
		inputTokens = lastResult.InputTokens
		outputTokens = lastResult.OutputTokens
	}

	logID := uuid.Must(uuid.NewV7())
	var summary *string
	if err == nil {
		truncated := cron.TruncateOutput(resultStr)
		summary = &truncated
	}
	if id, parseErr := uuid.Parse(job.ID); parseErr == nil {
		var agentUUID *uuid.UUID
		if aid, aidErr := uuid.Parse(job.AgentID); aidErr == nil {
			agentUUID = &aid
		}
		s.db.ExecContext(s.baseCtx,
			`INSERT INTO cron_run_logs (id, job_id, agent_id, status, error, summary, duration_ms, input_tokens, output_tokens, ran_at)
			 VALUES (?,?,?,?,?,?,?,?,?,?)`,
			logID, id, agentUUID, status, lastError, summary, durationMS, inputTokens, outputTokens, now,
		)
	}

	if job.DeleteAfterRun {
		if id, parseErr := uuid.Parse(job.ID); parseErr == nil {
			s.db.ExecContext(s.baseCtx, "DELETE FROM cron_jobs WHERE id = ?", id)
		}
	} else if id, parseErr := uuid.Parse(job.ID); parseErr == nil {
		schedule := job.Schedule
		var nextRunValue any

		// For "every" (interval) jobs, compute next run from the original scheduled
		// time (anchor) instead of "now". This prevents drift and synchronization
		// of interval-based jobs after server restarts.
		if schedule.Kind == "every" && scheduledAtMS != nil && schedule.EveryMS != nil && *schedule.EveryMS > 0 {
			anchor := time.UnixMilli(*scheduledAtMS)
			interval := time.Duration(*schedule.EveryMS) * time.Millisecond
			// O(1) advance to the next future slot from anchor
			elapsed := now.Sub(anchor)
			periods := int64(elapsed / interval)
			next := anchor.Add(interval * time.Duration(periods+1))
			nextRunValue = next
		} else {
			next := computeNextRun(&schedule, now, s.defaultTZ)
			if next != nil {
				nextRunValue = *next
			}
		}

		s.db.ExecContext(s.baseCtx,
			`UPDATE cron_jobs SET
			 last_run_at = ?, last_status = ?, last_error = ?, updated_at = ?,
			 next_run_at = CASE WHEN enabled = 1 AND next_run_at IS NULL THEN ? ELSE next_run_at END
			 WHERE id = ?`,
			now, status, lastError, now, nextRunValue, id,
		)
	}

	evt := store.CronEvent{Action: "completed", JobID: job.ID, JobName: job.Name, UserID: job.UserID, Status: status}
	if err != nil {
		evt.Action = "error"
		evt.Error = err.Error()
	}
	s.emitEvent(evt)
}

func (s *SQLiteCronStore) claimDueJob(id uuid.UUID, now time.Time) bool {
	res, err := s.db.ExecContext(
		s.baseCtx,
		`UPDATE cron_jobs
		 SET next_run_at = NULL
		 WHERE id = ? AND enabled = 1 AND next_run_at IS NOT NULL AND next_run_at <= ?`,
		id,
		now,
	)
	if err != nil {
		slog.Warn("cron: failed to claim due job", "id", id, "error", err)
		return false
	}

	n, _ := res.RowsAffected()
	return n == 1
}

func (s *SQLiteCronStore) loadClaimedJob(id uuid.UUID) (*store.CronJob, bool) {
	row := s.db.QueryRowContext(
		s.baseCtx,
		`SELECT id, tenant_id, agent_id, user_id, name, enabled, schedule_kind, cron_expression, run_at, timezone,
		 interval_ms, payload, delete_after_run, stateless, deliver, deliver_channel, deliver_to, wake_heartbeat,
		 next_run_at, last_run_at, last_status, last_error,
		 created_at, updated_at
		 FROM cron_jobs
		 WHERE id = ? AND enabled = 1 AND next_run_at IS NULL`,
		id,
	)
	job, err := scanCronRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false
	}
	if err != nil {
		slog.Warn("cron: failed to reload claimed job", "id", id, "error", err)
		return nil, false
	}
	return job, true
}
