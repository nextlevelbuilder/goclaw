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
