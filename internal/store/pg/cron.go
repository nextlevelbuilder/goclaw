package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/adhocore/gronx"
	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/cron"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const defaultCronCacheTTL = 2 * time.Minute

// PGCronStore implements store.CronStore backed by Postgres.
// GetDueJobs() uses an in-memory cache with TTL to reduce DB polling (1s interval).
type PGCronStore struct {
	db      *sql.DB
	mu      sync.Mutex
	onJob   func(job *store.CronJob) (*store.CronJobResult, error)
	running bool
	stop    chan struct{}

	// Job cache: reduces GetDueJobs polling from 86,400 queries/day to ~720/day
	jobCache    []store.CronJob
	cacheLoaded bool
	cacheTime   time.Time
	cacheTTL    time.Duration

	retryCfg cron.RetryConfig
}

func NewPGCronStore(db *sql.DB) *PGCronStore {
	return &PGCronStore{db: db, cacheTTL: defaultCronCacheTTL, retryCfg: cron.DefaultRetryConfig()}
}

// SetRetryConfig overrides the default retry configuration.
func (s *PGCronStore) SetRetryConfig(cfg cron.RetryConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retryCfg = cfg
}

func (s *PGCronStore) AddJob(name string, schedule store.CronSchedule, message string, deliver bool, channel, to, agentID, userID string) (*store.CronJob, error) {
	if schedule.TZ != "" {
		if _, err := time.LoadLocation(schedule.TZ); err != nil {
			return nil, fmt.Errorf("invalid timezone: %s", schedule.TZ)
		}
	}

	payload := store.CronPayload{
		Kind: "agent_turn", Message: message, Deliver: deliver, Channel: channel, To: to,
	}
	payloadJSON, _ := json.Marshal(payload)

	id := uuid.Must(uuid.NewV7())
	now := time.Now()
	scheduleKind := schedule.Kind
	deleteAfterRun := schedule.Kind == "at"

	var cronExpr, tz *string
	var runAt *time.Time
	if schedule.Expr != "" {
		cronExpr = &schedule.Expr
	}
	if schedule.AtMS != nil {
		t := time.UnixMilli(*schedule.AtMS)
		runAt = &t
	}
	if schedule.TZ != "" {
		tz = &schedule.TZ
	}

	var agentUUID *uuid.UUID
	if agentID != "" {
		aid, err := uuid.Parse(agentID)
		if err == nil {
			agentUUID = &aid
		}
	}

	var userIDPtr *string
	if userID != "" {
		userIDPtr = &userID
	}

	var intervalMS *int64
	if schedule.EveryMS != nil {
		intervalMS = schedule.EveryMS
	}

	nextRun := computeNextRun(&schedule, now)

	_, err := s.db.Exec(
		`INSERT INTO cron_jobs (id, agent_id, user_id, name, enabled, schedule_kind, cron_expression, run_at, timezone,
		 interval_ms, payload, delete_after_run, next_run_at, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, true, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		id, agentUUID, userIDPtr, name, scheduleKind, cronExpr, runAt, tz,
		intervalMS, payloadJSON, deleteAfterRun, nextRun, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("create cron job: %w", err)
	}

	s.cacheLoaded = false // invalidate cache

	job, _ := s.GetJob(id.String())
	return job, nil
}

func (s *PGCronStore) GetJob(jobID string) (*store.CronJob, bool) {
	id, err := uuid.Parse(jobID)
	if err != nil {
		return nil, false
	}
	job, err := s.scanJob(id)
	if err != nil {
		return nil, false
	}
	return job, true
}

func (s *PGCronStore) ListJobs(includeDisabled bool, agentID, userID string) []store.CronJob {
	q := `SELECT id, agent_id, user_id, name, enabled, schedule_kind, cron_expression, run_at, timezone,
		 interval_ms, payload, delete_after_run, next_run_at, last_run_at, last_status, last_error,
		 created_at, updated_at FROM cron_jobs WHERE 1=1`

	var args []interface{}
	argIdx := 1

	if !includeDisabled {
		q += fmt.Sprintf(" AND enabled = $%d", argIdx)
		args = append(args, true)
		argIdx++
	}
	if agentID != "" {
		if aid, err := uuid.Parse(agentID); err == nil {
			q += fmt.Sprintf(" AND agent_id = $%d", argIdx)
			args = append(args, aid)
			argIdx++
		}
	}
	if userID != "" {
		q += fmt.Sprintf(" AND user_id = $%d", argIdx)
		args = append(args, userID)
		argIdx++
	}

	q += " ORDER BY created_at DESC"

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []store.CronJob
	for rows.Next() {
		job, err := scanCronRow(rows)
		if err != nil {
			continue
		}
		result = append(result, *job)
	}
	return result
}

func (s *PGCronStore) RemoveJob(jobID string) error {
	id, err := uuid.Parse(jobID)
	if err != nil {
		return fmt.Errorf("invalid job ID: %s", jobID)
	}
	_, err = s.db.Exec("DELETE FROM cron_jobs WHERE id = $1", id)
	if err != nil {
		return err
	}
	s.cacheLoaded = false
	return nil
}

func (s *PGCronStore) UpdateJob(jobID string, patch store.CronJobPatch) (*store.CronJob, error) {
	id, err := uuid.Parse(jobID)
	if err != nil {
		return nil, fmt.Errorf("invalid job ID: %s", jobID)
	}

	updates := make(map[string]interface{})
	if patch.Name != "" {
		updates["name"] = patch.Name
	}
	if patch.Enabled != nil {
		updates["enabled"] = *patch.Enabled
	}
	if patch.Schedule != nil {
		// Fetch current schedule to merge with patch (partial updates)
		var curKind string
		var curExpr, curTZ *string
		var curIntervalMS *int64
		var curRunAt *time.Time
		s.db.QueryRow(
			"SELECT schedule_kind, cron_expression, timezone, interval_ms, run_at FROM cron_jobs WHERE id = $1", id,
		).Scan(&curKind, &curExpr, &curTZ, &curIntervalMS, &curRunAt)

		// Resolve the effective schedule kind
		newKind := patch.Schedule.Kind
		if newKind == "" {
			newKind = curKind // keep current kind if not specified
		}
		updates["schedule_kind"] = newKind

		// Set type-specific fields and clear others
		switch newKind {
		case "cron":
			if patch.Schedule.Expr != "" {
				updates["cron_expression"] = patch.Schedule.Expr
			}
			if patch.Schedule.TZ != "" {
				if _, err := time.LoadLocation(patch.Schedule.TZ); err != nil {
					return nil, fmt.Errorf("invalid timezone: %s", patch.Schedule.TZ)
				}
				updates["timezone"] = patch.Schedule.TZ
			}
			// Clear other type fields when switching to cron
			if curKind != "cron" {
				updates["interval_ms"] = nil
				updates["run_at"] = nil
			}
		case "every":
			if patch.Schedule.EveryMS != nil {
				updates["interval_ms"] = *patch.Schedule.EveryMS
			}
			// Clear other type fields when switching to every
			if curKind != "every" {
				updates["cron_expression"] = nil
				updates["timezone"] = nil
				updates["run_at"] = nil
			}
		case "at":
			if patch.Schedule.AtMS != nil {
				t := time.UnixMilli(*patch.Schedule.AtMS)
				updates["run_at"] = t
			}
			// Clear other type fields when switching to at
			if curKind != "at" {
				updates["cron_expression"] = nil
				updates["timezone"] = nil
				updates["interval_ms"] = nil
			}
		}

		// Build merged schedule for recomputing next_run_at
		merged := store.CronSchedule{Kind: newKind}
		switch newKind {
		case "cron":
			if patch.Schedule.Expr != "" {
				merged.Expr = patch.Schedule.Expr
			} else if curExpr != nil {
				merged.Expr = *curExpr
			}
			if patch.Schedule.TZ != "" {
				merged.TZ = patch.Schedule.TZ
			} else if curTZ != nil && newKind == curKind {
				merged.TZ = *curTZ
			}
		case "every":
			if patch.Schedule.EveryMS != nil {
				merged.EveryMS = patch.Schedule.EveryMS
			} else if curIntervalMS != nil {
				merged.EveryMS = curIntervalMS
			}
		case "at":
			if patch.Schedule.AtMS != nil {
				merged.AtMS = patch.Schedule.AtMS
			} else if curRunAt != nil {
				ms := curRunAt.UnixMilli()
				merged.AtMS = &ms
			}
		}

		// Validate the merged schedule before applying
		switch merged.Kind {
		case "cron":
			if merged.Expr == "" {
				return nil, fmt.Errorf("cron schedule requires expr")
			}
			gx := gronx.New()
			if !gx.IsValid(merged.Expr) {
				return nil, fmt.Errorf("invalid cron expression: %s", merged.Expr)
			}
		case "every":
			if merged.EveryMS == nil || *merged.EveryMS <= 0 {
				return nil, fmt.Errorf("every schedule requires positive everyMs")
			}
		case "at":
			if merged.AtMS == nil {
				return nil, fmt.Errorf("at schedule requires atMs")
			}
		}

		next := computeNextRun(&merged, time.Now())
		updates["next_run_at"] = next
	}
	if patch.DeleteAfterRun != nil {
		updates["delete_after_run"] = *patch.DeleteAfterRun
	}

	// Update agent_id column
	if patch.AgentID != nil {
		if *patch.AgentID == "" {
			updates["agent_id"] = nil
		} else if aid, parseErr := uuid.Parse(*patch.AgentID); parseErr == nil {
			updates["agent_id"] = aid
		}
	}

	// Update payload JSONB — fetch current, merge patch fields, re-serialize
	needsPayloadUpdate := patch.Message != "" || patch.Deliver != nil || patch.Channel != nil || patch.To != nil
	if needsPayloadUpdate {
		var payloadJSON []byte
		if scanErr := s.db.QueryRow("SELECT payload FROM cron_jobs WHERE id = $1", id).Scan(&payloadJSON); scanErr == nil {
			var payload store.CronPayload
			json.Unmarshal(payloadJSON, &payload)

			if patch.Message != "" {
				payload.Message = patch.Message
			}
			if patch.Deliver != nil {
				payload.Deliver = *patch.Deliver
			}
			if patch.Channel != nil {
				payload.Channel = *patch.Channel
			}
			if patch.To != nil {
				payload.To = *patch.To
			}

			merged, _ := json.Marshal(payload)
			updates["payload"] = merged
		}
	}

	updates["updated_at"] = time.Now()

	if err := execMapUpdate(context.Background(), s.db, "cron_jobs", id, updates); err != nil {
		return nil, err
	}

	s.cacheLoaded = false
	job, _ := s.scanJob(id)
	return job, nil
}

func (s *PGCronStore) EnableJob(jobID string, enabled bool) error {
	id, err := uuid.Parse(jobID)
	if err != nil {
		return fmt.Errorf("invalid job ID: %s", jobID)
	}
	_, err = s.db.Exec("UPDATE cron_jobs SET enabled = $1, updated_at = $2 WHERE id = $3", enabled, time.Now(), id)
	if err != nil {
		return err
	}
	s.cacheLoaded = false
	return nil
}

func (s *PGCronStore) GetRunLog(jobID string, limit int) []store.CronRunLogEntry {
	if limit <= 0 {
		limit = 20
	}

	var rows *sql.Rows
	var err error
	if jobID != "" {
		id, parseErr := uuid.Parse(jobID)
		if parseErr != nil {
			return nil
		}
		rows, err = s.db.Query(
			"SELECT job_id, status, error, summary, ran_at FROM cron_run_logs WHERE job_id = $1 ORDER BY ran_at DESC LIMIT $2",
			id, limit)
	} else {
		rows, err = s.db.Query(
			"SELECT job_id, status, error, summary, ran_at FROM cron_run_logs ORDER BY ran_at DESC LIMIT $1", limit)
	}
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []store.CronRunLogEntry
	for rows.Next() {
		var jobUUID uuid.UUID
		var status string
		var errStr, summary *string
		var ranAt time.Time
		if err := rows.Scan(&jobUUID, &status, &errStr, &summary, &ranAt); err != nil {
			continue
		}
		result = append(result, store.CronRunLogEntry{
			Ts:      ranAt.UnixMilli(),
			JobID:   jobUUID.String(),
			Status:  status,
			Error:   derefStr(errStr),
			Summary: derefStr(summary),
		})
	}
	return result
}

func (s *PGCronStore) Status() map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	var count int64
	s.db.QueryRow("SELECT COUNT(*) FROM cron_jobs WHERE enabled = true").Scan(&count)
	return map[string]interface{}{
		"enabled": s.running,
		"jobs":    count,
	}
}

func (s *PGCronStore) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}
	s.stop = make(chan struct{})
	s.running = true
	s.recomputeStaleJobs()
	go s.runLoop()
	slog.Info("pg cron service started")
	return nil
}

func (s *PGCronStore) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	close(s.stop)
	s.running = false
}

func (s *PGCronStore) SetOnJob(handler func(job *store.CronJob) (*store.CronJobResult, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onJob = handler
}

func (s *PGCronStore) RunJob(jobID string, force bool) (bool, string, error) {
	job, ok := s.GetJob(jobID)
	if !ok {
		return false, "", fmt.Errorf("job %s not found", jobID)
	}

	s.mu.Lock()
	handler := s.onJob
	s.mu.Unlock()

	if handler == nil {
		return false, "", fmt.Errorf("no job handler configured")
	}

	result, err := handler(job)
	content := ""
	if result != nil {
		content = result.Content
	}
	return true, content, err
}
