package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Trace status constants.
const (
	TraceStatusRunning   = "running"
	TraceStatusCompleted = "completed"
	TraceStatusError     = "error"
	TraceStatusCancelled = "cancelled"
)

// Span type constants.
const (
	SpanTypeLLMCall   = "llm_call"
	SpanTypeToolCall  = "tool_call"
	SpanTypeAgent     = "agent"
	SpanTypeEmbedding = "embedding"
	SpanTypeEvent     = "event"
)

// Span status constants.
const (
	SpanStatusCompleted = "completed"
	SpanStatusError     = "error"
	SpanStatusRunning   = "running"
)

// Span level constants.
const (
	SpanLevelDefault = "DEFAULT"
)

// TraceData represents a top-level trace (one per user request).
type TraceData struct {
	ID                uuid.UUID       `json:"id" db:"id"`
	ParentTraceID     *uuid.UUID      `json:"parent_trace_id,omitempty" db:"parent_trace_id"` // linked parent trace (delegation)
	AgentID           *uuid.UUID      `json:"agent_id,omitempty" db:"agent_id"`
	UserID            string          `json:"user_id,omitempty" db:"user_id"`
	SessionKey        string          `json:"session_key,omitempty" db:"session_key"`
	RunID             string          `json:"run_id,omitempty" db:"run_id"`
	StartTime         time.Time       `json:"start_time" db:"start_time"`
	EndTime           *time.Time      `json:"end_time,omitempty" db:"end_time"`
	DurationMS        int             `json:"duration_ms,omitempty" db:"duration_ms"`
	Name              string          `json:"name,omitempty" db:"name"`
	Channel           string          `json:"channel,omitempty" db:"channel"`
	InputPreview      string          `json:"input_preview,omitempty" db:"input_preview"`
	OutputPreview     string          `json:"output_preview,omitempty" db:"output_preview"`
	TotalInputTokens  int             `json:"total_input_tokens" db:"total_input_tokens"`
	TotalOutputTokens int             `json:"total_output_tokens" db:"total_output_tokens"`
	TotalCost         float64         `json:"total_cost" db:"total_cost"`
	SpanCount         int             `json:"span_count" db:"span_count"`
	LLMCallCount      int             `json:"llm_call_count" db:"llm_call_count"`
	ToolCallCount     int             `json:"tool_call_count" db:"tool_call_count"`
	Status            string          `json:"status" db:"status"`
	Error             string          `json:"error,omitempty" db:"error"`
	Metadata          json.RawMessage `json:"metadata,omitempty" db:"metadata"`
	Tags              []string        `json:"tags,omitempty" db:"tags"`
	TeamID            *uuid.UUID      `json:"team_id,omitempty" db:"team_id"`
	CreatedAt         time.Time       `json:"created_at" db:"created_at"`
}

// SpanData represents a single operation within a trace.
type SpanData struct {
	ID            uuid.UUID       `json:"id" db:"id"`
	TraceID       uuid.UUID       `json:"trace_id" db:"trace_id"`
	ParentSpanID  *uuid.UUID      `json:"parent_span_id,omitempty" db:"parent_span_id"`
	AgentID       *uuid.UUID      `json:"agent_id,omitempty" db:"agent_id"`
	SpanType      string          `json:"span_type" db:"span_type"` // "llm_call", "tool_call", "agent", "embedding", "event"
	Name          string          `json:"name,omitempty" db:"name"`
	StartTime     time.Time       `json:"start_time" db:"start_time"`
	EndTime       *time.Time      `json:"end_time,omitempty" db:"end_time"`
	DurationMS    int             `json:"duration_ms,omitempty" db:"duration_ms"`
	Status        string          `json:"status" db:"status"`
	Error         string          `json:"error,omitempty" db:"error"`
	Level         string          `json:"level,omitempty" db:"level"`
	Model         string          `json:"model,omitempty" db:"model"`
	Provider      string          `json:"provider,omitempty" db:"provider"`
	InputTokens   int             `json:"input_tokens,omitempty" db:"input_tokens"`
	OutputTokens  int             `json:"output_tokens,omitempty" db:"output_tokens"`
	TotalCost     *float64        `json:"total_cost,omitempty" db:"total_cost"`
	FinishReason  string          `json:"finish_reason,omitempty" db:"finish_reason"`
	ModelParams   json.RawMessage `json:"model_params,omitempty" db:"model_params"`
	ToolName      string          `json:"tool_name,omitempty" db:"tool_name"`
	ToolCallID    string          `json:"tool_call_id,omitempty" db:"tool_call_id"`
	InputPreview  string          `json:"input_preview,omitempty" db:"input_preview"`
	OutputPreview string          `json:"output_preview,omitempty" db:"output_preview"`
	Metadata      json.RawMessage `json:"metadata,omitempty" db:"metadata"`
	TeamID        *uuid.UUID      `json:"team_id,omitempty" db:"team_id"`
	TenantID      uuid.UUID       `json:"tenant_id" db:"tenant_id"`
	CreatedAt     time.Time       `json:"created_at" db:"created_at"`
}

// TraceListOpts configures trace listing.
type TraceListOpts struct {
	AgentID    *uuid.UUID
	UserID     string
	SessionKey string
	Status     string
	Channel    string
	Limit      int
	Offset     int
}

// CostSummaryOpts configures cost aggregation queries.
type CostSummaryOpts struct {
	AgentID *uuid.UUID
	From    *time.Time
	To      *time.Time
}

// CostSummaryRow is a single row of aggregated cost data.
type CostSummaryRow struct {
	AgentID           *uuid.UUID `json:"agent_id,omitempty" db:"agent_id"`
	TotalCost         float64    `json:"total_cost" db:"total_cost"`
	TotalInputTokens  int        `json:"total_input_tokens" db:"total_input_tokens"`
	TotalOutputTokens int        `json:"total_output_tokens" db:"total_output_tokens"`
	TraceCount        int        `json:"trace_count" db:"trace_count"`
}

// CodexPoolSpan holds the fields from a single LLM span for Codex pool activity analysis.
type CodexPoolSpan struct {
	SpanID     uuid.UUID
	TraceID    uuid.UUID
	StartedAt  time.Time
	DurationMS int
	Status     string
	Provider   string
	Model      string
	Metadata   json.RawMessage
}

// TracingStore manages LLM traces and spans.
type TracingStore interface {
	CreateTrace(ctx context.Context, trace *TraceData) error
	UpdateTrace(ctx context.Context, traceID uuid.UUID, updates map[string]any) error
	GetTrace(ctx context.Context, traceID uuid.UUID) (*TraceData, error)
	ListTraces(ctx context.Context, opts TraceListOpts) ([]TraceData, error)
	CountTraces(ctx context.Context, opts TraceListOpts) (int, error)

	CreateSpan(ctx context.Context, span *SpanData) error
	UpdateSpan(ctx context.Context, spanID uuid.UUID, updates map[string]any) error
	GetTraceSpans(ctx context.Context, traceID uuid.UUID) ([]SpanData, error)
	ListChildTraces(ctx context.Context, parentTraceID uuid.UUID) ([]TraceData, error)

	// Batch operations (async flush)
	BatchCreateSpans(ctx context.Context, spans []SpanData) error
	BatchUpdateTraceAggregates(ctx context.Context, traceID uuid.UUID) error

	// Cost aggregation
	GetMonthlyAgentCost(ctx context.Context, agentID uuid.UUID, year int, month time.Month) (float64, error)
	GetCostSummary(ctx context.Context, opts CostSummaryOpts) ([]CostSummaryRow, error)

	// Maintenance
	DeleteTracesOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
	// RecoverStaleRunningTraces marks traces stuck in "running" since before cutoff as "error".
	// Returns count of recovered traces. Called on startup to fix orphans from crashes.
	RecoverStaleRunningTraces(ctx context.Context, cutoff time.Time) (int64, error)

	// ListCodexPoolSpans returns recent LLM call spans for agents using Codex OAuth pool providers.
	ListCodexPoolSpans(ctx context.Context, agentID, tenantID uuid.UUID, poolProviders []string, limit int) ([]CodexPoolSpan, error)

	// ListCodexPoolSpansByProviders returns recent LLM call spans across all agents
	// that used any of the given pool providers. Used for provider-scoped activity monitoring.
	ListCodexPoolSpansByProviders(ctx context.Context, tenantID uuid.UUID, poolProviders []string, limit int) ([]CodexPoolProviderSpan, error)
}

// CodexPoolProviderSpan extends CodexPoolSpan with the agent ID for provider-scoped aggregation.
type CodexPoolProviderSpan struct {
	CodexPoolSpan
	AgentID uuid.UUID
}
