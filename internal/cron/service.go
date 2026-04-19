package cron

import (
	"fmt"
	"log/slog"
	"sync"
)

// Service manages cron jobs with persistence, scheduling, and execution.
type Service struct {
	storePath string
	store     Store
	onJob     JobHandler
	running   bool
	stopChan  chan struct{}
	mu        sync.Mutex
	runLog    []RunLogEntry // in-memory run history (last 200 entries)
	retryCfg  RetryConfig   // retry config for failed jobs
}

// NewService creates a new cron service.
// storePath is the path to the JSON file for job persistence.
// onJob is the callback invoked when a job fires (can be set later via SetOnJob).
func NewService(storePath string, onJob JobHandler) *Service {
	return &Service{
		storePath: storePath,
		store:     Store{Version: 1},
		onJob:     onJob,
		retryCfg:  DefaultRetryConfig(),
	}
}

// SetRetryConfig overrides the default retry configuration.
func (cs *Service) SetRetryConfig(cfg RetryConfig) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.retryCfg = cfg
}

// SetOnJob sets the job execution callback.
func (cs *Service) SetOnJob(handler JobHandler) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.onJob = handler
}

// Start loads persisted jobs and begins the scheduling loop.
func (cs *Service) Start() error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if cs.running {
		return nil
	}

	if err := cs.loadUnsafe(); err != nil {
		slog.Warn("cron: failed to load store, starting fresh", "error", err)
		cs.store = Store{Version: 1}
	}

	// Compute next runs for all enabled jobs: fix NULL and past-due next_run_at.
	// Past-due jobs happen when the service was stopped and their scheduled time passed.
	// Without this, ALL past-due jobs would fire simultaneously on the first tick.
	// NOTE: After prolonged downtime, all past-due "every" jobs with the same interval
	// will synchronize (all get next_run_at = now + interval). This is inherent because
	// the original schedule anchor is not persisted. After the first execution cycle,
	// anchor-based scheduling in executeJobByID preserves spacing going forward.
	now := nowMS()
	for i := range cs.store.Jobs {
		job := &cs.store.Jobs[i]
		if job.Enabled && (job.State.NextRunAtMS == nil || *job.State.NextRunAtMS <= now) {
			next := cs.computeNextRun(&job.Schedule, now)
			job.State.NextRunAtMS = next
			// Disable past-due one-time "at" jobs instead of leaving them as zombies
			if next == nil && job.Schedule.Kind == "at" {
				job.Enabled = false
			}
		}
	}
	if err := cs.saveUnsafe(); err != nil {
		slog.Warn("cron: failed to persist store on start", "error", err)
	}

	cs.stopChan = make(chan struct{})
	cs.running = true

	// Snapshot the tick interval before spawning so the goroutine doesn't
	// race with tests that mutate the package-level `runLoopTickInterval`
	// after a previous Stop() returned but the runLoop goroutine hasn't yet
	// executed its ticker construction.
	tick := runLoopTickInterval
	go cs.runLoop(cs.stopChan, tick)

	slog.Info("cron service started", "jobs", len(cs.store.Jobs))
	return nil
}

// Stop halts the scheduling loop.
func (cs *Service) Stop() {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if !cs.running {
		return
	}

	close(cs.stopChan)
	cs.running = false
	slog.Info("cron service stopped")
}

// AddJob creates and registers a new cron job.
func (cs *Service) AddJob(name string, schedule Schedule, message string, deliver bool, channel, to, agentID string) (*Job, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	// Validate schedule
	if err := cs.validateSchedule(&schedule); err != nil {
		return nil, fmt.Errorf("invalid schedule: %w", err)
	}

	now := nowMS()
	job := Job{
		ID:       generateID(),
		Name:     name,
		AgentID:  agentID,
		Enabled:  true,
		Schedule: schedule,
		Payload: Payload{
			Kind:    "agent_turn",
			Message: message,
		},
		Deliver:        deliver,
		DeliverChannel: channel,
		DeliverTo:      to,
		CreatedAtMS:    now,
		UpdatedAtMS:    now,
		DeleteAfterRun: schedule.Kind == "at",
	}

	next := cs.computeNextRun(&job.Schedule, now)
	job.State.NextRunAtMS = next

	cs.store.Jobs = append(cs.store.Jobs, job)
	if err := cs.saveUnsafe(); err != nil {
		slog.Warn("cron: failed to persist after adding job", "id", job.ID, "error", err)
	}

	slog.Info("cron job added", "id", job.ID, "name", name, "kind", schedule.Kind)
	return &job, nil
}

// RemoveJob deletes a job by ID.
func (cs *Service) RemoveJob(jobID string) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	for i, job := range cs.store.Jobs {
		if job.ID == jobID {
			cs.store.Jobs = append(cs.store.Jobs[:i], cs.store.Jobs[i+1:]...)
			if err := cs.saveUnsafe(); err != nil {
				slog.Warn("cron: failed to persist after removing job", "id", jobID, "error", err)
			}
			slog.Info("cron job removed", "id", jobID)
			return nil
		}
	}
	return fmt.Errorf("job %s not found", jobID)
}

// EnableJob toggles a job's enabled state.
func (cs *Service) EnableJob(jobID string, enabled bool) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	for i := range cs.store.Jobs {
		if cs.store.Jobs[i].ID == jobID {
			cs.store.Jobs[i].Enabled = enabled
			cs.store.Jobs[i].UpdatedAtMS = nowMS()
			if enabled {
				next := cs.computeNextRun(&cs.store.Jobs[i].Schedule, nowMS())
				cs.store.Jobs[i].State.NextRunAtMS = next
			} else {
				cs.store.Jobs[i].State.NextRunAtMS = nil
			}
			if err := cs.saveUnsafe(); err != nil {
				slog.Warn("cron: failed to persist after toggling job", "id", jobID, "error", err)
			}
			slog.Info("cron job toggled", "id", jobID, "enabled", enabled)
			return nil
		}
	}
	return fmt.Errorf("job %s not found", jobID)
}

// ListJobs returns all jobs, optionally including disabled ones.
func (cs *Service) ListJobs(includeDisabled bool) []Job {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	var result []Job
	for _, job := range cs.store.Jobs {
		if includeDisabled || job.Enabled {
			result = append(result, job)
		}
	}
	return result
}

// GetJob returns a job by ID.
func (cs *Service) GetJob(jobID string) (*Job, bool) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	for i, job := range cs.store.Jobs {
		if job.ID == jobID {
			return &cs.store.Jobs[i], true
		}
	}
	return nil, false
}

// UpdateJob patches an existing job's fields.
// Matching TS cron.update — only non-zero/non-nil fields are applied.
func (cs *Service) UpdateJob(jobID string, patch JobPatch) (*Job, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	for i := range cs.store.Jobs {
		if cs.store.Jobs[i].ID != jobID {
			continue
		}
		job := &cs.store.Jobs[i]

		if patch.Name != "" {
			job.Name = patch.Name
		}
		if patch.AgentID != nil {
			job.AgentID = *patch.AgentID
		}
		if patch.Enabled != nil {
			job.Enabled = *patch.Enabled
		}
		if patch.Schedule != nil {
			if err := cs.validateSchedule(patch.Schedule); err != nil {
				return nil, fmt.Errorf("invalid schedule: %w", err)
			}
			job.Schedule = *patch.Schedule
		}
		if patch.Message != "" {
			job.Payload.Message = patch.Message
		}
		if patch.Deliver != nil {
			job.Deliver = *patch.Deliver
		}
		if patch.DeliverChannel != nil {
			job.DeliverChannel = *patch.DeliverChannel
		}
		if patch.DeliverTo != nil {
			job.DeliverTo = *patch.DeliverTo
		}
		if patch.WakeHeartbeat != nil {
			job.WakeHeartbeat = *patch.WakeHeartbeat
		}
		if patch.Stateless != nil {
			job.Stateless = *patch.Stateless
		}
		if patch.DeleteAfterRun != nil {
			job.DeleteAfterRun = *patch.DeleteAfterRun
		}

		job.UpdatedAtMS = nowMS()

		// Recompute next run if schedule or enabled changed
		if job.Enabled {
			next := cs.computeNextRun(&job.Schedule, nowMS())
			job.State.NextRunAtMS = next
		} else {
			job.State.NextRunAtMS = nil
		}

		if err := cs.saveUnsafe(); err != nil {
			slog.Warn("cron: failed to persist after updating job", "id", jobID, "error", err)
		}
		slog.Info("cron job updated", "id", jobID)
		result := cs.store.Jobs[i] // copy
		return &result, nil
	}
	return nil, fmt.Errorf("job %s not found", jobID)
}

// Status returns the service status.
func (cs *Service) Status() map[string]any {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	return map[string]any{
		"enabled":      cs.running,
		"jobs":         len(cs.store.Jobs),
		"nextWakeAtMs": cs.getNextWakeMS(),
	}
}
