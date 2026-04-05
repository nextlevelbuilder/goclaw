package pg

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGTracingStore implements store.TracingStore backed by Postgres.
type PGTracingStore struct {
	db *sql.DB
}

func NewPGTracingStore(db *sql.DB) *PGTracingStore {
	return &PGTracingStore{db: db}
}

func (s *PGTracingStore) CreateTrace(ctx context.Context, trace *store.TraceData) error {
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
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26)`,
		trace.ID, nilUUID(trace.ParentTraceID), nilUUID(trace.AgentID), nilStr(trace.UserID), nilStr(trace.SessionKey),
		nilStr(trace.RunID), trace.StartTime, nilTime(trace.EndTime),
		nilInt(trace.DurationMS), nilStr(trace.Name), nilStr(trace.Channel),
		nilStr(trace.InputPreview), nilStr(trace.OutputPreview),
		trace.TotalInputTokens, trace.TotalOutputTokens, trace.TotalCost, trace.SpanCount, trace.LLMCallCount, trace.ToolCallCount,
		trace.Status, nilStr(trace.Error), jsonOrEmpty(trace.Metadata), pqStringArray(trace.Tags), nilUUID(trace.TeamID), trace.CreatedAt, tenantID,
	)
	return err
}

func (s *PGTracingStore) UpdateTrace(ctx context.Context, traceID uuid.UUID, updates map[string]any) error {
	if store.IsCrossTenant(ctx) {
		return execMapUpdate(ctx, s.db, "traces", traceID, updates)
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required for update")
	}
	return execMapUpdateWhereTenant(ctx, s.db, "traces", updates, traceID, tid)
}

func (s *PGTracingStore) GetTrace(ctx context.Context, traceID uuid.UUID) (*store.TraceData, error) {
	var d store.TraceData
	var parentTraceID, agentID, teamID *uuid.UUID
	var userID, sessionKey, runID, name, channel, inputPreview, outputPreview, errStr *string
	var endTime *time.Time
	var durationMS *int
	var metadata *[]byte
	var tags []byte

	query := `SELECT id, parent_trace_id, agent_id, user_id, session_key, run_id, start_time, end_time,
		 duration_ms, name, channel, input_preview, output_preview,
		 total_input_tokens, total_output_tokens, COALESCE(total_cost, 0), span_count, llm_call_count, tool_call_count,
		 status, error, metadata, tags, team_id, created_at
		 FROM traces WHERE id = $1`
	qArgs := []any{traceID}
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return nil, sql.ErrNoRows
		}
		query += ` AND tenant_id = $2`
		qArgs = append(qArgs, tenantID)
	}

	err := s.db.QueryRowContext(ctx, query, qArgs...).Scan(&d.ID, &parentTraceID, &agentID, &userID, &sessionKey, &runID, &d.StartTime, &endTime,
		&durationMS, &name, &channel, &inputPreview, &outputPreview,
		&d.TotalInputTokens, &d.TotalOutputTokens, &d.TotalCost, &d.SpanCount, &d.LLMCallCount, &d.ToolCallCount,
		&d.Status, &errStr, &metadata, &tags, &teamID, &d.CreatedAt)
	if err != nil {
		return nil, err
	}

	d.ParentTraceID = parentTraceID
	d.AgentID = agentID
	d.TeamID = teamID
	d.UserID = derefStr(userID)
	d.SessionKey = derefStr(sessionKey)
	d.RunID = derefStr(runID)
	d.EndTime = endTime
	if durationMS != nil {
		d.DurationMS = *durationMS
	}
	d.Name = derefStr(name)
	d.Channel = derefStr(channel)
	d.InputPreview = derefStr(inputPreview)
	d.OutputPreview = derefStr(outputPreview)
	d.Error = derefStr(errStr)
	if metadata != nil {
		d.Metadata = *metadata
	}
	scanStringArray(tags, &d.Tags)
	return &d, nil
}

func buildTraceWhere(ctx context.Context, opts store.TraceListOpts) (string, []any) {
	var conditions []string
	var args []any
	argIdx := 1

	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return " WHERE 1=0", nil // fail-closed: no tenant = no results
		}
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIdx))
		args = append(args, tenantID)
		argIdx++
	}

	if opts.AgentID != nil {
		conditions = append(conditions, fmt.Sprintf("agent_id = $%d", argIdx))
		args = append(args, *opts.AgentID)
		argIdx++
	}
	if opts.UserID != "" {
		conditions = append(conditions, fmt.Sprintf("user_id = $%d", argIdx))
		args = append(args, opts.UserID)
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
	if opts.Channel != "" {
		conditions = append(conditions, fmt.Sprintf("channel = $%d", argIdx))
		args = append(args, opts.Channel)
		argIdx++
	}

	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}
	return where, args
}

func (s *PGTracingStore) CountTraces(ctx context.Context, opts store.TraceListOpts) (int, error) {
	where, args := buildTraceWhere(ctx, opts)
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM traces"+where, args...).Scan(&count)
	return count, err
}

func (s *PGTracingStore) ListTraces(ctx context.Context, opts store.TraceListOpts) ([]store.TraceData, error) {
	where, args := buildTraceWhere(ctx, opts)

	q := `SELECT id, parent_trace_id, agent_id, user_id, session_key, run_id, start_time, end_time,
		 duration_ms, name, channel, input_preview, output_preview,
		 total_input_tokens, total_output_tokens, COALESCE(total_cost, 0), span_count, llm_call_count, tool_call_count,
		 status, error, metadata, tags, team_id, created_at
		 FROM traces` + where

	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	q += fmt.Sprintf(" ORDER BY created_at DESC OFFSET %d LIMIT %d", opts.Offset, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.TraceData
	for rows.Next() {
		var d store.TraceData
		var parentTraceID, agentID, teamID *uuid.UUID
		var userID, sessionKey, runID, name, channel, inputPreview, outputPreview, errStr *string
		var endTime *time.Time
		var durationMS *int
		var metadata *[]byte
		var tags []byte

		if err := rows.Scan(&d.ID, &parentTraceID, &agentID, &userID, &sessionKey, &runID, &d.StartTime, &endTime,
			&durationMS, &name, &channel, &inputPreview, &outputPreview,
			&d.TotalInputTokens, &d.TotalOutputTokens, &d.TotalCost, &d.SpanCount, &d.LLMCallCount, &d.ToolCallCount,
			&d.Status, &errStr, &metadata, &tags, &teamID, &d.CreatedAt); err != nil {
			continue
		}

		d.ParentTraceID = parentTraceID
		d.AgentID = agentID
		d.TeamID = teamID
		d.UserID = derefStr(userID)
		d.SessionKey = derefStr(sessionKey)
		d.RunID = derefStr(runID)
		d.EndTime = endTime
		if durationMS != nil {
			d.DurationMS = *durationMS
		}
		d.Name = derefStr(name)
		d.Channel = derefStr(channel)
		d.InputPreview = derefStr(inputPreview)
		d.OutputPreview = derefStr(outputPreview)
		d.Error = derefStr(errStr)
		if metadata != nil {
			d.Metadata = *metadata
		}
		scanStringArray(tags, &d.Tags)
		result = append(result, d)
	}
	return result, nil
}

func (s *PGTracingStore) ListChildTraces(ctx context.Context, parentTraceID uuid.UUID) ([]store.TraceData, error) {
	q := `SELECT id, parent_trace_id, agent_id, user_id, session_key, run_id, start_time, end_time,
		 duration_ms, name, channel, input_preview, output_preview,
		 total_input_tokens, total_output_tokens, COALESCE(total_cost, 0), span_count, llm_call_count, tool_call_count,
		 status, error, metadata, tags, team_id, created_at
		 FROM traces WHERE parent_trace_id = $1`
	qArgs := []any{parentTraceID}

	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return nil, fmt.Errorf("tenant_id required")
		}
		q += " AND tenant_id = $2"
		qArgs = append(qArgs, tid)
	}
	q += " ORDER BY created_at"

	rows, err := s.db.QueryContext(ctx, q, qArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.TraceData
	for rows.Next() {
		var d store.TraceData
		var parentID, agentID, teamID *uuid.UUID
		var userID, sessionKey, runID, name, channel, inputPreview, outputPreview, errStr *string
		var endTime *time.Time
		var durationMS *int
		var metadata *[]byte
		var tags []byte

		if err := rows.Scan(&d.ID, &parentID, &agentID, &userID, &sessionKey, &runID, &d.StartTime, &endTime,
			&durationMS, &name, &channel, &inputPreview, &outputPreview,
			&d.TotalInputTokens, &d.TotalOutputTokens, &d.TotalCost, &d.SpanCount, &d.LLMCallCount, &d.ToolCallCount,
			&d.Status, &errStr, &metadata, &tags, &teamID, &d.CreatedAt); err != nil {
			continue
		}

		d.ParentTraceID = parentID
		d.AgentID = agentID
		d.TeamID = teamID
		d.UserID = derefStr(userID)
		d.SessionKey = derefStr(sessionKey)
		d.RunID = derefStr(runID)
		d.EndTime = endTime
		if durationMS != nil {
			d.DurationMS = *durationMS
		}
		d.Name = derefStr(name)
		d.Channel = derefStr(channel)
		d.InputPreview = derefStr(inputPreview)
		d.OutputPreview = derefStr(outputPreview)
		d.Error = derefStr(errStr)
		if metadata != nil {
			d.Metadata = *metadata
		}
		scanStringArray(tags, &d.Tags)
		result = append(result, d)
	}
	return result, nil
}

func (s *PGTracingStore) CreateSpan(ctx context.Context, span *store.SpanData) error {
	if span.ID == uuid.Nil {
		span.ID = store.GenNewID()
	}
	tenantID := span.TenantID
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO spans (id, trace_id, parent_span_id, agent_id, span_type, name,
		 start_time, end_time, duration_ms, status, error, level,
		 model, provider, input_tokens, output_tokens, finish_reason,
		 model_params, tool_name, tool_call_id, input_preview, output_preview,
		 metadata, team_id, created_at, tenant_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26)`,
		span.ID, span.TraceID, span.ParentSpanID, span.AgentID, span.SpanType, nilStr(span.Name),
		span.StartTime, nilTime(span.EndTime), nilInt(span.DurationMS), span.Status, nilStr(span.Error), span.Level,
		nilStr(span.Model), nilStr(span.Provider), nilInt(span.InputTokens), nilInt(span.OutputTokens), nilStr(span.FinishReason),
		jsonOrNull(span.ModelParams), nilStr(span.ToolName), nilStr(span.ToolCallID), nilStr(span.InputPreview), nilStr(span.OutputPreview),
		jsonOrNull(span.Metadata), nilUUID(span.TeamID), span.CreatedAt, tenantID,
	)
	return err
}

func (s *PGTracingStore) UpdateSpan(ctx context.Context, spanID uuid.UUID, updates map[string]any) error {
	return execMapUpdate(ctx, s.db, "spans", spanID, updates)
}

func (s *PGTracingStore) GetTraceSpans(ctx context.Context, traceID uuid.UUID) ([]store.SpanData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, trace_id, parent_span_id, agent_id, span_type, name,
		 start_time, end_time, duration_ms, status, error, level,
		 model, provider, input_tokens, output_tokens, finish_reason,
		 model_params, tool_name, tool_call_id, input_preview, output_preview,
		 metadata, team_id, created_at
		 FROM spans WHERE trace_id = $1 ORDER BY start_time`, traceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.SpanData
	for rows.Next() {
		var d store.SpanData
		var parentSpanID, agentID, teamID *uuid.UUID
		var name, errStr, level, model, provider, finishReason, toolName, toolCallID, inputPreview, outputPreview *string
		var status *string
		var endTime *time.Time
		var durationMS, inputTokens, outputTokens *int
		var modelParams, metadata *[]byte

		if err := rows.Scan(&d.ID, &d.TraceID, &parentSpanID, &agentID, &d.SpanType, &name,
			&d.StartTime, &endTime, &durationMS, &status, &errStr, &level,
			&model, &provider, &inputTokens, &outputTokens, &finishReason,
			&modelParams, &toolName, &toolCallID, &inputPreview, &outputPreview,
			&metadata, &teamID, &d.CreatedAt); err != nil {
			slog.Warn("tracing: span scan failed", "trace_id", traceID, "error", err)
			continue
		}

		d.ParentSpanID = parentSpanID
		d.AgentID = agentID
		d.TeamID = teamID
		d.Name = derefStr(name)
		d.EndTime = endTime
		d.Status = derefStr(status)
		d.Level = derefStr(level)
		if modelParams != nil {
			d.ModelParams = *modelParams
		}
		if metadata != nil {
			d.Metadata = *metadata
		}
		if durationMS != nil {
			d.DurationMS = *durationMS
		}
		d.Error = derefStr(errStr)
		d.Model = derefStr(model)
		d.Provider = derefStr(provider)
		if inputTokens != nil {
			d.InputTokens = *inputTokens
		}
		if outputTokens != nil {
			d.OutputTokens = *outputTokens
		}
		d.FinishReason = derefStr(finishReason)
		d.ToolName = derefStr(toolName)
		d.ToolCallID = derefStr(toolCallID)
		d.InputPreview = derefStr(inputPreview)
		d.OutputPreview = derefStr(outputPreview)
		result = append(result, d)
	}
	return result, nil
}

func (s *PGTracingStore) BatchCreateSpans(ctx context.Context, spans []store.SpanData) error {
	if len(spans) == 0 {
		return nil
	}

	// Build multi-row INSERT
	const cols = 26
	valueGroups := make([]string, len(spans))
	args := make([]any, 0, len(spans)*cols)

	for i, span := range spans {
		if span.ID == uuid.Nil {
			span.ID = store.GenNewID()
			spans[i].ID = span.ID
		}
		tenantID := span.TenantID
		if tenantID == uuid.Nil {
			tenantID = store.MasterTenantID
		}
		base := i * cols
		placeholders := make([]string, cols)
		for j := range cols {
			placeholders[j] = fmt.Sprintf("$%d", base+j+1)
		}
		valueGroups[i] = "(" + strings.Join(placeholders, ", ") + ")"

		args = append(args,
			span.ID, span.TraceID, span.ParentSpanID, span.AgentID, span.SpanType, nilStr(span.Name),
			span.StartTime, nilTime(span.EndTime), nilInt(span.DurationMS), span.Status, nilStr(span.Error), span.Level,
			nilStr(span.Model), nilStr(span.Provider), nilInt(span.InputTokens), nilInt(span.OutputTokens), nilStr(span.FinishReason),
			jsonOrNull(span.ModelParams), nilStr(span.ToolName), nilStr(span.ToolCallID), nilStr(span.InputPreview), nilStr(span.OutputPreview),
			jsonOrNull(span.Metadata), nilUUID(span.TeamID), span.CreatedAt, tenantID,
		)
	}

	q := `INSERT INTO spans (id, trace_id, parent_span_id, agent_id, span_type, name,
		 start_time, end_time, duration_ms, status, error, level,
		 model, provider, input_tokens, output_tokens, finish_reason,
		 model_params, tool_name, tool_call_id, input_preview, output_preview,
		 metadata, team_id, created_at, tenant_id)
		 VALUES ` + strings.Join(valueGroups, ", ")

	_, err := s.db.ExecContext(ctx, q, args...)
	if err == nil {
		return nil
	}

	// Batch failed — fallback to individual inserts
	slog.Warn("tracing: batch insert failed, falling back to individual inserts", "count", len(spans), "error", err)
	var firstErr error
	for i := range spans {
		if e := s.CreateSpan(ctx, &spans[i]); e != nil {
			slog.Warn("tracing: individual span insert failed", "span_id", spans[i].ID, "error", e)
			if firstErr == nil {
				firstErr = e
			}
		}
	}
	return firstErr
}

func (s *PGTracingStore) BatchUpdateTraceAggregates(ctx context.Context, traceID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE traces SET
			span_count = (SELECT COUNT(*) FROM spans WHERE trace_id = $1),
			llm_call_count = (SELECT COUNT(*) FROM spans WHERE trace_id = $1 AND span_type = 'llm_call'),
			tool_call_count = (SELECT COUNT(*) FROM spans WHERE trace_id = $1 AND span_type = 'tool_call'),
			total_input_tokens = COALESCE((SELECT SUM(input_tokens) FROM spans WHERE trace_id = $1 AND span_type = 'llm_call' AND input_tokens IS NOT NULL), 0),
			total_output_tokens = COALESCE((SELECT SUM(output_tokens) FROM spans WHERE trace_id = $1 AND span_type = 'llm_call' AND output_tokens IS NOT NULL), 0),
			total_cost = COALESCE((SELECT SUM(total_cost) FROM spans WHERE trace_id = $1 AND total_cost IS NOT NULL), 0),
			metadata = (
				SELECT jsonb_build_object(
					'total_cache_read_tokens', COALESCE(SUM((metadata->>'cache_read_tokens')::int), 0),
					'total_cache_creation_tokens', COALESCE(SUM((metadata->>'cache_creation_tokens')::int), 0)
				)
				FROM spans WHERE trace_id = $1 AND span_type = 'llm_call' AND metadata IS NOT NULL
			)
		WHERE id = $1`, traceID)
	return err
}

func (s *PGTracingStore) GetMonthlyAgentCost(ctx context.Context, agentID uuid.UUID, year int, month time.Month) (float64, error) {
	start := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)

	q := `SELECT COALESCE(SUM(total_cost), 0) FROM traces
		 WHERE agent_id = $1 AND created_at >= $2 AND created_at < $3 AND parent_trace_id IS NULL`
	qArgs := []any{agentID, start, end}

	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid != uuid.Nil {
			q += " AND tenant_id = $4"
			qArgs = append(qArgs, tid)
		}
	}

	var cost float64
	err := s.db.QueryRowContext(ctx, q, qArgs...).Scan(&cost)
	return cost, err
}

func (s *PGTracingStore) GetCostSummary(ctx context.Context, opts store.CostSummaryOpts) ([]store.CostSummaryRow, error) {
	var conditions []string
	var args []any
	argIdx := 1

	// Only root traces (not delegations)
	conditions = append(conditions, "parent_trace_id IS NULL")

	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID != uuid.Nil {
			conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIdx))
			args = append(args, tenantID)
			argIdx++
		}
	}

	if opts.AgentID != nil {
		conditions = append(conditions, fmt.Sprintf("agent_id = $%d", argIdx))
		args = append(args, *opts.AgentID)
		argIdx++
	}
	if opts.From != nil {
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", argIdx))
		args = append(args, *opts.From)
		argIdx++
	}
	if opts.To != nil {
		conditions = append(conditions, fmt.Sprintf("created_at < $%d", argIdx))
		args = append(args, *opts.To)
		argIdx++
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
	return result, nil
}

// DeleteTracesOlderThan deletes traces and their spans older than cutoff.
// Spans are deleted first (FK), then traces. Returns total traces deleted.
func (s *PGTracingStore) DeleteTracesOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	// Delete spans belonging to old traces.
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM spans WHERE trace_id IN (SELECT id FROM traces WHERE created_at < $1)`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete old spans: %w", err)
	}

	res, err := s.db.ExecContext(ctx, `DELETE FROM traces WHERE created_at < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete old traces: %w", err)
	}
	return res.RowsAffected()
}

// RecoverStaleRunningTraces marks traces stuck in "running" since before cutoff as "error".
// Also recovers their stuck spans. Called on startup to fix orphans from crashes.
func (s *PGTracingStore) RecoverStaleRunningTraces(ctx context.Context, cutoff time.Time) (int64, error) {
	// Recover stuck spans first.
	_, err := s.db.ExecContext(ctx,
		`UPDATE spans SET status = 'error', error = 'recovered: server restart',
		   end_time = NOW(), duration_ms = EXTRACT(EPOCH FROM (NOW() - start_time))::int * 1000
		 WHERE status = 'running' AND start_time < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("recover stale spans: %w", err)
	}

	res, err := s.db.ExecContext(ctx,
		`UPDATE traces SET status = 'error',
		   error = 'recovered: stuck in running state (server restart)',
		   end_time = NOW(), duration_ms = EXTRACT(EPOCH FROM (NOW() - start_time))::int * 1000
		 WHERE status = 'running' AND start_time < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("recover stale running traces: %w", err)
	}
	return res.RowsAffected()
}
