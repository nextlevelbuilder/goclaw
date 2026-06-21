package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	UsageEventTypeToolCall        = "tool_call"
	UsageEventTypeSkillActivation = "skill_activation"
	UsageEventTypeMCPToolCall     = "mcp_tool_call"
	UsageEventTypeRuntimeToolCall = "runtime_tool_call"

	UsageResourceTypeTool        = "tool"
	UsageResourceTypeSkill       = "skill"
	UsageResourceTypeMCPTool     = "mcp_tool"
	UsageResourceTypeRuntimeTool = "runtime_tool"

	UsageSourceToolCall     = "tool_call"
	UsageSourceUseSkill     = "use_skill"
	UsageSourceSlashCommand = "slash-command"
)

// UsageEvent is an append-only analytics row for resource usage.
// It intentionally excludes user IDs, raw prompts, tool args, outputs, and shell commands.
type UsageEvent struct {
	ID           uuid.UUID       `json:"id" db:"id"`
	TenantID     uuid.UUID       `json:"tenant_id" db:"tenant_id"`
	EventTime    time.Time       `json:"event_time" db:"event_time"`
	BucketHour   time.Time       `json:"bucket_hour" db:"bucket_hour"`
	EventType    string          `json:"event_type" db:"event_type"`
	ResourceType string          `json:"resource_type" db:"resource_type"`
	ResourceName string          `json:"resource_name" db:"resource_name"`
	ResourceID   string          `json:"resource_id" db:"resource_id"`
	Source       string          `json:"source" db:"source"`
	AgentID      *uuid.UUID      `json:"agent_id,omitempty" db:"agent_id"`
	TeamID       *uuid.UUID      `json:"team_id,omitempty" db:"team_id"`
	TraceID      *uuid.UUID      `json:"trace_id,omitempty" db:"trace_id"`
	SpanID       *uuid.UUID      `json:"span_id,omitempty" db:"span_id"`
	RunID        string          `json:"run_id" db:"run_id"`
	SessionKey   string          `json:"session_key" db:"session_key"`
	Channel      string          `json:"channel" db:"channel"`
	Provider     string          `json:"provider" db:"provider"`
	Model        string          `json:"model" db:"model"`
	Status       string          `json:"status" db:"status"`
	InputTokens  int64           `json:"input_tokens" db:"input_tokens"`
	OutputTokens int64           `json:"output_tokens" db:"output_tokens"`
	TotalTokens  int64           `json:"total_tokens" db:"total_tokens"`
	CostUSD      float64         `json:"cost_usd" db:"cost_usd"`
	DurationMS   int             `json:"duration_ms" db:"duration_ms"`
	CallCount    int             `json:"call_count" db:"call_count"`
	ErrorCount   int             `json:"error_count" db:"error_count"`
	Metadata     json.RawMessage `json:"metadata,omitempty" db:"metadata"`
	CreatedAt    time.Time       `json:"created_at" db:"created_at"`
}

// UsageEventQuery filters usage event analytics.
type UsageEventQuery struct {
	From         time.Time
	To           time.Time
	AgentID      *uuid.UUID
	Channel      string
	EventType    string
	ResourceType string
	ResourceName string
	Provider     string
	Model        string
	Status       string
	Source       string
	GroupBy      string
	Limit        int
}

type UsageEventSummary struct {
	Calls         int     `json:"calls" db:"calls"`
	Errors        int     `json:"errors" db:"errors"`
	InputTokens   int64   `json:"input_tokens" db:"input_tokens"`
	OutputTokens  int64   `json:"output_tokens" db:"output_tokens"`
	TotalTokens   int64   `json:"total_tokens" db:"total_tokens"`
	CostUSD       float64 `json:"cost_usd" db:"cost_usd"`
	AvgDurationMS int     `json:"avg_duration_ms" db:"avg_duration_ms"`
}

type UsageEventTimeSeries struct {
	BucketTime    time.Time `json:"bucket_time" db:"bucket_time"`
	Calls         int       `json:"calls" db:"calls"`
	Errors        int       `json:"errors" db:"errors"`
	InputTokens   int64     `json:"input_tokens" db:"input_tokens"`
	OutputTokens  int64     `json:"output_tokens" db:"output_tokens"`
	TotalTokens   int64     `json:"total_tokens" db:"total_tokens"`
	CostUSD       float64   `json:"cost_usd" db:"cost_usd"`
	AvgDurationMS int       `json:"avg_duration_ms" db:"avg_duration_ms"`
}

type UsageEventBreakdown struct {
	Key           string  `json:"key" db:"key"`
	EventType     string  `json:"event_type" db:"event_type"`
	ResourceType  string  `json:"resource_type" db:"resource_type"`
	ResourceName  string  `json:"resource_name" db:"resource_name"`
	Source        string  `json:"source" db:"source"`
	Calls         int     `json:"calls" db:"calls"`
	Errors        int     `json:"errors" db:"errors"`
	InputTokens   int64   `json:"input_tokens" db:"input_tokens"`
	OutputTokens  int64   `json:"output_tokens" db:"output_tokens"`
	TotalTokens   int64   `json:"total_tokens" db:"total_tokens"`
	CostUSD       float64 `json:"cost_usd" db:"cost_usd"`
	AvgDurationMS int     `json:"avg_duration_ms" db:"avg_duration_ms"`
}

type UsageEventRollup struct {
	ID           uuid.UUID  `json:"id" db:"id"`
	TenantID     uuid.UUID  `json:"tenant_id" db:"tenant_id"`
	BucketHour   time.Time  `json:"bucket_hour" db:"bucket_hour"`
	EventType    string     `json:"event_type" db:"event_type"`
	ResourceType string     `json:"resource_type" db:"resource_type"`
	ResourceName string     `json:"resource_name" db:"resource_name"`
	Source       string     `json:"source" db:"source"`
	AgentID      *uuid.UUID `json:"agent_id,omitempty" db:"agent_id"`
	Channel      string     `json:"channel" db:"channel"`
	Provider     string     `json:"provider" db:"provider"`
	Model        string     `json:"model" db:"model"`
	Status       string     `json:"status" db:"status"`
	InputTokens  int64      `json:"input_tokens" db:"input_tokens"`
	OutputTokens int64      `json:"output_tokens" db:"output_tokens"`
	TotalTokens  int64      `json:"total_tokens" db:"total_tokens"`
	CostUSD      float64    `json:"cost_usd" db:"cost_usd"`
	DurationMS   int        `json:"duration_ms" db:"duration_ms"`
	CallCount    int        `json:"call_count" db:"call_count"`
	ErrorCount   int        `json:"error_count" db:"error_count"`
	CreatedAt    time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at" db:"updated_at"`
}

type UsageEventStore interface {
	InsertEvent(ctx context.Context, event *UsageEvent) error
	InsertEvents(ctx context.Context, events []UsageEvent) error
	RefreshEventRollupHour(ctx context.Context, bucketHour time.Time) error
	GetLatestEventRollupBucket(ctx context.Context) (*time.Time, error)
	GetEventTimeSeries(ctx context.Context, q UsageEventQuery) ([]UsageEventTimeSeries, error)
	GetEventBreakdown(ctx context.Context, q UsageEventQuery) ([]UsageEventBreakdown, error)
	GetEventSummary(ctx context.Context, q UsageEventQuery) (*UsageEventSummary, error)
}
