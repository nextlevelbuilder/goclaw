//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (s *SQLiteTracingStore) CreateSpan(ctx context.Context, span *store.SpanData) error {
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
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		span.ID, span.TraceID, span.ParentSpanID, span.AgentID, span.SpanType, nilStr(span.Name),
		span.StartTime, nilTime(span.EndTime), nilInt(span.DurationMS), span.Status, nilStr(span.Error), span.Level,
		nilStr(span.Model), nilStr(span.Provider), nilInt(span.InputTokens), nilInt(span.OutputTokens), nilStr(span.FinishReason),
		jsonOrNull(span.ModelParams), nilStr(span.ToolName), nilStr(span.ToolCallID), nilStr(span.InputPreview), nilStr(span.OutputPreview),
		jsonOrNull(span.Metadata), nilUUID(span.TeamID), span.CreatedAt, tenantID,
	)
	return err
}

func (s *SQLiteTracingStore) UpdateSpan(ctx context.Context, spanID uuid.UUID, updates map[string]any) error {
	return execMapUpdate(ctx, s.db, "spans", spanID, updates)
}

func (s *SQLiteTracingStore) GetTraceSpans(ctx context.Context, traceID uuid.UUID) ([]store.SpanData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, trace_id, parent_span_id, agent_id, span_type, name,
		 start_time, end_time, duration_ms, status, error, level,
		 model, provider, input_tokens, output_tokens, finish_reason,
		 model_params, tool_name, tool_call_id, input_preview, output_preview,
		 metadata, team_id, created_at
		 FROM spans WHERE trace_id = ? ORDER BY start_time`, traceID)
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
		var endTimeSt nullSqliteTime
		var durationMS, inputTokens, outputTokens *int
		var modelParams, metadata *[]byte
		var startTime, createdAt sqliteTime

		if err := rows.Scan(&d.ID, &d.TraceID, &parentSpanID, &agentID, &d.SpanType, &name,
			&startTime, &endTimeSt, &durationMS, &status, &errStr, &level,
			&model, &provider, &inputTokens, &outputTokens, &finishReason,
			&modelParams, &toolName, &toolCallID, &inputPreview, &outputPreview,
			&metadata, &teamID, &createdAt); err != nil {
			slog.Warn("tracing: span scan failed", "trace_id", traceID, "error", err)
			continue
		}
		d.StartTime = startTime.Time
		d.CreatedAt = createdAt.Time

		d.ParentSpanID = parentSpanID
		d.AgentID = agentID
		d.TeamID = teamID
		d.Name = derefStr(name)
		if endTimeSt.Valid {
			d.EndTime = &endTimeSt.Time
		}
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
	return result, rows.Err()
}

// sqliteSpanCols is the number of columns in the spans INSERT.
const sqliteSpanCols = 26

// sqliteSpanBatchSize limits rows per batch: 999 / 26 ≈ 38.
const sqliteSpanBatchSize = 38

// BatchCreateSpans inserts spans in batches to stay under SQLite's 999-variable limit.
func (s *SQLiteTracingStore) BatchCreateSpans(ctx context.Context, spans []store.SpanData) error {
	if len(spans) == 0 {
		return nil
	}
	for start := 0; start < len(spans); start += sqliteSpanBatchSize {
		end := start + sqliteSpanBatchSize
		if end > len(spans) {
			end = len(spans)
		}
		if err := s.batchInsertSpans(ctx, spans[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteTracingStore) batchInsertSpans(ctx context.Context, spans []store.SpanData) error {
	placeholderRow := "(" + strings.Repeat("?, ", sqliteSpanCols-1) + "?)"
	valueGroups := make([]string, len(spans))
	args := make([]any, 0, len(spans)*sqliteSpanCols)

	for i, span := range spans {
		if span.ID == uuid.Nil {
			span.ID = store.GenNewID()
			spans[i].ID = span.ID
		}
		tenantID := span.TenantID
		if tenantID == uuid.Nil {
			tenantID = store.MasterTenantID
		}
		valueGroups[i] = placeholderRow
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

	// Fallback to individual inserts on batch failure.
	slog.Warn("tracing: batch span insert failed, falling back", "count", len(spans), "error", err)
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

func (s *SQLiteTracingStore) BatchUpdateTraceAggregates(ctx context.Context, traceID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE traces SET
			span_count       = (SELECT COUNT(*) FROM spans WHERE trace_id = ?),
			llm_call_count   = (SELECT COUNT(*) FROM spans WHERE trace_id = ? AND span_type = 'llm_call'),
			tool_call_count  = (SELECT COUNT(*) FROM spans WHERE trace_id = ? AND span_type = 'tool_call'),
			total_input_tokens  = COALESCE((SELECT SUM(input_tokens)  FROM spans WHERE trace_id = ? AND span_type = 'llm_call' AND input_tokens  IS NOT NULL), 0),
			total_output_tokens = COALESCE((SELECT SUM(output_tokens) FROM spans WHERE trace_id = ? AND span_type = 'llm_call' AND output_tokens IS NOT NULL), 0),
			total_cost       = COALESCE((SELECT SUM(total_cost) FROM spans WHERE trace_id = ? AND total_cost IS NOT NULL), 0),
			metadata         = (
				SELECT json_object(
					'total_cache_read_tokens',     COALESCE(SUM(json_extract(metadata, '$.cache_read_tokens')), 0),
					'total_cache_creation_tokens', COALESCE(SUM(json_extract(metadata, '$.cache_creation_tokens')), 0)
				)
				FROM spans WHERE trace_id = ? AND span_type = 'llm_call' AND metadata IS NOT NULL
			)
		WHERE id = ?`,
		traceID, traceID, traceID, traceID, traceID, traceID, traceID, traceID)
	return err
}

