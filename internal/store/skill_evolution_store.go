package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	SkillEvolutionModeSuggestOnly = "suggest_only"
	SkillEvolutionModeAutoAnalyze = "auto_analyze"

	SkillUsageStatusStarted   = "started"
	SkillUsageStatusSucceeded = "succeeded"
	SkillUsageStatusFailed    = "failed"
	SkillUsageStatusAbandoned = "abandoned"

	SkillSuggestionStatusPending  = "pending"
	SkillSuggestionStatusApproved = "approved"
	SkillSuggestionStatusRejected = "rejected"
	SkillSuggestionStatusApplied  = "applied"
)

// SkillEvolutionSettings stores tenant-scoped self-evolution controls for one skill.
type SkillEvolutionSettings struct {
	TenantID       uuid.UUID  `json:"tenant_id" db:"tenant_id"`
	SkillID        uuid.UUID  `json:"skill_id" db:"skill_id"`
	Enabled        bool       `json:"enabled" db:"enabled"`
	Mode           string     `json:"mode" db:"mode"`
	LastAnalyzedAt *time.Time `json:"last_analyzed_at,omitempty" db:"last_analyzed_at"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at" db:"updated_at"`
}

// SkillUsageMetric is a durable usage event emitted by trusted runtime paths.
type SkillUsageMetric struct {
	ID               uuid.UUID `json:"id" db:"id"`
	TenantID         uuid.UUID `json:"tenant_id" db:"tenant_id"`
	SkillID          uuid.UUID `json:"skill_id" db:"skill_id"`
	SkillSlug        string    `json:"skill_slug" db:"skill_slug"`
	SkillVersion     int       `json:"skill_version" db:"skill_version"`
	AgentID          uuid.UUID `json:"agent_id,omitempty" db:"agent_id"`
	UserID           string    `json:"user_id,omitempty" db:"user_id"`
	SessionKey       string    `json:"session_key,omitempty" db:"session_key"`
	TraceID          string    `json:"trace_id,omitempty" db:"trace_id"`
	InvocationID     string    `json:"invocation_id,omitempty" db:"invocation_id"`
	InvocationSource string    `json:"invocation_source" db:"invocation_source"`
	Status           string    `json:"status" db:"status"`
	FailureReason    string    `json:"failure_reason,omitempty" db:"failure_reason"`
	ToolCallsCount   int       `json:"tool_calls_count" db:"tool_calls_count"`
	DurationMs       int64     `json:"duration_ms" db:"duration_ms"`
	CreatedAt        time.Time `json:"created_at" db:"created_at"`
}

type SkillFailureReason struct {
	Reason   string    `json:"reason"`
	Count    int       `json:"count"`
	LastSeen time.Time `json:"last_seen"`
}

type SkillUsageStats struct {
	SkillID           uuid.UUID            `json:"skill_id"`
	TotalCalls        int                  `json:"total_calls"`
	Started           int                  `json:"started"`
	Succeeded         int                  `json:"succeeded"`
	Failed            int                  `json:"failed"`
	Abandoned         int                  `json:"abandoned"`
	SuccessRate       float64              `json:"success_rate"`
	FailureRate       float64              `json:"failure_rate"`
	LastUsedAt        *time.Time           `json:"last_used_at,omitempty"`
	TopFailureReasons []SkillFailureReason `json:"top_failure_reasons,omitempty"`
}

type SkillImprovementSuggestion struct {
	ID                  uuid.UUID       `json:"id" db:"id"`
	TenantID            uuid.UUID       `json:"tenant_id" db:"tenant_id"`
	SkillID             uuid.UUID       `json:"skill_id" db:"skill_id"`
	SkillSlug           string          `json:"skill_slug" db:"skill_slug"`
	SuggestionType      string          `json:"suggestion_type" db:"suggestion_type"`
	Status              string          `json:"status" db:"status"`
	Reason              string          `json:"reason" db:"reason"`
	Evidence            json.RawMessage `json:"evidence,omitempty" db:"evidence"`
	DraftPatch          json.RawMessage `json:"draft_patch,omitempty" db:"draft_patch"`
	TargetFile          string          `json:"target_file,omitempty" db:"target_file"`
	CreatedByActorType  string          `json:"created_by_actor_type,omitempty" db:"created_by_actor_type"`
	CreatedByActorID    string          `json:"created_by_actor_id,omitempty" db:"created_by_actor_id"`
	ReviewedByActorType string          `json:"reviewed_by_actor_type,omitempty" db:"reviewed_by_actor_type"`
	ReviewedByActorID   string          `json:"reviewed_by_actor_id,omitempty" db:"reviewed_by_actor_id"`
	ReviewedAt          *time.Time      `json:"reviewed_at,omitempty" db:"reviewed_at"`
	AppliedVersion      *int            `json:"applied_version,omitempty" db:"applied_version"`
	CreatedAt           time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at" db:"updated_at"`
}

type SkillVersion struct {
	ID                      uuid.UUID       `json:"id" db:"id"`
	TenantID                uuid.UUID       `json:"tenant_id" db:"tenant_id"`
	SkillID                 uuid.UUID       `json:"skill_id" db:"skill_id"`
	Version                 int             `json:"version" db:"version"`
	ContentHash             string          `json:"content_hash" db:"content_hash"`
	ChangedFiles            json.RawMessage `json:"changed_files,omitempty" db:"changed_files"`
	CreatedByActorType      string          `json:"created_by_actor_type,omitempty" db:"created_by_actor_type"`
	CreatedByActorID        string          `json:"created_by_actor_id,omitempty" db:"created_by_actor_id"`
	CreatedFromSuggestionID *uuid.UUID      `json:"created_from_suggestion_id,omitempty" db:"created_from_suggestion_id"`
	CreatedAt               time.Time       `json:"created_at" db:"created_at"`
}

type SkillEvolutionStore interface {
	GetSettings(ctx context.Context, skillID uuid.UUID) (*SkillEvolutionSettings, error)
	UpsertSettings(ctx context.Context, settings SkillEvolutionSettings) (*SkillEvolutionSettings, error)
	RecordUsage(ctx context.Context, metric SkillUsageMetric) error
	AggregateUsage(ctx context.Context, skillID uuid.UUID, since *time.Time) (*SkillUsageStats, error)
	ListUsage(ctx context.Context, skillID uuid.UUID, limit int) ([]SkillUsageMetric, error)
	CreateSuggestion(ctx context.Context, suggestion SkillImprovementSuggestion) (*SkillImprovementSuggestion, error)
	ListSuggestions(ctx context.Context, skillID uuid.UUID, status string, limit int) ([]SkillImprovementSuggestion, error)
	GetSuggestion(ctx context.Context, id uuid.UUID) (*SkillImprovementSuggestion, error)
	UpdateSuggestionStatus(ctx context.Context, id uuid.UUID, status, actorType, actorID string) (*SkillImprovementSuggestion, error)
	MarkSuggestionApplied(ctx context.Context, id uuid.UUID, version int, actorType, actorID string) (*SkillImprovementSuggestion, error)
	CreateSkillVersion(ctx context.Context, version SkillVersion) (*SkillVersion, error)
	ListSkillVersions(ctx context.Context, skillID uuid.UUID, limit int) ([]SkillVersion, error)
	GetSkillVersion(ctx context.Context, skillID uuid.UUID, version int) (*SkillVersion, error)
}
