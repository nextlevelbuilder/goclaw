package pg

import (
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/cron"
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
	rows, err := s.db.Query(
		`SELECT id, tenant_id, agent_id, user_id, name, enabled, schedule_kind, cron_expression, run_at, timezone,
		 interval_ms, payload, delete_after_run, next_run_at, last_run_at, last_status, last_error,
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

// recomputeStaleJobs fixes enabled jobs that have next_run_at = NULL.
// This happens when the gateway was stopped/crashed while a job was executing,
// or when the previously computed next_run_at was consumed but never recomputed.
func (s *PGCronStore) recomputeStaleJobs() {
	rows, err := s.db.Query(
		`SELECT id, schedule_kind, cron_expression, run_at, timezone, interval_ms
		 FROM cron_jobs WHERE enabled = true AND next_run_at IS NULL`)
	if err != nil {
		slog.Warn("cron: failed to query stale jobs", "error", err)
		return
	}
	defer rows.Close()

	now := time.Now()
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
				s.db.Exec("UPDATE cron_jobs SET enabled = false, updated_at = $1 WHERE id = $2", now, id)
			}
			continue
		}

		s.db.Exec("UPDATE cron_jobs SET next_run_at = $1, updated_at = $2 WHERE id = $3", *next, now, id)
		fixed++
	}

	if fixed > 0 {
		slog.Info("cron: recomputed stale next_run_at on startup", "fixed", fixed)
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
			s.checkAndRunDueJobs()
		}
	}
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

	// Clear next_run for all due jobs first to prevent duplicate fires
	for _, job := range dueJobs {
		if id, parseErr := uuid.Parse(job.ID); parseErr == nil {
			s.db.Exec("UPDATE cron_jobs SET next_run_at = NULL WHERE id = $1", id)
		}
	}

	// Execute jobs in parallel — scheduler enforces per-session serialization
	var wg sync.WaitGroup
	for _, job := range dueJobs {
		wg.Add(1)
		go func(job store.CronJob) {
			defer wg.Done()
			s.executeOneJob(job, handler)
		}(job)
	}
	wg.Wait()

	// Invalidate cache after job execution changed next_run_at values
	s.mu.Lock()
	s.cacheLoaded = false
	s.mu.Unlock()
}

// executeOneJob runs a single cron job with retry, logs the result, and updates next_run_at.
func (s *PGCronStore) executeOneJob(job store.CronJob, handler func(job *store.CronJob) (*store.CronJobResult, error)) {
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
		s.db.Exec(
			`INSERT INTO cron_run_logs (id, job_id, agent_id, status, error, summary, duration_ms, input_tokens, output_tokens, ran_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			logID, id, agentUUID, status, lastError, summary, durationMS, inputTokens, outputTokens, now,
		)
	}

	// Recompute next run or delete
	if job.DeleteAfterRun {
		if id, parseErr := uuid.Parse(job.ID); parseErr == nil {
			s.db.Exec("DELETE FROM cron_jobs WHERE id = $1", id)
		}
	} else if id, parseErr := uuid.Parse(job.ID); parseErr == nil {
		schedule := job.Schedule
		next := computeNextRun(&schedule, now, s.defaultTZ)
		s.db.Exec(
			"UPDATE cron_jobs SET last_run_at = $1, last_status = $2, last_error = $3, next_run_at = $4, updated_at = $5 WHERE id = $6",
			now, status, lastError, next, now, id,
		)
	}

	// Emit completion event
	evt := store.CronEvent{Action: "completed", JobID: job.ID, JobName: job.Name, UserID: job.UserID, Status: status}
	if err != nil {
		evt.Action = "error"
		evt.Error = err.Error()
	}
	s.emitEvent(evt)
}
