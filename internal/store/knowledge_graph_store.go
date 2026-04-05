package store

import "context"

// Entity represents a node in the knowledge graph.
type Entity struct {
	ID          string            `json:"id"`
	AgentID     string            `json:"agent_id"`
	UserID      string            `json:"user_id,omitempty"`
	ExternalID  string            `json:"external_id"`
	Name        string            `json:"name"`
	EntityType  string            `json:"entity_type"`
	Description string            `json:"description,omitempty"`
	Properties  map[string]string `json:"properties,omitempty"`
	SourceID    string            `json:"source_id,omitempty"`
	Confidence  float64           `json:"confidence"`
	CreatedAt   int64             `json:"created_at"`
	UpdatedAt   int64             `json:"updated_at"`
}

// Relation represents an edge between two entities.
type Relation struct {
	ID             string            `json:"id"`
	AgentID        string            `json:"agent_id"`
	UserID         string            `json:"user_id,omitempty"`
	SourceEntityID string            `json:"source_entity_id"`
	RelationType   string            `json:"relation_type"`
	TargetEntityID string            `json:"target_entity_id"`
	Confidence     float64           `json:"confidence"`
	Properties     map[string]string `json:"properties,omitempty"`
	CreatedAt      int64             `json:"created_at"`
}

// TraversalResult is a connected entity with path info.
type TraversalResult struct {
	Entity Entity   `json:"entity"`
	Depth  int      `json:"depth"`
	Path   []string `json:"path"`
	Via    string   `json:"via"`
}

// EntityListOptions configures a list query for entities.
type EntityListOptions struct {
	EntityType string
	Limit      int
	Offset     int
}

// GraphStats contains aggregate counts for a scoped graph.
type GraphStats struct {
	EntityCount   int            `json:"entity_count"`
	RelationCount int            `json:"relation_count"`
	EntityTypes   map[string]int `json:"entity_types"`
}

// DedupCandidate represents a pair of entities that may be duplicates.
type DedupCandidate struct {
	ID         string  `json:"id"`
	EntityA    Entity  `json:"entity_a"`
	EntityB    Entity  `json:"entity_b"`
	Similarity float64 `json:"similarity"`
	Status     string  `json:"status"`
	CreatedAt  int64   `json:"created_at"`
}

// KnowledgeGraphStore manages entity-relationship graphs.
type KnowledgeGraphStore interface {
	UpsertEntity(ctx context.Context, entity *Entity) error
	GetEntity(ctx context.Context, agentID, userID, entityID string) (*Entity, error)
	DeleteEntity(ctx context.Context, agentID, userID, entityID string) error
	ListEntities(ctx context.Context, agentID, userID string, opts EntityListOptions) ([]Entity, error)
	SearchEntities(ctx context.Context, agentID, userID, query string, limit int) ([]Entity, error)

	UpsertRelation(ctx context.Context, relation *Relation) error
	DeleteRelation(ctx context.Context, agentID, userID, relationID string) error
	ListRelations(ctx context.Context, agentID, userID, entityID string) ([]Relation, error)
	// ListAllRelations returns all relations for an agent (optionally scoped by user).
	ListAllRelations(ctx context.Context, agentID, userID string, limit int) ([]Relation, error)

	Traverse(ctx context.Context, agentID, userID, startEntityID string, maxDepth int) ([]TraversalResult, error)

	// IngestExtraction upserts entities and relations from an LLM extraction.
	// Returns the DB UUIDs of all upserted entities for downstream processing (e.g. dedup).
	IngestExtraction(ctx context.Context, agentID, userID string, entities []Entity, relations []Relation) ([]string, error)
	PruneByConfidence(ctx context.Context, agentID, userID string, minConfidence float64) (int, error)

	// DedupAfterExtraction checks newly upserted entities for duplicates.
	// Auto-merges at high similarity (>0.98 + name match), flags medium (>0.90) as candidates.
	DedupAfterExtraction(ctx context.Context, agentID, userID string, newEntityIDs []string) (merged int, flagged int, err error)
	// ScanDuplicates scans ALL entities with embeddings for duplicates (self-join).
	// Flags candidates above threshold. Used for on-demand bulk scanning of existing data.
	ScanDuplicates(ctx context.Context, agentID, userID string, threshold float64, limit int) (int, error)
	// ListDedupCandidates returns pending dedup candidates for review.
	ListDedupCandidates(ctx context.Context, agentID, userID string, limit int) ([]DedupCandidate, error)
	// MergeEntities merges sourceID into targetID: re-points relations, deletes source.
	MergeEntities(ctx context.Context, agentID, userID, targetID, sourceID string) error
	// DismissCandidate marks a dedup candidate as dismissed (not a duplicate).
	// Scoped by agentID + tenant to prevent cross-agent dismissal.
	DismissCandidate(ctx context.Context, agentID, candidateID string) error

	Stats(ctx context.Context, agentID, userID string) (*GraphStats, error)

	// SetEmbeddingProvider configures the embedding provider for semantic search.
	SetEmbeddingProvider(provider EmbeddingProvider)

	Close() error
}
