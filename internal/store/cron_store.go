package store

import (
	"context"
	"time"

	"github.com/adhocore/gronx"
	"github.com/google/uuid"
)

// CronJob represents a scheduled job.
type CronJob struct {
	ID             string       `json:"id"`
	TenantID       uuid.UUID    `json:"tenantId,omitempty"`
	Name           string       `json:"name"`
	AgentID        string       `json:"agentId,omitempty"`
	UserID         string       `json:"userId,omitempty"`
	Enabled        bool         `json:"enabled"`
	Schedule       CronSchedule `json:"schedule"`
	Payload        CronPayload  `json:"payload"`
	State          CronJobState `json:"state"`
	CreatedAtMS    int64        `json:"createdAtMs"`
	UpdatedAtMS    int64        `json:"updatedAtMs"`
	DeleteAfterRun bool         `json:"deleteAfterRun,omitempty"`
}

// CronSchedule defines when a job should run.
type CronSchedule struct {
	Kind    string `json:"kind"` // "at", "every", "cron"
	AtMS    *int64 `json:"atMs,omitempty"`
	EveryMS *int64 `json:"everyMs,omitempty"`
	Expr    string `json:"expr,omitempty"`
	TZ      string `json:"tz,omitempty"`
}

// CronPayload describes what a job does when triggered.
type CronPayload struct {
	Kind          string `json:"kind"`
	Message       string `json:"message"`
	Command       string `json:"command,omitempty"`
	Deliver       bool   `json:"deliver"`
	Channel       string `json:"channel,omitempty"`
	To            string `json:"to,omitempty"`
	WakeHeartbeat bool   `json:"wake_heartbeat,omitempty"` // trigger heartbeat after job completes
}

// CronJobState tracks runtime state for a job.
type CronJobState struct {
	NextRunAtMS *int64 `json:"nextRunAtMs,omitempty"`
	LastRunAtMS *int64 `json:"lastRunAtMs,omitempty"`
	LastStatus  string `json:"lastStatus,omitempty"`
	LastError   string `json:"lastError,omitempty"`
}

// CronRunLogEntry records a job execution.
type CronRunLogEntry struct {
	Ts           int64  `json:"ts"`
	JobID        string `json:"jobId"`
	Status       string `json:"status,omitempty"`
	Error        string `json:"error,omitempty"`
	Summary      string `json:"summary,omitempty"`
	DurationMS   int64  `json:"durationMs,omitempty"`
	InputTokens  int    `json:"inputTokens,omitempty"`
	OutputTokens int    `json:"outputTokens,omitempty"`
}

// CronJobResult is the output of a cron job handler execution.
type CronJobResult struct {
	Content      string `json:"content"`
	InputTokens  int    `json:"inputTokens,omitempty"`
	OutputTokens int    `json:"outputTokens,omitempty"`
	DurationMS   int64  `json:"durationMs,omitempty"`
}

// CronJobPatch holds optional fields for updating a job.
type CronJobPatch struct {
	Name           string        `json:"name,omitempty"`
	AgentID        *string       `json:"agentId,omitempty"`
	Enabled        *bool         `json:"enabled,omitempty"`
	Schedule       *CronSchedule `json:"schedule,omitempty"`
	Message        string        `json:"message,omitempty"`
	Deliver        *bool         `json:"deliver,omitempty"`
	Channel        *string       `json:"channel,omitempty"`
	To             *string       `json:"to,omitempty"`
	DeleteAfterRun *bool         `json:"deleteAfterRun,omitempty"`
	WakeHeartbeat  *bool         `json:"wakeHeartbeat,omitempty"`
}

// CronEvent represents a job lifecycle event sent to subscribers.
type CronEvent struct {
	Action  string `json:"action"` // "running", "completed", "error"
	JobID   string `json:"jobId"`
	JobName string `json:"jobName,omitempty"`
	UserID  string `json:"userId,omitempty"` // job owner for event filtering
	Status  string `json:"status,omitempty"` // final status for completed/error
	Error   string `json:"error,omitempty"`
}

// CronStore manages scheduled jobs.
type CronStore interface {
	AddJob(ctx context.Context, name string, schedule CronSchedule, message string, deliver bool, channel, to, agentID, userID string) (*CronJob, error)
	GetJob(ctx context.Context, jobID string) (*CronJob, bool)
	ListJobs(ctx context.Context, includeDisabled bool, agentID, userID string) []CronJob
	RemoveJob(ctx context.Context, jobID string) error
	UpdateJob(ctx context.Context, jobID string, patch CronJobPatch) (*CronJob, error)
	EnableJob(ctx context.Context, jobID string, enabled bool) error
	GetRunLog(ctx context.Context, jobID string, limit, offset int) ([]CronRunLogEntry, int)
	Status() map[string]any

	// Lifecycle
	Start() error
	Stop()

	// Job execution
	SetOnJob(handler func(job *CronJob) (*CronJobResult, error))
	SetOnEvent(handler func(event CronEvent))
	RunJob(ctx context.Context, jobID string, force bool) (ran bool, reason string, err error)
	SetDefaultTimezone(tz string)

	// Due job detection (for scheduler)
	GetDueJobs(now time.Time) []CronJob
}

// CacheInvalidatable is an optional interface for stores that support cache invalidation.
type CacheInvalidatable interface {
	InvalidateCache()
}

// ComputeNextRun calculates the next run time for a cron schedule.
// defaultTZ is used for cron expressions that do not specify a per-job timezone.
func ComputeNextRun(schedule *CronSchedule, now time.Time, defaultTZ string) *time.Time {
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

// NextRunForToggle returns the next run state after explicitly enabling or
// disabling a cron job. Disabling clears next_run_at immediately so the
// scheduler stops seeing the job as runnable.
func NextRunForToggle(schedule *CronSchedule, enabled bool, now time.Time, defaultTZ string) *time.Time {
	if !enabled {
		return nil
	}
	return ComputeNextRun(schedule, now, defaultTZ)
}
