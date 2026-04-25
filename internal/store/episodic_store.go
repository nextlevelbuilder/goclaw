package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// EpisodicSummary represents a Tier 2 episodic memory entry.
// Created from session summaries via the consolidation pipeline.
type EpisodicSummary struct {
	ID         uuid.UUID  `json:"id" db:"id"`
	TenantID   uuid.UUID  `json:"tenant_id" db:"tenant_id"`
	AgentID    uuid.UUID  `json:"agent_id" db:"agent_id"`
	UserID     string     `json:"user_id" db:"user_id"` // string: chat-based IDs
	SessionKey string     `json:"session_key" db:"session_key"`
	Summary    string     `json:"summary" db:"summary"`
	KeyTopics  []string   `json:"key_topics" db:"key_topics"`
	L0Abstract string     `json:"l0_abstract" db:"l0_abstract"` // ~50 tokens, pre-computed
	SourceType string     `json:"source_type" db:"source_type"` // "session", "v2_daily", "manual"
	SourceID   string     `json:"source_id" db:"source_id"`     // dedup key
	TurnCount  int        `json:"turn_count" db:"turn_count"`
	TokenCount int        `json:"token_count" db:"token_count"`
	CreatedAt  time.Time  `json:"created_at" db:"created_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty" db:"expires_at"`

	// Phase 10 — dreaming weighted scoring signals. Populated by
	// EpisodicStore.RecordRecall; consumed by consolidation.ComputeRecallScore.
	RecallCount    int        `json:"recall_count" db:"recall_count"`
	RecallScore    float64    `json:"recall_score" db:"recall_score"`         // running average of memory_search hit scores
	LastRecalledAt *time.Time `json:"last_recalled_at,omitempty" db:"last_recalled_at"`

	// Structured extraction from summary (decisions, actions, key entities).
	// Populated by episodic worker during consolidation.
	Metadata *EpisodicMetadata `json:"metadata,omitempty" db:"metadata"`
}

// EpisodicMetadata contains structured data extracted from session summaries.
type EpisodicMetadata struct {
	Decisions   []EpisodicDecision   `json:"decisions,omitempty"`
	ActionItems []EpisodicActionItem `json:"action_items,omitempty"`
	Entities    []string             `json:"entities,omitempty"` // key entity names mentioned
}

// EpisodicDecision represents a decision made during a session.
type EpisodicDecision struct {
	Content string `json:"content"`
	Status  string `json:"status,omitempty"` // "confirmed", "pending", "rejected"
}

// EpisodicActionItem represents a task or follow-up from a session.
type EpisodicActionItem struct {
	Content  string `json:"content"`
	Assignee string `json:"assignee,omitempty"`
	DueDate  string `json:"due_date,omitempty"` // ISO date string
	Status   string `json:"status,omitempty"`   // "pending", "in_progress", "done"
}

// EpisodicSearchResult is a search hit with L0 summary.
type EpisodicSearchResult struct {
	EpisodicID string    `json:"episodic_id" db:"episodic_id"`
	L0Abstract string    `json:"l0_abstract" db:"l0_abstract"`
	Score      float64   `json:"score" db:"score"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
	SessionKey string    `json:"session_key" db:"session_key"`
}

// EpisodicSearchOptions configures episodic search behavior.
type EpisodicSearchOptions struct {
	MaxResults   int
	MinScore     float64
	VectorWeight float64
	TextWeight   float64

	// SessionKeyPrefix filters/boosts results from the same session context.
	// e.g., "agent:nta-leader:nta-telegram-ops:group:-123" matches memories
	// from the same group. Empty = no session-aware filtering.
	SessionKeyPrefix string

	// SameSessionBoost adds this value to scores for memories matching
	// SessionKeyPrefix. Default 0.3. Applied after weight normalization.
	SameSessionBoost float64
}

// EpisodicStore manages Tier 2 episodic memory.
// Implementations MUST extract tenant_id from context via store.TenantIDFromContext(ctx)
// and scope all queries by it. Cross-tenant retrieval is a security violation.
type EpisodicStore interface {
	// CRUD
	Create(ctx context.Context, ep *EpisodicSummary) error
	Get(ctx context.Context, id string) (*EpisodicSummary, error)
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, agentID, userID string, limit, offset int) ([]EpisodicSummary, error)

	// Search (hybrid FTS + vector, returns L0 by default)
	Search(ctx context.Context, query string, agentID, userID string, opts EpisodicSearchOptions) ([]EpisodicSearchResult, error)

	// Lifecycle
	ExistsBySourceID(ctx context.Context, agentID, userID, sourceID string) (bool, error)
	PruneExpired(ctx context.Context) (int, error)

	// Promotion lifecycle (used by consolidation pipeline)
	// ListUnpromoted returns summaries not yet promoted to long-term memory, oldest first.
	ListUnpromoted(ctx context.Context, agentID, userID string, limit int) ([]EpisodicSummary, error)
	// ListUnpromotedScored returns unpromoted summaries ordered by recall_score DESC
	// (fallback: created_at ASC). Used by the dreaming worker to prioritise entries
	// with stronger recall signals — see internal/consolidation/scoring.go.
	ListUnpromotedScored(ctx context.Context, agentID, userID string, limit int) ([]EpisodicSummary, error)
	// MarkPromoted sets promoted_at=now() for the given IDs.
	MarkPromoted(ctx context.Context, ids []string) error
	// CountUnpromoted returns the count of unpromoted summaries for an agent/user.
	CountUnpromoted(ctx context.Context, agentID, userID string) (int, error)
	// RecordRecall updates the per-episode recall signal after a memory_search hit.
	// Implementations must increment recall_count, fold `score` into the running
	// average stored in recall_score, and set last_recalled_at=NOW().
	RecordRecall(ctx context.Context, id string, score float64) error

	// Embedding
	SetEmbeddingProvider(provider EmbeddingProvider)
	Close() error
}

// DailyDigest represents an aggregated daily report from episodic summaries.
// Replaces manual "daily log" cron jobs with structured, queryable data.
type DailyDigest struct {
	ID               uuid.UUID  `json:"id" db:"id"`
	TenantID         uuid.UUID  `json:"tenant_id" db:"tenant_id"`
	AgentID          uuid.UUID  `json:"agent_id" db:"agent_id"`
	UserID           string     `json:"user_id" db:"user_id"`
	ChannelScope     string     `json:"channel_scope" db:"channel_scope"`           // channel filter (empty = all)
	SessionKeyPrefix string     `json:"session_key_prefix" db:"session_key_prefix"` // group prefix filter
	DigestDate       time.Time  `json:"digest_date" db:"digest_date"`
	Decisions        []EpisodicDecision   `json:"decisions" db:"decisions"`
	ActionItems      []EpisodicActionItem `json:"action_items" db:"action_items"`
	KeyTopics        []string   `json:"key_topics" db:"key_topics"`
	Summary          string     `json:"summary" db:"summary"`
	SessionCount     int        `json:"session_count" db:"session_count"`
	MessageCount     int        `json:"message_count" db:"message_count"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at" db:"updated_at"`
}

// DailyDigestStore manages daily digest aggregation.
type DailyDigestStore interface {
	// Upsert creates or updates a daily digest for the given date and scope.
	Upsert(ctx context.Context, digest *DailyDigest) error
	// Get retrieves a daily digest by agent, user, date, and optional scope.
	Get(ctx context.Context, agentID, userID string, date time.Time, channelScope string) (*DailyDigest, error)
	// List returns recent daily digests for an agent/user.
	List(ctx context.Context, agentID, userID string, limit int) ([]DailyDigest, error)
	// ListByDateRange returns digests within a date range.
	ListByDateRange(ctx context.Context, agentID, userID string, from, to time.Time) ([]DailyDigest, error)
}
