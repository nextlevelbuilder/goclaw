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

	IngestExtraction(ctx context.Context, agentID, userID string, entities []Entity, relations []Relation) error
	PruneByConfidence(ctx context.Context, agentID, userID string, minConfidence float64) (int, error)

	Stats(ctx context.Context, agentID, userID string) (*GraphStats, error)

	// SetEmbeddingProvider configures the embedding provider for semantic search.
	SetEmbeddingProvider(provider EmbeddingProvider)

	Close() error
}
