// Package cron provides a lightweight cron/scheduler for recurring agent tasks.
// Jobs are persisted to JSON and executed via callback to the agent runtime.
//
// Three schedule types are supported:
//   - "at":    one-time execution at a specific timestamp
//   - "every": recurring interval (in milliseconds)
//   - "cron":  standard cron expression (5-field, parsed by gronx)
package cron

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// Schedule defines when a job should run.
type Schedule struct {
	Kind    string `json:"kind"`              // "at", "every", or "cron"
	AtMS    *int64 `json:"atMs,omitempty"`    // absolute timestamp (for "at")
	EveryMS *int64 `json:"everyMs,omitempty"` // interval in milliseconds (for "every")
	Expr    string `json:"expr,omitempty"`    // cron expression (for "cron")
	TZ      string `json:"tz,omitempty"`      // timezone (reserved)
}

// Payload describes what a job does when triggered.
type Payload struct {
	Kind    string `json:"kind"`              // "agent_turn"
	Message string `json:"message"`           // content to process
	Command string `json:"command,omitempty"` // optional shell command
}

// JobState tracks runtime state for a job.
type JobState struct {
	NextRunAtMS *int64 `json:"nextRunAtMs,omitempty"` // next scheduled execution
	LastRunAtMS *int64 `json:"lastRunAtMs,omitempty"` // last execution timestamp
	LastStatus  string `json:"lastStatus,omitempty"`  // "ok" or "error"
	LastError   string `json:"lastError,omitempty"`   // error message if failed
}

// Job represents a scheduled cron job.
type Job struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	AgentID        string   `json:"agentId,omitempty"`
	Enabled        bool     `json:"enabled"`
	Schedule       Schedule `json:"schedule"`
	Payload        Payload  `json:"payload"`
	State          JobState `json:"state"`
	CreatedAtMS    int64    `json:"createdAtMs"`
	UpdatedAtMS    int64    `json:"updatedAtMs"`
	DeleteAfterRun bool     `json:"deleteAfterRun,omitempty"`
	Stateless      bool     `json:"stateless"`
	Deliver        bool     `json:"deliver"`
	DeliverChannel string   `json:"deliverChannel"`
	DeliverTo      string   `json:"deliverTo"`
	WakeHeartbeat  bool     `json:"wakeHeartbeat"`
}

// Store is the persistent store for all cron jobs.
type Store struct {
	Version int   `json:"version"`
	Jobs    []Job `json:"jobs"`
}

// JobPatch holds optional fields for updating a job.
// Only non-zero/non-nil fields are applied. Matching TS CronJobPatch.
type JobPatch struct {
	Name           string    `json:"name,omitempty"`
	AgentID        *string   `json:"agentId,omitempty"`
	Enabled        *bool     `json:"enabled,omitempty"`
	Schedule       *Schedule `json:"schedule,omitempty"`
	Message        string    `json:"message,omitempty"`
	DeleteAfterRun *bool     `json:"deleteAfterRun,omitempty"`
	Stateless      *bool     `json:"stateless,omitempty"`
	Deliver        *bool     `json:"deliver,omitempty"`
	DeliverChannel *string   `json:"deliverChannel,omitempty"`
	DeliverTo      *string   `json:"deliverTo,omitempty"`
	WakeHeartbeat  *bool     `json:"wakeHeartbeat,omitempty"`
}

// RunLogEntry is an in-memory record of a job execution.
// Matching TS CronRunLogEntry.
type RunLogEntry struct {
	Ts      int64  `json:"ts"`
	JobID   string `json:"jobId"`
	Status  string `json:"status,omitempty"` // "ok", "error"
	Error   string `json:"error,omitempty"`
	Summary string `json:"summary,omitempty"`
}

// JobHandler is a callback invoked when a job fires.
// Returns the execution result string and any error.
type JobHandler func(job *Job) (string, error)

// generateID creates a random 8-byte hex ID for a new job.
func generateID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// nowMS returns the current time in milliseconds.
func nowMS() int64 {
	return time.Now().UnixMilli()
}
