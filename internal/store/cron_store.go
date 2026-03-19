package store

import "time"

// CronJob represents a scheduled job.
type CronJob struct {
	ID             string       `json:"id"`
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
	Status  string `json:"status,omitempty"` // final status for completed/error
	Error   string `json:"error,omitempty"`
}

// CronStore manages scheduled jobs.
type CronStore interface {
	AddJob(name string, schedule CronSchedule, message string, deliver bool, channel, to, agentID, userID string) (*CronJob, error)
	GetJob(jobID string) (*CronJob, bool)
	ListJobs(includeDisabled bool, agentID, userID string) []CronJob
	RemoveJob(jobID string) error
	UpdateJob(jobID string, patch CronJobPatch) (*CronJob, error)
	EnableJob(jobID string, enabled bool) error
	GetRunLog(jobID string, limit, offset int) ([]CronRunLogEntry, int)
	Status() map[string]any

	// Lifecycle
	Start() error
	Stop()

	// Job execution
	SetOnJob(handler func(job *CronJob) (*CronJobResult, error))
	SetOnEvent(handler func(event CronEvent))
	RunJob(jobID string, force bool) (ran bool, reason string, err error)
	SetDefaultTimezone(tz string)

	// Due job detection (for scheduler)
	GetDueJobs(now time.Time) []CronJob
}

// CacheInvalidatable is an optional interface for stores that support cache invalidation.
type CacheInvalidatable interface {
	InvalidateCache()
}
