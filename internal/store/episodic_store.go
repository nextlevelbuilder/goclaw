package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// EpisodicSummary represents a Tier 2 episodic memory entry.
// Created from session summaries via the consolidation pipeline.
type EpisodicSummary struct {
	ID           uuid.UUID  `json:"id" db:"id"`
	TenantID     uuid.UUID  `json:"tenant_id" db:"tenant_id"`
	AgentID      uuid.UUID  `json:"agent_id" db:"agent_id"`
	UserID       string     `json:"user_id" db:"user_id"` // string: chat-based IDs
	SessionKey   string     `json:"session_key" db:"session_key"`
	Summary      string     `json:"summary" db:"summary"`
	KeyTopics    []string   `json:"key_topics" db:"key_topics"`
	L0Abstract   string     `json:"l0_abstract" db:"l0_abstract"` // ~50 tokens, pre-computed
	SourceType   string     `json:"source_type" db:"source_type"` // "session", "v2_daily", "manual"
	SourceID     string     `json:"source_id" db:"source_id"`     // dedup key
	TurnCount    int        `json:"turn_count" db:"turn_count"`
	TokenCount   int        `json:"token_count" db:"token_count"`
	CreatedAt    time.Time  `json:"created_at" db:"created_at"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty" db:"expires_at"`
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
	// MarkPromoted sets promoted_at=now() for the given IDs.
	MarkPromoted(ctx context.Context, ids []string) error
	// CountUnpromoted returns the count of unpromoted summaries for an agent/user.
	CountUnpromoted(ctx context.Context, agentID, userID string) (int, error)

	// Embedding
	SetEmbeddingProvider(provider EmbeddingProvider)
	Close() error
}
