//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/cron"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const defaultCronCacheTTL = 2 * time.Minute

// SQLiteCronStore implements store.CronStore backed by SQLite.
type SQLiteCronStore struct {
	db        *sql.DB
	mu        sync.Mutex
	writeMu   sync.Mutex
	baseCtx   context.Context
	cancelCtx context.CancelFunc
	onJob     func(job *store.CronJob) (*store.CronJobResult, error)
	onEvent   func(event store.CronEvent)
	running   bool
	stop      chan struct{}

	jobCache    []store.CronJob
	cacheLoaded bool
	cacheTime   time.Time
	cacheTTL    time.Duration

	retryCfg  cron.RetryConfig
	defaultTZ string
}

func NewSQLiteCronStore(db *sql.DB) *SQLiteCronStore {
	return &SQLiteCronStore{db: db, cacheTTL: defaultCronCacheTTL, retryCfg: cron.DefaultRetryConfig()}
}

func (s *SQLiteCronStore) SetRetryConfig(cfg cron.RetryConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retryCfg = cfg
}

func (s *SQLiteCronStore) SetDefaultTimezone(tz string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.defaultTZ = tz
}

func (s *SQLiteCronStore) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}
	s.baseCtx, s.cancelCtx = context.WithCancel(context.Background())
	s.stop = make(chan struct{})
	s.running = true
	s.recomputeStaleJobs()
	go s.runLoop()
	slog.Info("sqlite cron service started")
	return nil
}

func (s *SQLiteCronStore) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	close(s.stop)
	if s.cancelCtx != nil {
		s.cancelCtx()
	}
	s.running = false
}

func (s *SQLiteCronStore) SetOnJob(handler func(job *store.CronJob) (*store.CronJobResult, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onJob = handler
}

func (s *SQLiteCronStore) SetOnEvent(handler func(event store.CronEvent)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onEvent = handler
}

func (s *SQLiteCronStore) emitEvent(event store.CronEvent) {
	s.mu.Lock()
	fn := s.onEvent
	s.mu.Unlock()
	if fn != nil {
		fn(event)
	}
}

// --- Scan helpers ---

type cronRowScanner interface {
	Scan(dest ...any) error
}

func scanCronRow(row cronRowScanner) (*store.CronJob, error) {
	var id uuid.UUID
	var tenantID uuid.UUID
	var agentID *uuid.UUID
	var userID *string
	var name, scheduleKind string
	var enabled, deleteAfterRun bool
	var stateless, deliver, wakeHeartbeat bool
	var deliverChannel, deliverTo string
	var cronExpr, tz, lastStatus, lastError *string
	var runAt, nextRunAt, lastRunAt nullSqliteTime
	var intervalMS *int64
	var payloadJSON []byte
	createdAt, updatedAt := scanTimePair()

	err := row.Scan(&id, &tenantID, &agentID, &userID, &name, &enabled, &scheduleKind, &cronExpr, &runAt, &tz,
		&intervalMS, &payloadJSON, &deleteAfterRun, &stateless, &deliver, &deliverChannel, &deliverTo, &wakeHeartbeat,
		&nextRunAt, &lastRunAt, &lastStatus, &lastError,
		createdAt, updatedAt)
	if err != nil {
		return nil, err
	}

	var payload store.CronPayload
	if len(payloadJSON) > 0 {
		if err := json.Unmarshal(payloadJSON, &payload); err != nil {
			return nil, fmt.Errorf("failed to parse cron job payload: %w", err)
		}
	}

	job := &store.CronJob{
		ID:             id.String(),
		TenantID:       tenantID,
		Name:           name,
		Enabled:        enabled,
		Schedule:       store.CronSchedule{Kind: scheduleKind},
		Payload:        payload,
		CreatedAtMS:    createdAt.Time.UnixMilli(),
		UpdatedAtMS:    updatedAt.Time.UnixMilli(),
		DeleteAfterRun: deleteAfterRun,
		Stateless:      stateless,
		Deliver:        deliver,
		DeliverChannel: deliverChannel,
		DeliverTo:      deliverTo,
		WakeHeartbeat:  wakeHeartbeat,
	}

	if agentID != nil {
		job.AgentID = agentID.String()
	}
	if userID != nil {
		job.UserID = *userID
	}
	if cronExpr != nil {
		job.Schedule.Expr = *cronExpr
	}
	if runAt.Valid {
		ms := runAt.Time.UnixMilli()
		job.Schedule.AtMS = &ms
	}
	if intervalMS != nil {
		job.Schedule.EveryMS = intervalMS
	}
	if tz != nil {
		job.Schedule.TZ = *tz
	}
	if nextRunAt.Valid {
		ms := nextRunAt.Time.UnixMilli()
		job.State.NextRunAtMS = &ms
	}
	if lastRunAt.Valid {
		ms := lastRunAt.Time.UnixMilli()
		job.State.LastRunAtMS = &ms
	}
	if lastStatus != nil {
		job.State.LastStatus = *lastStatus
	}
	if lastError != nil {
		job.State.LastError = *lastError
	}

	return job, nil
}

// computeNextRun calculates the next run time for a schedule.
func computeNextRun(schedule *store.CronSchedule, now time.Time, defaultTZ string) *time.Time {
	return store.ComputeNextRun(schedule, now, defaultTZ)
}

func (s *SQLiteCronStore) scanJob(ctx context.Context, id uuid.UUID) (*store.CronJob, error) {
	q := `SELECT id, tenant_id, agent_id, user_id, name, enabled, schedule_kind, cron_expression, run_at, timezone,
		 interval_ms, payload, delete_after_run, stateless, deliver, deliver_channel, deliver_to, wake_heartbeat,
		 next_run_at, last_run_at, last_status, last_error,
		 created_at, updated_at FROM cron_jobs WHERE id = ?`
	args := []any{id}

	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return nil, fmt.Errorf("tenant_id required")
		}
		q += " AND tenant_id = ?"
		args = append(args, tid)
	}

	row := s.db.QueryRowContext(ctx, q, args...)
	return scanCronRow(row)
}
