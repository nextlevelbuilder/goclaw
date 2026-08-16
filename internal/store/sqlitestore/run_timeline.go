//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteRunTimelineStore implements store.RunTimelineStore backed by SQLite.
type SQLiteRunTimelineStore struct {
	db *sql.DB
}

func NewSQLiteRunTimelineStore(db *sql.DB) *SQLiteRunTimelineStore {
	return &SQLiteRunTimelineStore{db: db}
}

func (s *SQLiteRunTimelineStore) AppendRunTimelineItem(ctx context.Context, item *store.RunTimelineItem) error {
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
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (tenant_id, run_id, seq) DO UPDATE SET
		  session_key = excluded.session_key,
		  agent_id = excluded.agent_id,
		  user_id = excluded.user_id,
		  channel = excluded.channel,
		  chat_id = excluded.chat_id,
		  item_type = excluded.item_type,
		  status = excluded.status,
		  title = excluded.title,
		  preview = excluded.preview,
		  content = '',
		  tool_name = excluded.tool_name,
		  tool_call_id = excluded.tool_call_id,
		  trace_id = excluded.trace_id,
		  span_id = excluded.span_id,
		  metadata = excluded.metadata,
		  created_at = excluded.created_at`,
		item.ID, tenantID, item.RunID, item.SessionKey, nilUUID(item.AgentID), nilStr(item.UserID),
		nilStr(item.Channel), nilStr(item.ChatID), item.Seq, item.ItemType, nilStr(item.Status),
		nilStr(item.Title), nilStr(item.Preview), "", nilStr(item.ToolName), nilStr(item.ToolCallID),
		nilUUID(item.TraceID), nilUUID(item.SpanID), string(metadata), item.CreatedAt,
	)
	if err == nil {
		item.Content = ""
	}
	return err
}

func (s *SQLiteRunTimelineStore) ListRunTimelineItems(ctx context.Context, opts store.RunTimelineListOpts) ([]store.RunTimelineItem, error) {
	where, args := buildRunTimelineWhere(ctx, opts)
	limit := opts.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	q := `SELECT id, tenant_id, run_id, session_key, agent_id, user_id, channel, chat_id, seq,
		 item_type, status, title, preview, COALESCE(content, '') AS content, tool_name, tool_call_id,
		 trace_id, span_id, COALESCE(metadata, '{}') AS metadata, created_at
		 FROM run_timeline_items` + where +
		runTimelineOrderBy(opts) +
		fmt.Sprintf(" LIMIT %d OFFSET %d", limit, opts.Offset)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRunTimelineRows(rows)
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
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return " WHERE 1=0", nil
		}
		conditions = append(conditions, "tenant_id = ?")
		args = append(args, tenantID)
	}
	if opts.RunID != "" {
		conditions = append(conditions, "run_id = ?")
		args = append(args, opts.RunID)
	}
	if opts.AfterSeq > 0 {
		conditions = append(conditions, "seq > ?")
		args = append(args, opts.AfterSeq)
	}
	if opts.SessionKey != "" {
		conditions = append(conditions, "session_key = ?")
		args = append(args, opts.SessionKey)
	}
	if len(conditions) == 0 {
		return " WHERE 1=0", nil
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

func scanRunTimelineRows(rows *sql.Rows) ([]store.RunTimelineItem, error) {
	var items []store.RunTimelineItem
	for rows.Next() {
		var item store.RunTimelineItem
		var agentID, traceID, spanID *uuid.UUID
		var userID, channel, chatID, status, title, preview, toolName, toolCallID sql.NullString
		var metadata string
		var createdAt sqliteTime
		if err := rows.Scan(&item.ID, &item.TenantID, &item.RunID, &item.SessionKey, &agentID,
			&userID, &channel, &chatID, &item.Seq, &item.ItemType, &status, &title, &preview,
			&item.Content, &toolName, &toolCallID, &traceID, &spanID, &metadata, &createdAt); err != nil {
			return nil, err
		}
		item.AgentID = agentID
		item.TraceID = traceID
		item.SpanID = spanID
		item.UserID = userID.String
		item.Channel = channel.String
		item.ChatID = chatID.String
		item.Status = status.String
		item.Title = title.String
		item.Preview = preview.String
		item.ToolName = toolName.String
		item.ToolCallID = toolCallID.String
		item.Metadata = []byte(metadata)
		item.CreatedAt = createdAt.Time
		items = append(items, item)
	}
	return items, rows.Err()
}

// SQLiteRunStore implements store.RunsStore backed by SQLite.
type SQLiteRunStore struct {
	db *sql.DB
}

func NewSQLiteRunStore(db *sql.DB) *SQLiteRunStore {
	return &SQLiteRunStore{db: db}
}

func (s *SQLiteRunStore) CreateRun(ctx context.Context, run *store.AgentRun) error {
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
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (tenant_id, run_id) DO UPDATE SET
		  session_key = excluded.session_key,
		  agent_id = excluded.agent_id,
		  user_id = excluded.user_id,
		  channel = excluded.channel,
		  chat_id = excluded.chat_id,
		  status = excluded.status,
		  attempt = excluded.attempt,
		  checkpoint = excluded.checkpoint,
		  heartbeat_at = excluded.heartbeat_at,
		  started_at = excluded.started_at,
		  completed_at = excluded.completed_at,
		  error = excluded.error,
		  metadata = excluded.metadata,
		  updated_at = excluded.updated_at`,
		run.ID, run.TenantID, run.RunID, run.SessionKey, nilUUID(run.AgentID), nilStr(run.UserID),
		nilStr(run.Channel), nilStr(run.ChatID), run.Status, run.Attempt, checkpoint,
		run.HeartbeatAt, run.StartedAt, nilTime(run.CompletedAt), nilStr(run.Error),
		string(metadata), run.UpdatedAt, run.CreatedAt,
	)
	return err
}

func (s *SQLiteRunStore) UpdateRunStatus(ctx context.Context, runID, status string) error {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE agent_runs SET status = ?, updated_at = ?
		 WHERE run_id = ? AND tenant_id = ?`,
		status, time.Now(), runID, tid)
	return err
}

func (s *SQLiteRunStore) UpdateRunTerminal(ctx context.Context, runID, status, errMsg string, completedAt time.Time) error {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE agent_runs SET status = ?, error = ?, completed_at = ?, updated_at = ?
		 WHERE run_id = ? AND tenant_id = ?`,
		status, nilStr(errMsg), completedAt, time.Now(), runID, tid)
	return err
}

func (s *SQLiteRunStore) TouchHeartbeat(ctx context.Context, runID string) error {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE agent_runs SET heartbeat_at = ?, updated_at = ?
		 WHERE run_id = ? AND tenant_id = ?`,
		time.Now(), time.Now(), runID, tid)
	return err
}

func (s *SQLiteRunStore) GetRun(ctx context.Context, runID string) (*store.AgentRun, error) {
	where, args := buildSQLiteRunWhere(ctx, runID)
	q := `SELECT id, tenant_id, run_id, session_key, agent_id, user_id, channel, chat_id,
		 status, attempt, checkpoint, heartbeat_at, started_at, completed_at, error,
		 COALESCE(metadata, '{}') AS metadata, updated_at, created_at
		 FROM agent_runs` + where
	row := s.db.QueryRowContext(ctx, q, args...)
	var r agentRunRow
	if err := scanAgentRunRow(row.Scan, &r); err != nil {
		return nil, err
	}
	run := r.toStore()
	return &run, nil
}

func (s *SQLiteRunStore) ListRuns(ctx context.Context, opts store.RunListOpts) ([]store.AgentRun, error) {
	where, args := buildSQLiteRunListWhere(ctx, opts)
	limit := opts.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT id, tenant_id, run_id, session_key, agent_id, user_id, channel, chat_id,
		 status, attempt, checkpoint, heartbeat_at, started_at, completed_at, error,
		 COALESCE(metadata, '{}') AS metadata, updated_at, created_at
		 FROM agent_runs` + where +
		` ORDER BY created_at DESC, rowid DESC` +
		fmt.Sprintf(" LIMIT %d OFFSET %d", limit, opts.Offset)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []store.AgentRun
	for rows.Next() {
		var r agentRunRow
		if err := scanAgentRunRow(rows.Scan, &r); err != nil {
			return nil, err
		}
		runs = append(runs, r.toStore())
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if runs == nil {
		runs = []store.AgentRun{}
	}
	return runs, nil
}

// scanAgentRunRow scans one agent_runs row into the row struct, handling
// SQLite's text timestamps via sqliteTime/nullSqliteTime.
func scanAgentRunRow(scan func(dest ...any) error, r *agentRunRow) error {
	return scan(
		&r.ID, &r.TenantID, &r.RunID, &r.SessionKey, &r.AgentID,
		&r.UserID, &r.Channel, &r.ChatID, &r.Status, &r.Attempt, &r.Checkpoint,
		&r.HeartbeatAt, &r.StartedAt, &r.CompletedAt, &r.Error,
		&r.Metadata, &r.UpdatedAt, &r.CreatedAt,
	)
}

// RecoverStaleRuns marks runs whose heartbeat has not advanced within staleAfter
// as failed. Cross-tenant (startup + periodic).
func (s *SQLiteRunStore) RecoverStaleRuns(ctx context.Context, staleAfter time.Duration) (int64, error) {
	deadline := time.Now().Add(-staleAfter)
	res, err := s.db.ExecContext(ctx,
		`UPDATE agent_runs SET status = ?, error = ?,
		        completed_at = COALESCE(completed_at, ?), updated_at = ?
		 WHERE status IN ('pending', 'running', 'compacting')
		   AND heartbeat_at < ?`,
		store.AgentRunStatusFailed, "run stalled: heartbeat expired", deadline, deadline, deadline)
	if err != nil {
		return 0, fmt.Errorf("recover stale runs: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// buildSQLiteRunWhere scopes a single-record read. Fails closed (WHERE 1=0) when
// a tenant ID is required but absent from the context.
func buildSQLiteRunWhere(ctx context.Context, runID string) (string, []any) {
	var conditions []string
	var args []any
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return " WHERE 1=0", nil
		}
		conditions = append(conditions, "tenant_id = ?")
		args = append(args, tenantID)
		conditions = append(conditions, "run_id = ?")
		args = append(args, runID)
	} else {
		conditions = append(conditions, "run_id = ?")
		args = append(args, runID)
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

// buildSQLiteRunListWhere scopes a run-record list read. Fails closed (WHERE 1=0)
// when a tenant ID is required but absent from the context.
func buildSQLiteRunListWhere(ctx context.Context, opts store.RunListOpts) (string, []any) {
	var conditions []string
	var args []any
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return " WHERE 1=0", nil
		}
		conditions = append(conditions, "tenant_id = ?")
		args = append(args, tenantID)
	}
	if opts.RunID != "" {
		conditions = append(conditions, "run_id = ?")
		args = append(args, opts.RunID)
	}
	if opts.SessionKey != "" {
		conditions = append(conditions, "session_key = ?")
		args = append(args, opts.SessionKey)
	}
	if opts.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, opts.Status)
	}
	if len(conditions) == 0 {
		return " WHERE 1=0", nil
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

const interruptedRunPreview = "interrupted: gateway stopped while this run was in progress"

// interruptedRunMetadata marks a backfilled terminal status so it is
// distinguishable from a genuine agent failure in the timeline.
var interruptedRunMetadata = []byte(`{"event_type":"run.failed","interrupted":true,"reason":"server_restart"}`)

// RecoverInterruptedRuns appends a terminal failed run.status item to every run
// that has a "started" run.status but no terminal sibling — i.e. runs killed
// mid-execution by a previous gateway stop, which would otherwise stay
// "running" forever. Cross-tenant (startup reconciliation); see the interface doc.
func (s *SQLiteRunTimelineStore) RecoverInterruptedRuns(ctx context.Context) (int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT st.tenant_id, st.run_id, st.session_key, st.agent_id, st.user_id, st.channel, st.chat_id, agg.max_seq
		FROM (
			SELECT run_id, MAX(seq) AS max_seq,
			       MAX(item_type = 'run.status' AND status = 'started') AS has_start,
			       MAX(item_type = 'run.status' AND status IN ('completed', 'failed', 'cancelled')) AS has_term
			FROM run_timeline_items
			GROUP BY run_id
		) agg
		JOIN run_timeline_items st
		  ON st.run_id = agg.run_id AND st.item_type = 'run.status' AND st.status = 'started'
		WHERE agg.has_start = 1 AND IFNULL(agg.has_term, 0) = 0`)
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
// the caller can issue inserts on the same connection without cursor contention.
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
