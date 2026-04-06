package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// MetricType identifies the category of evolution metric.
type MetricType string

const (
	MetricRetrieval MetricType = "retrieval"
	MetricTool      MetricType = "tool"
	MetricFeedback  MetricType = "feedback"
)

// EvolutionMetric is a single recorded metric data point.
type EvolutionMetric struct {
	ID         uuid.UUID       `json:"id"`
	TenantID   uuid.UUID       `json:"tenant_id"`
	AgentID    uuid.UUID       `json:"agent_id"`
	SessionKey string          `json:"session_key"`
	MetricType MetricType      `json:"metric_type"`
	MetricKey  string          `json:"metric_key"`
	Value      json.RawMessage `json:"value"`
	CreatedAt  time.Time       `json:"created_at"`
}

// ToolAggregate summarizes per-tool metrics over a period.
type ToolAggregate struct {
	ToolName    string
	CallCount   int
	SuccessRate float64
	AvgDuration time.Duration
}

// RetrievalAggregate summarizes per-source retrieval metrics.
type RetrievalAggregate struct {
	Source     string
	QueryCount int
	UsageRate  float64 // fraction of results used in reply
	AvgScore   float64
}

// EvolutionMetricsStore manages self-evolution metrics (Stage 1).
type EvolutionMetricsStore interface {
	RecordMetric(ctx context.Context, metric EvolutionMetric) error
	QueryMetrics(ctx context.Context, agentID uuid.UUID, metricType MetricType, since time.Time, limit int) ([]EvolutionMetric, error)
	AggregateToolMetrics(ctx context.Context, agentID uuid.UUID, since time.Time) ([]ToolAggregate, error)
	AggregateRetrievalMetrics(ctx context.Context, agentID uuid.UUID, since time.Time) ([]RetrievalAggregate, error)
	Cleanup(ctx context.Context, olderThan time.Time) (int64, error)
}

// SuggestionType identifies the kind of evolution suggestion.
type SuggestionType string

const (
	SuggestThreshold   SuggestionType = "threshold"
	SuggestToolOrder   SuggestionType = "tool_order"
	SuggestSkillAdd    SuggestionType = "skill_add"
	SuggestMemoryPrune SuggestionType = "memory_prune"
)

// EvolutionSuggestion is a data-driven suggestion for agent improvement.
type EvolutionSuggestion struct {
	ID             uuid.UUID       `json:"id"`
	TenantID       uuid.UUID       `json:"tenant_id"`
	AgentID        uuid.UUID       `json:"agent_id"`
	SuggestionType SuggestionType  `json:"suggestion_type"`
	Suggestion     string          `json:"suggestion"`
	Rationale      string          `json:"rationale"`
	Parameters     json.RawMessage `json:"parameters,omitempty"`
	Status         string          `json:"status"` // pending, approved, rejected, applied, rolled_back
	ReviewedBy     string          `json:"reviewed_by,omitempty"`
	ReviewedAt     *time.Time      `json:"reviewed_at,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

// EvolutionSuggestionStore manages suggestions (Stage 2).
type EvolutionSuggestionStore interface {
	CreateSuggestion(ctx context.Context, s EvolutionSuggestion) error
	ListSuggestions(ctx context.Context, agentID uuid.UUID, status string, limit int) ([]EvolutionSuggestion, error)
	UpdateSuggestionStatus(ctx context.Context, id uuid.UUID, status, reviewedBy string) error
	UpdateSuggestionParameters(ctx context.Context, id uuid.UUID, params json.RawMessage) error
	GetSuggestion(ctx context.Context, id uuid.UUID) (*EvolutionSuggestion, error)
}
