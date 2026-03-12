package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// UsageSnapshot represents one hourly aggregation row.
type UsageSnapshot struct {
	ID                uuid.UUID  `json:"id"`
	BucketHour        time.Time  `json:"bucket_hour"`
	AgentID           *uuid.UUID `json:"agent_id,omitempty"`
	Provider          string     `json:"provider"`
	Model             string     `json:"model"`
	Channel           string     `json:"channel"`
	InputTokens       int64      `json:"input_tokens"`
	OutputTokens      int64      `json:"output_tokens"`
	CacheReadTokens   int64      `json:"cache_read_tokens"`
	CacheCreateTokens int64      `json:"cache_create_tokens"`
	ThinkingTokens    int64      `json:"thinking_tokens"`
	TotalCost         float64    `json:"total_cost"`
	RequestCount      int        `json:"request_count"`
	LLMCallCount      int        `json:"llm_call_count"`
	ToolCallCount     int        `json:"tool_call_count"`
	ErrorCount        int        `json:"error_count"`
	UniqueUsers       int        `json:"unique_users"`
	AvgDurationMS     int        `json:"avg_duration_ms"`
	MemoryDocs        int        `json:"memory_docs"`
	MemoryChunks      int        `json:"memory_chunks"`
	KGEntities        int        `json:"kg_entities"`
	KGRelations       int        `json:"kg_relations"`
	CreatedAt         time.Time  `json:"created_at"`
}

// SnapshotQuery filters for listing snapshots.
type SnapshotQuery struct {
	From     time.Time  // required: start of time range (inclusive)
	To       time.Time  // required: end of time range (exclusive)
	AgentID  *uuid.UUID // optional: filter by agent
	Provider string     // optional: cross-filter by provider
	Model    string     // optional: cross-filter by model
	Channel  string     // optional: cross-filter by channel
	GroupBy  string     // "hour" (default), "day", "provider", "model", "channel", "agent"
}

// SnapshotTimeSeries is a single point in a time series response.
type SnapshotTimeSeries struct {
	BucketTime        time.Time `json:"bucket_time"`
	InputTokens       int64     `json:"input_tokens"`
	OutputTokens      int64     `json:"output_tokens"`
	CacheReadTokens   int64     `json:"cache_read_tokens"`
	CacheCreateTokens int64     `json:"cache_create_tokens"`
	ThinkingTokens    int64     `json:"thinking_tokens"`
	TotalCost         float64   `json:"total_cost"`
	RequestCount      int       `json:"request_count"`
	LLMCallCount      int       `json:"llm_call_count"`
	ToolCallCount     int       `json:"tool_call_count"`
	ErrorCount        int       `json:"error_count"`
	UniqueUsers       int       `json:"unique_users"`
	AvgDurationMS     int       `json:"avg_duration_ms"`
	MemoryDocs        int       `json:"memory_docs"`
	MemoryChunks      int       `json:"memory_chunks"`
	KGEntities        int       `json:"kg_entities"`
	KGRelations       int       `json:"kg_relations"`
}

// SnapshotBreakdown is a grouped aggregation row (by provider, model, etc.).
type SnapshotBreakdown struct {
	Key               string  `json:"key"`
	InputTokens       int64   `json:"input_tokens"`
	OutputTokens      int64   `json:"output_tokens"`
	CacheReadTokens   int64   `json:"cache_read_tokens"`
	CacheCreateTokens int64   `json:"cache_create_tokens"`
	TotalCost         float64 `json:"total_cost"`
	RequestCount      int     `json:"request_count"`
	LLMCallCount      int     `json:"llm_call_count"`
	ToolCallCount     int     `json:"tool_call_count"`
	ErrorCount        int     `json:"error_count"`
	AvgDurationMS     int     `json:"avg_duration_ms"`
}

// SnapshotStore manages pre-computed usage snapshots.
type SnapshotStore interface {
	// UpsertSnapshots inserts or updates (on conflict, replace) a batch of snapshots.
	UpsertSnapshots(ctx context.Context, snapshots []UsageSnapshot) error

	// GetTimeSeries returns hourly (or daily) aggregated time series.
	GetTimeSeries(ctx context.Context, q SnapshotQuery) ([]SnapshotTimeSeries, error)

	// GetBreakdown returns aggregated data grouped by a dimension (provider, model, channel, agent).
	GetBreakdown(ctx context.Context, q SnapshotQuery) ([]SnapshotBreakdown, error)

	// GetLatestBucket returns the most recent bucket_hour, used by worker to know where to resume.
	GetLatestBucket(ctx context.Context) (*time.Time, error)
}
