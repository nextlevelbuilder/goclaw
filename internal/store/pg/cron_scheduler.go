package pg

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/cron"
	"github.com/nextlevelbuilder/goclaw/internal/safego"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (s *PGCronStore) GetDueJobs(now time.Time) []store.CronJob {
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
func (s *PGCronStore) refreshJobCache() {
	rows, err := s.db.QueryContext(s.baseCtx,
		`SELECT id, tenant_id, agent_id, user_id, name, enabled, schedule_kind, cron_expression, run_at, timezone,
		 interval_ms, payload, delete_after_run, stateless, deliver, deliver_channel, deliver_to, wake_heartbeat,
		 next_run_at, last_run_at, last_status, last_error,
		 created_at, updated_at FROM cron_jobs WHERE enabled = true`)
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
	s.cacheLoaded = true
	s.cacheTime = time.Now()
}

// InvalidateCache forces a cache refresh on the next GetDueJobs call.
func (s *PGCronStore) InvalidateCache() {
	s.mu.Lock()
	s.cacheLoaded = false
	s.mu.Unlock()
}

// recomputeStaleJobs fixes enabled jobs on startup:
//   - Resets any jobs stuck in 'running' state from a previous crash.
//   - Recomputes next_run_at for jobs where it is NULL (crashed mid-execution).
//   - Advances past-due jobs (next_run_at < now) to their next future run time,
//     preventing a flood of all missed jobs firing simultaneously after downtime.
func (s *PGCronStore) recomputeStaleJobs() {
	// Reset stale 'running' status — jobs that were mid-execution when the server
	// crashed will never self-recover, so mark them as interrupted on startup.
	if res, err := s.db.ExecContext(s.baseCtx,
		`UPDATE cron_jobs SET last_status = 'interrupted' WHERE last_status = 'running'`); err != nil {
		slog.Warn("cron: failed to reset stale running jobs on startup", "error", err)
	} else if n, _ := res.RowsAffected(); n > 0 {
		slog.Info("cron: reset stale running jobs to interrupted", "count", n)
	}

	// Fix jobs with NULL next_run_at OR past-due next_run_at.
	// Past-due jobs happen when the server was down and their scheduled time passed.
	// Without this, ALL past-due jobs would fire simultaneously on the first tick.
	// NOTE: After prolonged downtime, all past-due "every" jobs with the same interval
	// will synchronize (all get next_run_at = now + interval). This is inherent because
	// the original schedule anchor is not persisted. After the first execution cycle,
	// anchor-based scheduling in executeOneJob preserves spacing going forward.
	now := time.Now()
	rows, err := s.db.QueryContext(s.baseCtx,
		`SELECT id, schedule_kind, cron_expression, run_at, timezone, interval_ms
		 FROM cron_jobs WHERE enabled = true AND (next_run_at IS NULL OR next_run_at < $1)`, now)
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
				if _, err := s.db.ExecContext(s.baseCtx, "UPDATE cron_jobs SET enabled = false, updated_at = $1 WHERE id = $2", now, id); err != nil {
					slog.Warn("cron: failed to disable one-shot job", "id", id, "error", err)
				}
			}
			continue
		}

		if _, err := s.db.ExecContext(s.baseCtx, "UPDATE cron_jobs SET next_run_at = $1, updated_at = $2 WHERE id = $3", *next, now, id); err != nil {
			slog.Warn("cron: failed to advance stale job", "id", id, "error", err)
		}
		fixed++
	}
	if err := rows.Err(); err != nil {
		slog.Warn("cron: recompute stale iteration error", "error", err)
	}

	if fixed > 0 {
		slog.Info("cron: advanced stale/past-due jobs to next future run", "fixed", fixed)
	}
}

func (s *PGCronStore) runLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.safeCheckAndRunDueJobs()
		}
	}
}

// safeCheckAndRunDueJobs wraps checkAndRunDueJobs with panic recovery
// so a panic in any check/claim logic doesn't kill the runLoop goroutine.
func (s *PGCronStore) safeCheckAndRunDueJobs() {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("cron: checkAndRunDueJobs panicked — runLoop continues", "panic", fmt.Sprint(r))
		}
	}()
	s.checkAndRunDueJobs()
}

func (s *PGCronStore) checkAndRunDueJobs() {
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

	// Execute jobs in parallel without blocking the runLoop.
	// Previously wg.Wait() blocked here — if any job hung (e.g. LLM timeout,
	// agent loop stuck), the entire cron scheduler would stop checking for new
	// due jobs. Now each job runs independently; cache is invalidated per-job.
	for _, job := range claimedJobs {
		go func(job store.CronJob) {
			defer safego.Recover(nil, "component", "cron_job", "job_id", job.ID, "job_name", job.Name)
			defer s.InvalidateCache()
			s.executeOneJob(job, handler, true)
		}(job)
	}
}

// executeOneJob runs a single cron job with retry, logs the result, and updates next_run_at.
// executeOneJob runs a claimed job. When reloadClaimed is true (scheduler path),
// it re-reads the job from DB to verify claim invariants (enabled + next_run_at IS NULL).
// When false (manual RunJob path), it uses the already-loaded job directly —
// skipping the reload avoids the enabled=true filter that would reject disabled jobs.
func (s *PGCronStore) executeOneJob(job store.CronJob, handler func(job *store.CronJob) (*store.CronJobResult, error), reloadClaimed bool) {
	// Preserve the original scheduled time before reload clears it.
	// For "every" jobs, this anchor is used to compute the next run from the
	// intended schedule time (not "now"), preventing drift and synchronization
	// of interval-based jobs after server restarts.
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

	// Wrap handler to fit ExecuteWithRetry's (string, error) signature
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

	// Extract token usage from handler result
	var inputTokens, outputTokens int
	if lastResult != nil {
		inputTokens = lastResult.InputTokens
		outputTokens = lastResult.OutputTokens
	}

	// Log run
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
		if _, err := s.db.ExecContext(s.baseCtx,
			`INSERT INTO cron_run_logs (id, job_id, agent_id, status, error, summary, duration_ms, input_tokens, output_tokens, ran_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			logID, id, agentUUID, status, lastError, summary, durationMS, inputTokens, outputTokens, now,
		); err != nil {
			slog.Warn("cron: failed to insert run log", "job_id", job.ID, "error", err)
		}
	}

	// Recompute next run or delete
	if job.DeleteAfterRun {
		if id, parseErr := uuid.Parse(job.ID); parseErr == nil {
			if _, err := s.db.ExecContext(s.baseCtx, "DELETE FROM cron_jobs WHERE id = $1", id); err != nil {
				slog.Warn("cron: failed to delete one-shot job", "job_id", job.ID, "error", err)
			}
		}
	} else if id, parseErr := uuid.Parse(job.ID); parseErr == nil {
		schedule := job.Schedule
		var nextRunValue any

		// For "every" (interval) jobs, compute next run from the original scheduled
		// time (anchor) instead of "now". This prevents:
		//  1. Drift: interval is always exact, not interval + execution_time
		//  2. Synchronization: after restart, jobs that started at different offsets
		//     keep their original spacing instead of clustering together
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

		if _, err := s.db.ExecContext(s.baseCtx,
			`UPDATE cron_jobs SET
			 last_run_at = $1, last_status = $2, last_error = $3, updated_at = $4,
			 next_run_at = CASE WHEN enabled = true AND next_run_at IS NULL THEN $5 ELSE next_run_at END
			 WHERE id = $6`,
			now, status, lastError, now, nextRunValue, id,
		); err != nil {
			slog.Warn("cron: failed to update job after run", "job_id", job.ID, "error", err)
		}
	}

	// Emit completion event
	evt := store.CronEvent{Action: "completed", JobID: job.ID, JobName: job.Name, UserID: job.UserID, Status: status}
	if err != nil {
		evt.Action = "error"
		evt.Error = err.Error()
	}
	s.emitEvent(evt)
}

func (s *PGCronStore) claimDueJob(id uuid.UUID, now time.Time) bool {
	res, err := s.db.ExecContext(
		s.baseCtx,
		`UPDATE cron_jobs
		 SET next_run_at = NULL
		 WHERE id = $1 AND enabled = true AND next_run_at IS NOT NULL AND next_run_at <= $2`,
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

func (s *PGCronStore) loadClaimedJob(id uuid.UUID) (*store.CronJob, bool) {
	row := s.db.QueryRowContext(
		s.baseCtx,
		`SELECT id, tenant_id, agent_id, user_id, name, enabled, schedule_kind, cron_expression, run_at, timezone,
		 interval_ms, payload, delete_after_run, stateless, deliver, deliver_channel, deliver_to, wake_heartbeat,
		 next_run_at, last_run_at, last_status, last_error,
		 created_at, updated_at
		 FROM cron_jobs
		 WHERE id = $1 AND enabled = true AND next_run_at IS NULL`,
		id,
	)
	job, err := scanCronSingleRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false
	}
	if err != nil {
		slog.Warn("cron: failed to reload claimed job", "id", id, "error", err)
		return nil, false
	}
	return job, true
}
