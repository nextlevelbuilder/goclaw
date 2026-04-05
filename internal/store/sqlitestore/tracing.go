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

// SQLiteTracingStore implements store.TracingStore backed by SQLite.
type SQLiteTracingStore struct {
	db *sql.DB
}

func NewSQLiteTracingStore(db *sql.DB) *SQLiteTracingStore {
	return &SQLiteTracingStore{db: db}
}

func (s *SQLiteTracingStore) CreateTrace(ctx context.Context, trace *store.TraceData) error {
	if trace.ID == uuid.Nil {
		trace.ID = store.GenNewID()
	}
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO traces (id, parent_trace_id, agent_id, user_id, session_key, run_id, start_time, end_time,
		 duration_ms, name, channel, input_preview, output_preview,
		 total_input_tokens, total_output_tokens, total_cost, span_count, llm_call_count, tool_call_count,
		 status, error, metadata, tags, team_id, created_at, tenant_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		trace.ID, nilUUID(trace.ParentTraceID), nilUUID(trace.AgentID), nilStr(trace.UserID), nilStr(trace.SessionKey),
		nilStr(trace.RunID), trace.StartTime, nilTime(trace.EndTime),
		nilInt(trace.DurationMS), nilStr(trace.Name), nilStr(trace.Channel),
		nilStr(trace.InputPreview), nilStr(trace.OutputPreview),
		trace.TotalInputTokens, trace.TotalOutputTokens, trace.TotalCost, trace.SpanCount, trace.LLMCallCount, trace.ToolCallCount,
		trace.Status, nilStr(trace.Error), jsonOrEmpty(trace.Metadata), jsonStringArray(trace.Tags), nilUUID(trace.TeamID), trace.CreatedAt, tenantID,
	)
	return err
}

func (s *SQLiteTracingStore) UpdateTrace(ctx context.Context, traceID uuid.UUID, updates map[string]any) error {
	if store.IsCrossTenant(ctx) {
		return execMapUpdate(ctx, s.db, "traces", traceID, updates)
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required for update")
	}
	return execMapUpdateWhereTenant(ctx, s.db, "traces", updates, traceID, tid)
}

func (s *SQLiteTracingStore) GetTrace(ctx context.Context, traceID uuid.UUID) (*store.TraceData, error) {
	query := `SELECT id, parent_trace_id, agent_id, user_id, session_key, run_id, start_time, end_time,
		 duration_ms, name, channel, input_preview, output_preview,
		 total_input_tokens, total_output_tokens, COALESCE(total_cost, 0), span_count, llm_call_count, tool_call_count,
		 status, error, metadata, tags, team_id, created_at
		 FROM traces WHERE id = ?`
	qArgs := []any{traceID}
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return nil, sql.ErrNoRows
		}
		query += ` AND tenant_id = ?`
		qArgs = append(qArgs, tenantID)
	}
	return scanTraceRow(s.db.QueryRowContext(ctx, query, qArgs...))
}

func buildTraceWhere(ctx context.Context, opts store.TraceListOpts) (string, []any) {
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
	if opts.AgentID != nil {
		conditions = append(conditions, "agent_id = ?")
		args = append(args, *opts.AgentID)
	}
	if opts.UserID != "" {
		conditions = append(conditions, "user_id = ?")
		args = append(args, opts.UserID)
	}
	if opts.SessionKey != "" {
		conditions = append(conditions, "session_key = ?")
		args = append(args, opts.SessionKey)
	}
	if opts.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, opts.Status)
	}
	if opts.Channel != "" {
		conditions = append(conditions, "channel = ?")
		args = append(args, opts.Channel)
	}

	if len(conditions) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

func (s *SQLiteTracingStore) CountTraces(ctx context.Context, opts store.TraceListOpts) (int, error) {
	where, args := buildTraceWhere(ctx, opts)
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM traces"+where, args...).Scan(&count)
	return count, err
}

func (s *SQLiteTracingStore) ListTraces(ctx context.Context, opts store.TraceListOpts) ([]store.TraceData, error) {
	where, args := buildTraceWhere(ctx, opts)
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT id, parent_trace_id, agent_id, user_id, session_key, run_id, start_time, end_time,
		 duration_ms, name, channel, input_preview, output_preview,
		 total_input_tokens, total_output_tokens, COALESCE(total_cost, 0), span_count, llm_call_count, tool_call_count,
		 status, error, metadata, tags, team_id, created_at
		 FROM traces` + where +
		fmt.Sprintf(" ORDER BY created_at DESC LIMIT %d OFFSET %d", limit, opts.Offset)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTraceRows(rows)
}

func (s *SQLiteTracingStore) ListChildTraces(ctx context.Context, parentTraceID uuid.UUID) ([]store.TraceData, error) {
	q := `SELECT id, parent_trace_id, agent_id, user_id, session_key, run_id, start_time, end_time,
		 duration_ms, name, channel, input_preview, output_preview,
		 total_input_tokens, total_output_tokens, COALESCE(total_cost, 0), span_count, llm_call_count, tool_call_count,
		 status, error, metadata, tags, team_id, created_at
		 FROM traces WHERE parent_trace_id = ?`
	qArgs := []any{parentTraceID}
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return nil, fmt.Errorf("tenant_id required")
		}
		q += " AND tenant_id = ?"
		qArgs = append(qArgs, tid)
	}
	q += " ORDER BY created_at"
	rows, err := s.db.QueryContext(ctx, q, qArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTraceRows(rows)
}

func (s *SQLiteTracingStore) GetMonthlyAgentCost(ctx context.Context, agentID uuid.UUID, year int, month time.Month) (float64, error) {
	start := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	q := `SELECT COALESCE(SUM(total_cost), 0) FROM traces
		 WHERE agent_id = ? AND created_at >= ? AND created_at < ? AND parent_trace_id IS NULL`
	qArgs := []any{agentID, start, end}
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid != uuid.Nil {
			q += " AND tenant_id = ?"
			qArgs = append(qArgs, tid)
		}
	}
	var cost float64
	err := s.db.QueryRowContext(ctx, q, qArgs...).Scan(&cost)
	return cost, err
}

func (s *SQLiteTracingStore) GetCostSummary(ctx context.Context, opts store.CostSummaryOpts) ([]store.CostSummaryRow, error) {
	var conditions []string
	var args []any

	conditions = append(conditions, "parent_trace_id IS NULL")

	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID != uuid.Nil {
			conditions = append(conditions, "tenant_id = ?")
			args = append(args, tenantID)
		}
	}
	if opts.AgentID != nil {
		conditions = append(conditions, "agent_id = ?")
		args = append(args, *opts.AgentID)
	}
	if opts.From != nil {
		conditions = append(conditions, "created_at >= ?")
		args = append(args, *opts.From)
	}
	if opts.To != nil {
		conditions = append(conditions, "created_at < ?")
		args = append(args, *opts.To)
	}

	where := " WHERE " + strings.Join(conditions, " AND ")
	q := `SELECT agent_id, COALESCE(SUM(total_cost), 0), COALESCE(SUM(total_input_tokens), 0),
		  COALESCE(SUM(total_output_tokens), 0), COUNT(*)
		  FROM traces` + where + ` GROUP BY agent_id ORDER BY SUM(total_cost) DESC`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.CostSummaryRow
	for rows.Next() {
		var r store.CostSummaryRow
		var agentID *uuid.UUID
		if err := rows.Scan(&agentID, &r.TotalCost, &r.TotalInputTokens, &r.TotalOutputTokens, &r.TraceCount); err != nil {
			continue
		}
		r.AgentID = agentID
		result = append(result, r)
	}
	return result, rows.Err()
}

// DeleteTracesOlderThan deletes traces and their spans older than cutoff.
func (s *SQLiteTracingStore) DeleteTracesOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	// Delete spans belonging to old traces.
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM spans WHERE trace_id IN (SELECT id FROM traces WHERE created_at < ?)`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete old spans: %w", err)
	}

	res, err := s.db.ExecContext(ctx, `DELETE FROM traces WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete old traces: %w", err)
	}
	return res.RowsAffected()
}

// ListCodexPoolSpans is not supported in SQLite (Codex pool is a standard-edition feature).
func (s *SQLiteTracingStore) ListCodexPoolSpans(_ context.Context, _, _ uuid.UUID, _ []string, _ int) ([]store.CodexPoolSpan, error) {
	return nil, nil
}

// ListCodexPoolSpansByProviders is not supported in SQLite (Codex pool is a standard-edition feature).
func (s *SQLiteTracingStore) ListCodexPoolSpansByProviders(_ context.Context, _ uuid.UUID, _ []string, _ int) ([]store.CodexPoolProviderSpan, error) {
	return nil, nil
}
