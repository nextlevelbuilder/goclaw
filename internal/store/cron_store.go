package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/adhocore/gronx"
	"github.com/google/uuid"
)

var (
	ErrCronJobNotFound    = errors.New("cron job not found")
	ErrCronJobNoFutureRun = errors.New("cron job has no future run")
)

// CronJob represents a scheduled job.
type CronJob struct {
	ID             string       `json:"id" db:"id"`
	TenantID       uuid.UUID    `json:"tenantId,omitempty" db:"tenant_id"`
	Name           string       `json:"name" db:"name"`
	AgentID        string       `json:"agentId,omitempty" db:"agent_id"`
	UserID         string       `json:"userId,omitempty" db:"user_id"`
	Enabled        bool         `json:"enabled" db:"enabled"`
	Schedule       CronSchedule `json:"schedule" db:"-"`
	Payload        CronPayload  `json:"payload" db:"-"`
	State          CronJobState `json:"state" db:"-"`
	CreatedAtMS    int64        `json:"createdAtMs" db:"-"`
	UpdatedAtMS    int64        `json:"updatedAtMs" db:"-"`
	DeleteAfterRun bool         `json:"deleteAfterRun,omitempty" db:"delete_after_run"`
	Stateless      bool         `json:"stateless" db:"stateless"`
	Deliver        bool         `json:"deliver" db:"deliver"`
	DeliverChannel string       `json:"deliverChannel" db:"deliver_channel"`
	DeliverTo      string       `json:"deliverTo" db:"deliver_to"`
	WakeHeartbeat  bool         `json:"wakeHeartbeat" db:"wake_heartbeat"`
}

// CronSchedule defines when a job should run.
type CronSchedule struct {
	Kind    string `json:"kind" db:"-"` // "at", "every", "cron"
	AtMS    *int64 `json:"atMs,omitempty" db:"-"`
	EveryMS *int64 `json:"everyMs,omitempty" db:"-"`
	Expr    string `json:"expr,omitempty" db:"-"`
	TZ      string `json:"tz,omitempty" db:"-"`
}

// CronPayload describes what a job does when triggered.
type CronPayload struct {
	Kind    string `json:"kind" db:"-"`
	Message string `json:"message" db:"-"`
	Command string `json:"command,omitempty" db:"-"`
}

// CronJobState tracks runtime state for a job.
type CronJobState struct {
	NextRunAtMS *int64 `json:"nextRunAtMs,omitempty" db:"-"`
	LastRunAtMS *int64 `json:"lastRunAtMs,omitempty" db:"-"`
	LastStatus  string `json:"lastStatus,omitempty" db:"-"`
	LastError   string `json:"lastError,omitempty" db:"-"`
}

// CronRunLogEntry records a job execution.
type CronRunLogEntry struct {
	Ts           int64  `json:"ts" db:"-"`
	JobID        string `json:"jobId" db:"-"`
	Status       string `json:"status,omitempty" db:"-"`
	Error        string `json:"error,omitempty" db:"-"`
	Summary      string `json:"summary,omitempty" db:"-"`
	DurationMS   int64  `json:"durationMs,omitempty" db:"-"`
	InputTokens  int    `json:"inputTokens,omitempty" db:"-"`
	OutputTokens int    `json:"outputTokens,omitempty" db:"-"`
}

// CronJobResult is the output of a cron job handler execution.
type CronJobResult struct {
	Content      string `json:"content" db:"-"`
	InputTokens  int    `json:"inputTokens,omitempty" db:"-"`
	OutputTokens int    `json:"outputTokens,omitempty" db:"-"`
	DurationMS   int64  `json:"durationMs,omitempty" db:"-"`
}

// CronJobPatch holds optional fields for updating a job.
type CronJobPatch struct {
	Name           string        `json:"name,omitempty" db:"-"`
	AgentID        *string       `json:"agentId,omitempty" db:"-"`
	Enabled        *bool         `json:"enabled,omitempty" db:"-"`
	Schedule       *CronSchedule `json:"schedule,omitempty" db:"-"`
	Message        string        `json:"message,omitempty" db:"-"`
	DeleteAfterRun *bool         `json:"deleteAfterRun,omitempty" db:"-"`
	Stateless      *bool         `json:"stateless,omitempty" db:"-"`
	Deliver        *bool         `json:"deliver,omitempty" db:"-"`
	DeliverChannel *string       `json:"deliverChannel,omitempty" db:"-"`
	DeliverTo      *string       `json:"deliverTo,omitempty" db:"-"`
	WakeHeartbeat  *bool         `json:"wakeHeartbeat,omitempty" db:"-"`
}

// CronEvent represents a job lifecycle event sent to subscribers.
type CronEvent struct {
	Action  string `json:"action" db:"-"` // "running", "completed", "error"
	JobID   string `json:"jobId" db:"-"`
	JobName string `json:"jobName,omitempty" db:"-"`
	UserID  string `json:"userId,omitempty" db:"-"` // job owner for event filtering
	Status  string `json:"status,omitempty" db:"-"` // final status for completed/error
	Error   string `json:"error,omitempty" db:"-"`
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

// CronJobMutableState holds the mutable fields of a cron job loaded within a
// transaction for read-compute-write operations (EnableJob, UpdateJob).
type CronJobMutableState struct {
	Enabled   bool         `db:"-"`
	Schedule  CronSchedule `db:"-"`
	NextRunAt *time.Time   `db:"-"`
	Payload   CronPayload  `db:"-"`
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

// NextRunForSchedule resolves the persisted next_run_at for a given schedule state.
func NextRunForSchedule(schedule *CronSchedule, enabled bool, now time.Time, defaultTZ string) (*time.Time, error) {
	if !enabled {
		return nil, nil
	}

	next := ComputeNextRun(schedule, now, defaultTZ)
	if next != nil {
		return next, nil
	}

	switch schedule.Kind {
	case "at":
		return nil, fmt.Errorf("%w: at schedule is already in the past", ErrCronJobNoFutureRun)
	case "cron":
		return nil, fmt.Errorf("%w: cron schedule has no valid next execution", ErrCronJobNoFutureRun)
	case "every":
		return nil, fmt.Errorf("%w: every schedule has no valid interval", ErrCronJobNoFutureRun)
	default:
		return nil, fmt.Errorf("%w: unsupported schedule kind %q", ErrCronJobNoFutureRun, schedule.Kind)
	}
}

// NextRunForToggle returns the next run state after explicitly enabling or
// disabling a cron job. Disabling clears next_run_at immediately so the
// scheduler stops seeing the job as runnable.
func NextRunForToggle(schedule *CronSchedule, enabled, currentlyEnabled bool, currentNextRunAt *time.Time, now time.Time, defaultTZ string) (*time.Time, error) {
	if !enabled {
		return nil, nil
	}
	if currentlyEnabled && currentNextRunAt != nil {
		next := *currentNextRunAt
		return &next, nil
	}
	return NextRunForSchedule(schedule, true, now, defaultTZ)
}

// MergeCronSchedule applies a partial schedule patch on top of the current schedule.
func MergeCronSchedule(current CronSchedule, patch *CronSchedule) CronSchedule {
	if patch == nil {
		return current
	}

	newKind := patch.Kind
	if newKind == "" {
		newKind = current.Kind
	}

	merged := CronSchedule{Kind: newKind}
	// TZ: always use patch value for all schedule kinds. Empty = UTC (default).
	merged.TZ = patch.TZ
	switch newKind {
	case "cron":
		if patch.Expr != "" {
			merged.Expr = patch.Expr
		} else if current.Kind == newKind {
			merged.Expr = current.Expr
		}
	case "every":
		if patch.EveryMS != nil {
			merged.EveryMS = patch.EveryMS
		} else if current.Kind == newKind {
			merged.EveryMS = current.EveryMS
		}
	case "at":
		if patch.AtMS != nil {
			merged.AtMS = patch.AtMS
		} else if current.Kind == newKind {
			merged.AtMS = current.AtMS
		}
	}

	return merged
}

// ValidateCronSchedule checks structural schedule validity without evaluating future run existence.
func ValidateCronSchedule(schedule *CronSchedule) error {
	switch schedule.Kind {
	case "cron":
		if schedule.Expr == "" {
			return fmt.Errorf("cron schedule requires expr")
		}
		if !gronx.New().IsValid(schedule.Expr) {
			return fmt.Errorf("invalid cron expression: %s", schedule.Expr)
		}
		if schedule.TZ != "" {
			if _, err := time.LoadLocation(schedule.TZ); err != nil {
				return fmt.Errorf("invalid timezone: %s", schedule.TZ)
			}
		}
	case "every":
		if schedule.EveryMS == nil || *schedule.EveryMS <= 0 {
			return fmt.Errorf("every schedule requires positive everyMs")
		}
	case "at":
		if schedule.AtMS == nil {
			return fmt.Errorf("at schedule requires atMs")
		}
	default:
		return fmt.Errorf("invalid schedule kind: %s", schedule.Kind)
	}
	return nil
}

// ApplyCronScheduleUpdates populates the update map with the column values
// for a fully-resolved cron schedule (after merge + validation).
func ApplyCronScheduleUpdates(updates map[string]any, schedule CronSchedule) {
	updates["schedule_kind"] = schedule.Kind

	// TZ applies to all schedule kinds (used for display and cron evaluation).
	if schedule.TZ != "" {
		updates["timezone"] = schedule.TZ
	} else {
		updates["timezone"] = nil
	}

	switch schedule.Kind {
	case "cron":
		updates["cron_expression"] = schedule.Expr
		updates["interval_ms"] = nil
		updates["run_at"] = nil
	case "every":
		updates["cron_expression"] = nil
		updates["interval_ms"] = *schedule.EveryMS
		updates["run_at"] = nil
	case "at":
		runAt := time.UnixMilli(*schedule.AtMS)
		updates["cron_expression"] = nil
		updates["interval_ms"] = nil
		updates["run_at"] = runAt
	}
}

// SortedUpdateColumns returns the map keys in sorted order for deterministic
// SQL generation.
func SortedUpdateColumns(updates map[string]any) []string {
	cols := make([]string, 0, len(updates))
	for col := range updates {
		cols = append(cols, col)
	}
	sort.Strings(cols)
	return cols
}
