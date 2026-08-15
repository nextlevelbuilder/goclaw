package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGRunTimelineStore implements store.RunTimelineStore backed by PostgreSQL.
type PGRunTimelineStore struct {
	db *sql.DB
}

func NewPGRunTimelineStore(db *sql.DB) *PGRunTimelineStore {
	return &PGRunTimelineStore{db: db}
}

func (s *PGRunTimelineStore) AppendRunTimelineItem(ctx context.Context, item *store.RunTimelineItem) error {
	if item.ID == uuid.Nil {
		item.ID = store.GenNewID()
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now()
	}
	tenantID := tenantIDForInsert(ctx)
	item.TenantID = tenantID
	metadata := item.Metadata
	if len(metadata) == 0 {
		metadata = []byte(`{}`)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO run_timeline_items
		 (id, tenant_id, run_id, session_key, agent_id, user_id, channel, chat_id, seq,
		  item_type, status, title, preview, content, tool_name, tool_call_id, trace_id, span_id,
		  metadata, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9,
		  $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
		 ON CONFLICT (tenant_id, run_id, seq) DO UPDATE SET
		  session_key = EXCLUDED.session_key,
		  agent_id = EXCLUDED.agent_id,
		  user_id = EXCLUDED.user_id,
		  channel = EXCLUDED.channel,
		  chat_id = EXCLUDED.chat_id,
		  item_type = EXCLUDED.item_type,
		  status = EXCLUDED.status,
		  title = EXCLUDED.title,
		  preview = EXCLUDED.preview,
		  content = '',
		  tool_name = EXCLUDED.tool_name,
		  tool_call_id = EXCLUDED.tool_call_id,
		  trace_id = EXCLUDED.trace_id,
		  span_id = EXCLUDED.span_id,
		  metadata = EXCLUDED.metadata,
		  created_at = EXCLUDED.created_at`,
		item.ID, tenantID, item.RunID, item.SessionKey, nilUUID(item.AgentID), nilStr(item.UserID),
		nilStr(item.Channel), nilStr(item.ChatID), item.Seq, item.ItemType, nilStr(item.Status),
		nilStr(item.Title), nilStr(item.Preview), "", nilStr(item.ToolName), nilStr(item.ToolCallID),
		nilUUID(item.TraceID), nilUUID(item.SpanID), jsonOrEmpty(metadata), item.CreatedAt,
	)
	if err == nil {
		item.Content = ""
	}
	return err
}

func (s *PGRunTimelineStore) ListRunTimelineItems(ctx context.Context, opts store.RunTimelineListOpts) ([]store.RunTimelineItem, error) {
	where, args := buildRunTimelineWhere(ctx, opts)
	limit := opts.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	q := `SELECT id, tenant_id, run_id, session_key, agent_id, user_id, channel, chat_id, seq,
		 item_type, status, title, preview, COALESCE(content, '') AS content, tool_name, tool_call_id,
		 trace_id, span_id, COALESCE(metadata, '{}'::jsonb) AS metadata, created_at
		 FROM run_timeline_items` + where +
		runTimelineOrderBy(opts) +
		fmt.Sprintf(" OFFSET %d LIMIT %d", opts.Offset, limit)

	var rows []runTimelineRow
	if err := pkgSqlxDB.SelectContext(ctx, &rows, q, args...); err != nil {
		return nil, err
	}
	items := make([]store.RunTimelineItem, len(rows))
	for i, row := range rows {
		items[i] = row.toStore()
	}
	return items, nil
}

func runTimelineOrderBy(opts store.RunTimelineListOpts) string {
	if opts.RunID != "" {
		return " ORDER BY seq ASC, created_at ASC"
	}
	return " ORDER BY created_at ASC, seq ASC"
}

func buildRunTimelineWhere(ctx context.Context, opts store.RunTimelineListOpts) (string, []any) {
	var conditions []string
	var args []any
	argIdx := 1
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return " WHERE 1=0", nil
		}
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIdx))
		args = append(args, tenantID)
		argIdx++
	}
	if opts.RunID != "" {
		conditions = append(conditions, fmt.Sprintf("run_id = $%d", argIdx))
		args = append(args, opts.RunID)
		argIdx++
	}
	if opts.AfterSeq > 0 {
		conditions = append(conditions, fmt.Sprintf("seq > $%d", argIdx))
		args = append(args, opts.AfterSeq)
		argIdx++
	}
	if opts.SessionKey != "" {
		conditions = append(conditions, fmt.Sprintf("session_key = $%d", argIdx))
		args = append(args, opts.SessionKey)
	}
	if len(conditions) == 0 {
		return " WHERE 1=0", nil
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

type runTimelineRow struct {
	ID         uuid.UUID       `db:"id"`
	TenantID   uuid.UUID       `db:"tenant_id"`
	RunID      string          `db:"run_id"`
	SessionKey string          `db:"session_key"`
	AgentID    *uuid.UUID      `db:"agent_id"`
	UserID     *string         `db:"user_id"`
	Channel    *string         `db:"channel"`
	ChatID     *string         `db:"chat_id"`
	Seq        int             `db:"seq"`
	ItemType   string          `db:"item_type"`
	Status     *string         `db:"status"`
	Title      *string         `db:"title"`
	Preview    *string         `db:"preview"`
	Content    string          `db:"content"`
	ToolName   *string         `db:"tool_name"`
	ToolCallID *string         `db:"tool_call_id"`
	TraceID    *uuid.UUID      `db:"trace_id"`
	SpanID     *uuid.UUID      `db:"span_id"`
	Metadata   json.RawMessage `db:"metadata"`
	CreatedAt  time.Time       `db:"created_at"`
}

func (r runTimelineRow) toStore() store.RunTimelineItem {
	return store.RunTimelineItem{
		ID: r.ID, TenantID: r.TenantID, RunID: r.RunID, SessionKey: r.SessionKey,
		AgentID: r.AgentID, UserID: derefStr(r.UserID), Channel: derefStr(r.Channel),
		ChatID: derefStr(r.ChatID), Seq: r.Seq, ItemType: r.ItemType,
		Status: derefStr(r.Status), Title: derefStr(r.Title), Preview: derefStr(r.Preview),
		Content: r.Content, ToolName: derefStr(r.ToolName), ToolCallID: derefStr(r.ToolCallID),
		TraceID: r.TraceID, SpanID: r.SpanID, Metadata: r.Metadata, CreatedAt: r.CreatedAt,
	}
}

// PGRunStore implements store.RunsStore backed by PostgreSQL.
type PGRunStore struct {
	db *sql.DB
}

func NewPGRunStore(db *sql.DB) *PGRunStore {
	return &PGRunStore{db: db}
}

func (s *PGRunStore) CreateRun(ctx context.Context, run *store.AgentRun) error {
	if run.ID == uuid.Nil {
		run.ID = store.GenNewID()
	}
	if run.RunID == "" {
		return fmt.Errorf("create run: run_id required")
	}
	if run.Status == "" {
		run.Status = store.AgentRunStatusPending
	}
	if run.Attempt == 0 {
		run.Attempt = 1
	}
	now := time.Now()
	if run.StartedAt.IsZero() {
		run.StartedAt = now
	}
	if run.CreatedAt.IsZero() {
		run.CreatedAt = now
	}
	if run.UpdatedAt.IsZero() {
		run.UpdatedAt = now
	}
	if run.HeartbeatAt.IsZero() {
		run.HeartbeatAt = run.StartedAt
	}
	run.TenantID = tenantIDForInsert(ctx)
	metadata := run.Metadata
	if len(metadata) == 0 {
		metadata = []byte(`{}`)
	}
	checkpoint := run.Checkpoint
	if len(checkpoint) == 0 {
		checkpoint = nil
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_runs
		 (id, tenant_id, run_id, session_key, agent_id, user_id, channel, chat_id,
		  status, attempt, checkpoint, heartbeat_at, started_at, completed_at, error,
		  metadata, updated_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8,
		  $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		 ON CONFLICT (tenant_id, run_id) DO UPDATE SET
		  session_key = EXCLUDED.session_key,
		  agent_id = EXCLUDED.agent_id,
		  user_id = EXCLUDED.user_id,
		  channel = EXCLUDED.channel,
		  chat_id = EXCLUDED.chat_id,
		  status = EXCLUDED.status,
		  attempt = EXCLUDED.attempt,
		  checkpoint = EXCLUDED.checkpoint,
		  heartbeat_at = EXCLUDED.heartbeat_at,
		  started_at = EXCLUDED.started_at,
		  completed_at = EXCLUDED.completed_at,
		  error = EXCLUDED.error,
		  metadata = EXCLUDED.metadata,
		  updated_at = EXCLUDED.updated_at`,
		run.ID, run.TenantID, run.RunID, run.SessionKey, nilUUID(run.AgentID), nilStr(run.UserID),
		nilStr(run.Channel), nilStr(run.ChatID), run.Status, run.Attempt, checkpoint,
		run.HeartbeatAt, run.StartedAt, nilTime(run.CompletedAt), nilStr(run.Error),
		jsonOrEmpty(metadata), run.UpdatedAt, run.CreatedAt,
	)
	return err
}

func (s *PGRunStore) UpdateRunStatus(ctx context.Context, runID, status string) error {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE agent_runs SET status = $3, updated_at = $4
		 WHERE run_id = $1 AND tenant_id = $2`,
		runID, tid, status, time.Now())
	return err
}

func (s *PGRunStore) UpdateRunTerminal(ctx context.Context, runID, status, errMsg string, completedAt time.Time) error {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE agent_runs SET status = $3, error = $4, completed_at = $5, updated_at = $6
		 WHERE run_id = $1 AND tenant_id = $2`,
		runID, tid, status, nilStr(errMsg), completedAt, time.Now())
	return err
}

func (s *PGRunStore) TouchHeartbeat(ctx context.Context, runID string) error {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE agent_runs SET heartbeat_at = $3, updated_at = $3
		 WHERE run_id = $1 AND tenant_id = $2`,
		runID, tid, time.Now())
	return err
}

func (s *PGRunStore) GetRun(ctx context.Context, runID string) (*store.AgentRun, error) {
	where, args := buildRunWhere(ctx, runID)
	q := `SELECT id, tenant_id, run_id, session_key, agent_id, user_id, channel, chat_id,
		 status, attempt, checkpoint, heartbeat_at, started_at, completed_at, error,
		 COALESCE(metadata, '{}'::jsonb) AS metadata, updated_at, created_at
		 FROM agent_runs` + where
	var row agentRunRow
	if err := pkgSqlxDB.GetContext(ctx, &row, q, args...); err != nil {
		return nil, err
	}
	run := row.toStore()
	return &run, nil
}

func (s *PGRunStore) ListRuns(ctx context.Context, opts store.RunListOpts) ([]store.AgentRun, error) {
	where, args := buildRunListWhere(ctx, opts)
	limit := opts.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT id, tenant_id, run_id, session_key, agent_id, user_id, channel, chat_id,
		 status, attempt, checkpoint, heartbeat_at, started_at, completed_at, error,
		 COALESCE(metadata, '{}'::jsonb) AS metadata, updated_at, created_at
		 FROM agent_runs` + where +
		` ORDER BY created_at DESC` +
		fmt.Sprintf(" OFFSET %d LIMIT %d", opts.Offset, limit)

	var rows []agentRunRow
	if err := pkgSqlxDB.SelectContext(ctx, &rows, q, args...); err != nil {
		return nil, err
	}
	runs := make([]store.AgentRun, len(rows))
	for i, row := range rows {
		runs[i] = row.toStore()
	}
	return runs, nil
}

// RecoverStaleRuns marks runs whose heartbeat has not advanced within staleAfter
// as failed. Cross-tenant (startup + periodic).
func (s *PGRunStore) RecoverStaleRuns(ctx context.Context, staleAfter time.Duration) (int64, error) {
	deadline := time.Now().Add(-staleAfter)
	res, err := s.db.ExecContext(ctx,
		`UPDATE agent_runs SET status = $1, error = $2,
		        completed_at = COALESCE(completed_at, $3), updated_at = $3
		 WHERE status IN ('pending', 'running', 'compacting')
		   AND heartbeat_at < $4`,
		store.AgentRunStatusFailed, "run stalled: heartbeat expired", deadline, deadline)
	if err != nil {
		return 0, fmt.Errorf("recover stale runs: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// buildRunWhere scopes a single-record read. Fails closed (WHERE 1=0) when a
// tenant ID is required but absent from the context.
func buildRunWhere(ctx context.Context, runID string) (string, []any) {
	var conditions []string
	var args []any
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return " WHERE 1=0", nil
		}
		conditions = append(conditions, "tenant_id = $1")
		args = append(args, tenantID)
		conditions = append(conditions, "run_id = $2")
		args = append(args, runID)
	} else {
		conditions = append(conditions, "run_id = $1")
		args = append(args, runID)
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

// buildRunListWhere scopes a run-record list read. Fails closed (WHERE 1=0)
// when a tenant ID is required but absent from the context.
func buildRunListWhere(ctx context.Context, opts store.RunListOpts) (string, []any) {
	var conditions []string
	var args []any
	argIdx := 1
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return " WHERE 1=0", nil
		}
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIdx))
		args = append(args, tenantID)
		argIdx++
	}
	if opts.RunID != "" {
		conditions = append(conditions, fmt.Sprintf("run_id = $%d", argIdx))
		args = append(args, opts.RunID)
		argIdx++
	}
	if opts.SessionKey != "" {
		conditions = append(conditions, fmt.Sprintf("session_key = $%d", argIdx))
		args = append(args, opts.SessionKey)
		argIdx++
	}
	if opts.Status != "" {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, opts.Status)
		argIdx++
	}
	if len(conditions) == 0 {
		return " WHERE 1=0", nil
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

type agentRunRow struct {
	ID          uuid.UUID       `db:"id"`
	TenantID    uuid.UUID       `db:"tenant_id"`
	RunID       string          `db:"run_id"`
	SessionKey  string          `db:"session_key"`
	AgentID     *uuid.UUID      `db:"agent_id"`
	UserID      *string         `db:"user_id"`
	Channel     *string         `db:"channel"`
	ChatID      *string         `db:"chat_id"`
	Status      string          `db:"status"`
	Attempt     int             `db:"attempt"`
	Checkpoint  []byte          `db:"checkpoint"`
	HeartbeatAt time.Time       `db:"heartbeat_at"`
	StartedAt   time.Time       `db:"started_at"`
	CompletedAt *time.Time      `db:"completed_at"`
	Error       *string         `db:"error"`
	Metadata    json.RawMessage `db:"metadata"`
	UpdatedAt   time.Time       `db:"updated_at"`
	CreatedAt   time.Time       `db:"created_at"`
}

func (r agentRunRow) toStore() store.AgentRun {
	var checkpoint json.RawMessage
	if len(r.Checkpoint) > 0 {
		checkpoint = r.Checkpoint
	}
	return store.AgentRun{
		ID:          r.ID,
		TenantID:    r.TenantID,
		RunID:       r.RunID,
		SessionKey:  r.SessionKey,
		AgentID:     r.AgentID,
		UserID:      derefStr(r.UserID),
		Channel:     derefStr(r.Channel),
		ChatID:      derefStr(r.ChatID),
		Status:      r.Status,
		Attempt:     r.Attempt,
		Checkpoint:  checkpoint,
		HeartbeatAt: r.HeartbeatAt,
		StartedAt:   r.StartedAt,
		CompletedAt: r.CompletedAt,
		Error:       derefStr(r.Error),
		Metadata:    r.Metadata,
		UpdatedAt:   r.UpdatedAt,
		CreatedAt:   r.CreatedAt,
	}
}

// interruptedRunMetadata marks a backfilled terminal status so it is
// distinguishable from a genuine agent failure in the timeline.
var interruptedRunMetadata = json.RawMessage(`{"event_type":"run.failed","interrupted":true,"reason":"server_restart"}`)

const interruptedRunPreview = "interrupted: gateway stopped while this run was in progress"

// RecoverInterruptedRuns appends a terminal failed run.status item to every run
// that has a "started" run.status but no terminal sibling — i.e. runs killed
// mid-execution by a previous gateway stop, which would otherwise stay
// "running" forever. Cross-tenant (startup reconciliation); see the interface doc.
func (s *PGRunTimelineStore) RecoverInterruptedRuns(ctx context.Context) (int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT st.tenant_id, st.run_id, st.session_key, st.agent_id, st.user_id, st.channel, st.chat_id, agg.max_seq
		FROM (
			SELECT run_id, MAX(seq) AS max_seq,
			       bool_or(item_type = 'run.status' AND status = 'started') AS has_start,
			       bool_or(item_type = 'run.status' AND status IN ('completed', 'failed', 'cancelled')) AS has_term
			FROM run_timeline_items
			GROUP BY run_id
		) agg
		JOIN run_timeline_items st
		  ON st.run_id = agg.run_id AND st.item_type = 'run.status' AND st.status = 'started'
		WHERE agg.has_start AND NOT agg.has_term`)
	if err != nil {
		return 0, fmt.Errorf("list interrupted runs: %w", err)
	}
	orphans, err := scanInterruptedRuns(rows)
	if err != nil {
		return 0, err
	}

	var recovered int64
	for i := range orphans {
		item := &orphans[i]
		if err := s.AppendRunTimelineItem(store.WithTenantID(ctx, item.TenantID), item); err != nil {
			return recovered, fmt.Errorf("append interrupted terminal for run %s: %w", item.RunID, err)
		}
		recovered++
	}
	return recovered, nil
}

// scanInterruptedRuns reads orphaned-run rows and pre-builds the terminal failed
// item to append for each. Rows are fully drained and closed before returning so
// the caller can issue inserts on the same pool without cursor contention.
func scanInterruptedRuns(rows *sql.Rows) ([]store.RunTimelineItem, error) {
	defer rows.Close()
	var items []store.RunTimelineItem
	for rows.Next() {
		var (
			tenantID                uuid.UUID
			runID, sessionKey       string
			agentID                 uuid.NullUUID
			userID, channel, chatID sql.NullString
			maxSeq                  int
		)
		if err := rows.Scan(&tenantID, &runID, &sessionKey, &agentID, &userID, &channel, &chatID, &maxSeq); err != nil {
			return nil, fmt.Errorf("scan interrupted run: %w", err)
		}
		item := store.RunTimelineItem{
			TenantID:   tenantID,
			RunID:      runID,
			SessionKey: sessionKey,
			UserID:     userID.String,
			Channel:    channel.String,
			ChatID:     chatID.String,
			Seq:        maxSeq + 1,
			ItemType:   store.RunTimelineItemTypeRunStatus,
			Status:     store.RunTimelineStatusFailed,
			Title:      "Run failed",
			Preview:    interruptedRunPreview,
			Metadata:   interruptedRunMetadata,
		}
		if agentID.Valid {
			id := agentID.UUID
			item.AgentID = &id
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
