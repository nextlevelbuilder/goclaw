package pg

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// traceRow is an sqlx scan struct for traces table rows.
// Uses pointer types for nullable columns that map to non-pointer domain fields,
// and pq.StringArray for the PostgreSQL text[] tags column.
type traceRow struct {
	ID                uuid.UUID       `json:"id" db:"id"`
	ParentTraceID     *uuid.UUID      `json:"parent_trace_id" db:"parent_trace_id"`
	AgentID           *uuid.UUID      `json:"agent_id" db:"agent_id"`
	UserID            *string         `json:"user_id" db:"user_id"`
	SessionKey        *string         `json:"session_key" db:"session_key"`
	RunID             *string         `json:"run_id" db:"run_id"`
	StartTime         time.Time       `json:"start_time" db:"start_time"`
	EndTime           *time.Time      `json:"end_time" db:"end_time"`
	DurationMS        *int            `json:"duration_ms" db:"duration_ms"`
	Name              *string         `json:"name" db:"name"`
	Channel           *string         `json:"channel" db:"channel"`
	InputPreview      *string         `json:"input_preview" db:"input_preview"`
	OutputPreview     *string         `json:"output_preview" db:"output_preview"`
	TotalInputTokens  int             `json:"total_input_tokens" db:"total_input_tokens"`
	TotalOutputTokens int             `json:"total_output_tokens" db:"total_output_tokens"`
	TotalCost         float64         `json:"total_cost" db:"total_cost"` // COALESCE in SQL guarantees non-NULL
	SpanCount         int             `json:"span_count" db:"span_count"`
	LLMCallCount      int             `json:"llm_call_count" db:"llm_call_count"`
	ToolCallCount     int             `json:"tool_call_count" db:"tool_call_count"`
	Status            string          `json:"status" db:"status"`
	Error             *string         `json:"error" db:"error"`
	Metadata          json.RawMessage `json:"metadata" db:"metadata"`
	Tags              pq.StringArray  `json:"tags" db:"tags"`
	TeamID            *uuid.UUID      `json:"team_id" db:"team_id"`
	CreatedAt         time.Time       `json:"created_at" db:"created_at"`
}

func (r *traceRow) toTraceData() store.TraceData {
	return store.TraceData{
		ID: r.ID, ParentTraceID: r.ParentTraceID, AgentID: r.AgentID,
		UserID: derefStr(r.UserID), SessionKey: derefStr(r.SessionKey), RunID: derefStr(r.RunID),
		StartTime: r.StartTime, EndTime: r.EndTime, DurationMS: derefInt(r.DurationMS),
		Name: derefStr(r.Name), Channel: derefStr(r.Channel),
		InputPreview: derefStr(r.InputPreview), OutputPreview: derefStr(r.OutputPreview),
		TotalInputTokens: r.TotalInputTokens, TotalOutputTokens: r.TotalOutputTokens,
		TotalCost: r.TotalCost, SpanCount: r.SpanCount,
		LLMCallCount: r.LLMCallCount, ToolCallCount: r.ToolCallCount,
		Status: r.Status, Error: derefStr(r.Error),
		Metadata: r.Metadata, Tags: []string(r.Tags),
		TeamID: r.TeamID, CreatedAt: r.CreatedAt,
	}
}

func traceRowsToData(rows []traceRow) []store.TraceData {
	result := make([]store.TraceData, len(rows))
	for i := range rows {
		result[i] = rows[i].toTraceData()
	}
	return result
}

// spanRow is an sqlx scan struct for spans table rows.
type spanRow struct {
	ID            uuid.UUID       `json:"id" db:"id"`
	TraceID       uuid.UUID       `json:"trace_id" db:"trace_id"`
	ParentSpanID  *uuid.UUID      `json:"parent_span_id" db:"parent_span_id"`
	AgentID       *uuid.UUID      `json:"agent_id" db:"agent_id"`
	SpanType      string          `json:"span_type" db:"span_type"`
	Name          *string         `json:"name" db:"name"`
	StartTime     time.Time       `json:"start_time" db:"start_time"`
	EndTime       *time.Time      `json:"end_time" db:"end_time"`
	DurationMS    *int            `json:"duration_ms" db:"duration_ms"`
	Status        *string         `json:"status" db:"status"`
	Error         *string         `json:"error" db:"error"`
	Level         *string         `json:"level" db:"level"`
	Model         *string         `json:"model" db:"model"`
	Provider      *string         `json:"provider" db:"provider"`
	InputTokens   *int            `json:"input_tokens" db:"input_tokens"`
	OutputTokens  *int            `json:"output_tokens" db:"output_tokens"`
	FinishReason  *string         `json:"finish_reason" db:"finish_reason"`
	ModelParams   json.RawMessage `json:"model_params" db:"model_params"`
	ToolName      *string         `json:"tool_name" db:"tool_name"`
	ToolCallID    *string         `json:"tool_call_id" db:"tool_call_id"`
	InputPreview  *string         `json:"input_preview" db:"input_preview"`
	OutputPreview *string         `json:"output_preview" db:"output_preview"`
	Metadata      json.RawMessage `json:"metadata" db:"metadata"`
	TeamID        *uuid.UUID      `json:"team_id" db:"team_id"`
	CreatedAt     time.Time       `json:"created_at" db:"created_at"`
}

func (r *spanRow) toSpanData() store.SpanData {
	return store.SpanData{
		ID: r.ID, TraceID: r.TraceID, ParentSpanID: r.ParentSpanID, AgentID: r.AgentID,
		SpanType: r.SpanType, Name: derefStr(r.Name),
		StartTime: r.StartTime, EndTime: r.EndTime, DurationMS: derefInt(r.DurationMS),
		Status: derefStr(r.Status), Error: derefStr(r.Error), Level: derefStr(r.Level),
		Model: derefStr(r.Model), Provider: derefStr(r.Provider),
		InputTokens: derefInt(r.InputTokens), OutputTokens: derefInt(r.OutputTokens),
		FinishReason: derefStr(r.FinishReason), ModelParams: r.ModelParams,
		ToolName: derefStr(r.ToolName), ToolCallID: derefStr(r.ToolCallID),
		InputPreview: derefStr(r.InputPreview), OutputPreview: derefStr(r.OutputPreview),
		Metadata: r.Metadata, TeamID: r.TeamID, CreatedAt: r.CreatedAt,
	}
}

func spanRowsToData(rows []spanRow) []store.SpanData {
	result := make([]store.SpanData, len(rows))
	for i := range rows {
		result[i] = rows[i].toSpanData()
	}
	return result
}
