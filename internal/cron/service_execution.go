package cron

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/adhocore/gronx"

	"github.com/nextlevelbuilder/goclaw/internal/safego"
)

// RunJob manually triggers a job execution.
// mode: "force" = run regardless of schedule, "due" = only run if due.
// Returns (ran bool, reason string, error).
func (cs *Service) RunJob(jobID string, force bool) (bool, string, error) {
	cs.mu.Lock()

	var job *Job
	for i := range cs.store.Jobs {
		if cs.store.Jobs[i].ID == jobID {
			j := cs.store.Jobs[i] // copy
			job = &j
			break
		}
	}
	handler := cs.onJob
	cs.mu.Unlock()

	if job == nil {
		return false, "", fmt.Errorf("job %s not found", jobID)
	}
	if handler == nil {
		return false, "", fmt.Errorf("no job handler configured")
	}

	if !force {
		// Check if job is due
		if job.State.NextRunAtMS == nil || *job.State.NextRunAtMS > nowMS() {
			return false, "not-due", nil
		}
	}

	// Execute outside lock with retry
	slog.Info("cron manual run", "id", job.ID, "name", job.Name, "force", force)
	result, _, err := ExecuteWithRetry(func() (string, error) {
		return handler(job)
	}, cs.retryCfg)

	// Update state
	cs.mu.Lock()
	defer cs.mu.Unlock()

	for i := range cs.store.Jobs {
		if cs.store.Jobs[i].ID != jobID {
			continue
		}
		now := nowMS()
		cs.store.Jobs[i].State.LastRunAtMS = &now
		if err != nil {
			cs.store.Jobs[i].State.LastStatus = "error"
			cs.store.Jobs[i].State.LastError = err.Error()
		} else {
			cs.store.Jobs[i].State.LastStatus = "ok"
			cs.store.Jobs[i].State.LastError = ""
		}

		// Recompute next run (unless one-time and delete after run)
		if cs.store.Jobs[i].DeleteAfterRun {
			cs.store.Jobs = append(cs.store.Jobs[:i], cs.store.Jobs[i+1:]...)
		} else {
			next := cs.computeNextRun(&cs.store.Jobs[i].Schedule, now)
			cs.store.Jobs[i].State.NextRunAtMS = next
		}
		cs.saveUnsafe()
		break
	}

	// Record run log (already holding cs.mu via defer above)
	cs.recordRunLocked(jobID, err, result)

	if err != nil {
		return true, "", err
	}
	return true, result, nil
}

// GetRunLog returns recent run log entries for a job (or all jobs if jobID is empty).
func (cs *Service) GetRunLog(jobID string, limit int) []RunLogEntry {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if limit <= 0 {
		limit = 20
	}

	var result []RunLogEntry
	for i := len(cs.runLog) - 1; i >= 0 && len(result) < limit; i-- {
		entry := cs.runLog[i]
		if jobID == "" || entry.JobID == jobID {
			result = append(result, entry)
		}
	}
	return result
}

func (cs *Service) recordRun(jobID string, err error, resultText string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.recordRunLocked(jobID, err, resultText)
}

// recordRunLocked appends a run log entry. Must be called with cs.mu held.
func (cs *Service) recordRunLocked(jobID string, err error, resultText string) {
	entry := RunLogEntry{
		Ts:    nowMS(),
		JobID: jobID,
	}
	if err != nil {
		entry.Status = "error"
		entry.Error = err.Error()
	} else {
		entry.Status = "ok"
		entry.Summary = TruncateOutput(resultText)
	}

	cs.runLog = append(cs.runLog, entry)
	// Keep last 200 entries in memory
	if len(cs.runLog) > 200 {
		cs.runLog = cs.runLog[len(cs.runLog)-200:]
	}
}

// --- Internal scheduling loop ---

func (cs *Service) runLoop(stopChan chan struct{}) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stopChan:
			return
		case <-ticker.C:
			cs.checkJobs()
		}
	}
}

func (cs *Service) checkJobs() {
	cs.mu.Lock()

	now := nowMS()

	// Collect due jobs and preserve their original scheduled times.
	// The scheduled time is used as anchor for "every" jobs to prevent drift.
	type dueJob struct {
		id            string
		scheduledAtMS int64
	}
	var dueJobs []dueJob

	for i := range cs.store.Jobs {
		job := &cs.store.Jobs[i]
		if job.Enabled && job.State.NextRunAtMS != nil && *job.State.NextRunAtMS <= now {
			dueJobs = append(dueJobs, dueJob{id: job.ID, scheduledAtMS: *job.State.NextRunAtMS})
		}
	}

	if len(dueJobs) == 0 {
		cs.mu.Unlock()
		return
	}

	// Clear NextRunAtMS to prevent duplicate execution
	dueMap := make(map[string]bool, len(dueJobs))
	for _, dj := range dueJobs {
		dueMap[dj.id] = true
	}
	for i := range cs.store.Jobs {
		if dueMap[cs.store.Jobs[i].ID] {
			cs.store.Jobs[i].State.NextRunAtMS = nil
		}
	}
	cs.saveUnsafe()
	cs.mu.Unlock()

	// Execute jobs in parallel — scheduler enforces per-session serialization
	var wg sync.WaitGroup
	for _, dj := range dueJobs {
		wg.Add(1)
		go func(id string, scheduledAtMS int64) {
			defer wg.Done()
			defer safego.Recover(nil, "job_id", id)
			cs.executeJobByID(id, scheduledAtMS)
		}(dj.id, dj.scheduledAtMS)
	}
	wg.Wait()
}

func (cs *Service) executeJobByID(jobID string, scheduledAtMS int64) {
	cs.mu.Lock()
	var job *Job
	for i := range cs.store.Jobs {
		if cs.store.Jobs[i].ID == jobID {
			j := cs.store.Jobs[i] // copy
			job = &j
			break
		}
	}
	handler := cs.onJob
	cs.mu.Unlock()

	if job == nil || handler == nil {
		return
	}

	slog.Info("cron executing job", "id", job.ID, "name", job.Name)

	result, attempts, err := ExecuteWithRetry(func() (string, error) {
		return handler(job)
	}, cs.retryCfg)

	if attempts > 1 {
		slog.Info("cron job retried", "id", job.ID, "attempts", attempts, "success", err == nil)
	}

	cs.mu.Lock()
	defer cs.mu.Unlock()

	for i := range cs.store.Jobs {
		if cs.store.Jobs[i].ID != jobID {
			continue
		}

		now := nowMS()
		cs.store.Jobs[i].State.LastRunAtMS = &now

		if err != nil {
			cs.store.Jobs[i].State.LastStatus = "error"
			cs.store.Jobs[i].State.LastError = err.Error()
			slog.Error("cron job failed", "id", jobID, "error", err)
		} else {
			cs.store.Jobs[i].State.LastStatus = "ok"
			cs.store.Jobs[i].State.LastError = ""
			slog.Info("cron job completed", "id", jobID, "result", result)
		}

		// Schedule next run or handle one-time jobs
		if cs.store.Jobs[i].DeleteAfterRun {
			cs.store.Jobs = append(cs.store.Jobs[:i], cs.store.Jobs[i+1:]...)
		} else {
			schedule := &cs.store.Jobs[i].Schedule
			// For "every" (interval) jobs, compute next run from the original
			// scheduled time (anchor) to prevent drift and synchronization.
			if schedule.Kind == "every" && schedule.EveryMS != nil && *schedule.EveryMS > 0 && scheduledAtMS > 0 {
				interval := *schedule.EveryMS
				// O(1) advance to the next future slot from anchor
				elapsed := now - scheduledAtMS
				periods := elapsed / interval
				next := scheduledAtMS + (periods+1)*interval
				cs.store.Jobs[i].State.NextRunAtMS = &next
			} else {
				next := cs.computeNextRun(schedule, now)
				cs.store.Jobs[i].State.NextRunAtMS = next
				if next == nil {
					cs.store.Jobs[i].Enabled = false
				}
			}
		}
		break
	}

	// Record run log (already holding cs.mu via defer above)
	cs.recordRunLocked(jobID, err, result)

	cs.saveUnsafe()
}

// --- Schedule computation ---

func (cs *Service) computeNextRun(schedule *Schedule, now int64) *int64 {
	switch schedule.Kind {
	case "at":
		if schedule.AtMS != nil && *schedule.AtMS > now {
			return schedule.AtMS
		}
		return nil

	case "every":
		if schedule.EveryMS == nil || *schedule.EveryMS <= 0 {
			return nil
		}
		next := now + *schedule.EveryMS
		return &next

	case "cron":
		if schedule.Expr == "" {
			return nil
		}
		nowTime := time.UnixMilli(now)
		if schedule.TZ != "" {
			if loc, err := time.LoadLocation(schedule.TZ); err == nil {
				nowTime = nowTime.In(loc)
			}
		}
		nextTime, err := gronx.NextTickAfter(schedule.Expr, nowTime, false)
		if err != nil {
			slog.Error("cron: failed to compute next run", "expr", schedule.Expr, "error", err)
			return nil
		}
		nextMS := nextTime.UnixMilli()
		return &nextMS

	default:
		return nil
	}
}

func (cs *Service) validateSchedule(schedule *Schedule) error {
	switch schedule.Kind {
	case "at":
		if schedule.AtMS == nil {
			return fmt.Errorf("at schedule requires atMs")
		}
	case "every":
		if schedule.EveryMS == nil || *schedule.EveryMS <= 0 {
			return fmt.Errorf("every schedule requires positive everyMs")
		}
	case "cron":
		if schedule.Expr == "" {
			return fmt.Errorf("cron schedule requires expr")
		}
		gx := gronx.New()
		if !gx.IsValid(schedule.Expr) {
			return fmt.Errorf("invalid cron expression: %s", schedule.Expr)
		}
		if schedule.TZ != "" {
			if _, err := time.LoadLocation(schedule.TZ); err != nil {
				return fmt.Errorf("invalid timezone: %s", schedule.TZ)
			}
		}
	default:
		return fmt.Errorf("unknown schedule kind: %s", schedule.Kind)
	}
	return nil
}

func (cs *Service) getNextWakeMS() *int64 {
	var earliest *int64
	for _, job := range cs.store.Jobs {
		if job.Enabled && job.State.NextRunAtMS != nil {
			if earliest == nil || *job.State.NextRunAtMS < *earliest {
				earliest = job.State.NextRunAtMS
			}
		}
	}
	return earliest
}

// --- Persistence ---

func (cs *Service) loadUnsafe() error {
	data, err := os.ReadFile(cs.storePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return json.Unmarshal(data, &cs.store)
}

func (cs *Service) saveUnsafe() error {
	dir := filepath.Dir(cs.storePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cs.store, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cs.storePath, data, 0644)
}
