package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ParseFlexibleTime attempts to parse a datetime string using multiple common
// formats. Returns nil if no format matches. Used for LLM-extracted timestamps.
func ParseFlexibleTime(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	for _, layout := range flexibleLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			utc := t.UTC()
			return &utc
		}
	}
	return nil
}

var flexibleLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02T15:04:05",
	"2006-01-02T15:04:05-07:00",
	"2006-01-02 15:04:05 -0700",
	"2006-01-02 15:04:05",
	"2006-01-02",
	"02-01-2006",
	"02 January 2006",
	"02 January 2006 15:04",
	"02 January 2006 15:04:05",
}

// Entity represents a node in the knowledge graph.
type Entity struct {
	ID          string            `json:"id" db:"id"`
	AgentID     string            `json:"agent_id" db:"agent_id"`
	UserID      string            `json:"user_id,omitempty" db:"user_id"`
	ExternalID  string            `json:"external_id" db:"external_id"`
	Name        string            `json:"name" db:"name"`
	EntityType  string            `json:"entity_type" db:"entity_type"`
	Description string            `json:"description,omitempty" db:"description"`
	Properties  map[string]string `json:"properties,omitempty" db:"properties"`
	SourceID    string            `json:"source_id,omitempty" db:"source_id"`
	Confidence  float64           `json:"confidence" db:"confidence"`
	CreatedAt   int64             `json:"created_at" db:"created_at"`
	UpdatedAt   int64             `json:"updated_at" db:"updated_at"`
	ValidFrom   *time.Time        `json:"valid_from,omitempty" db:"valid_from"`
	ValidUntil  *time.Time        `json:"valid_until,omitempty" db:"valid_until"`
	EventTime   *time.Time        `json:"-" db:"event_time"`
	// RawEventTime captures the LLM's event_time field for flexible parsing.
	RawEventTime any `json:"event_time,omitempty" db:"-"`
}

// UnmarshalJSON implements custom JSON unmarshaling for Entity.
// Handles cases where LLM returns non-string values in properties (numbers, bools).
func (e *Entity) UnmarshalJSON(b []byte) error {
	type Alias Entity
	aux := &struct {
		Properties json.RawMessage `json:"properties,omitempty"`
		*Alias
	}{
		Alias: (*Alias)(e),
	}
	if err := json.Unmarshal(b, aux); err != nil {
		return err
	}
	if len(aux.Properties) > 0 {
		var raw map[string]any
		if err := json.Unmarshal(aux.Properties, &raw); err == nil {
			e.Properties = make(map[string]string, len(raw))
			for k, v := range raw {
				e.Properties[k] = fmt.Sprintf("%v", v)
			}
		}
	}
	return nil
}

// Relation represents an edge between two entities.
type Relation struct {
	ID             string            `json:"id" db:"id"`
	AgentID        string            `json:"agent_id" db:"agent_id"`
	UserID         string            `json:"user_id,omitempty" db:"user_id"`
	SourceEntityID string            `json:"source_entity_id" db:"source_entity_id"`
	RelationType   string            `json:"relation_type" db:"relation_type"`
	TargetEntityID string            `json:"target_entity_id" db:"target_entity_id"`
	Confidence     float64           `json:"confidence" db:"confidence"`
	Properties     map[string]string `json:"properties,omitempty" db:"properties"`
	CreatedAt      int64             `json:"created_at" db:"created_at"`
	ValidFrom      *time.Time        `json:"valid_from,omitempty" db:"valid_from"`
	ValidUntil     *time.Time        `json:"valid_until,omitempty" db:"valid_until"`
}

// TraversalResult is a connected entity with path info.
type TraversalResult struct {
	Entity Entity   `json:"entity" db:"-"`
	Depth  int      `json:"depth" db:"-"`
	Path   []string `json:"path" db:"-"`
	Via    string   `json:"via" db:"-"`
}

// EntityListOptions configures a list query for entities.
type EntityListOptions struct {
	EntityType string
	ScopeID    string // filter by user_id (graph scope, e.g. "project-sovereign")
	Limit      int
	Offset     int
}

// GraphStats contains aggregate counts for a scoped graph.
type GraphStats struct {
	EntityCount   int            `json:"entity_count" db:"-"`
	RelationCount int            `json:"relation_count" db:"-"`
	EntityTypes   map[string]int `json:"entity_types" db:"-"`
	UserIDs       []string       `json:"user_ids,omitempty" db:"-"`
}

// DedupCandidate represents a pair of entities that may be duplicates.
type DedupCandidate struct {
	ID         string  `json:"id" db:"id"`
	EntityA    Entity  `json:"entity_a" db:"-"`
	EntityB    Entity  `json:"entity_b" db:"-"`
	Similarity float64 `json:"similarity" db:"similarity"`
	Status     string  `json:"status" db:"status"`
	CreatedAt  int64   `json:"created_at" db:"created_at"`
}

// KnowledgeGraphStore manages entity-relationship graphs.
type KnowledgeGraphStore interface {
	UpsertEntity(ctx context.Context, entity *Entity) error
	GetEntity(ctx context.Context, agentID, userID, entityID string) (*Entity, error)
	DeleteEntity(ctx context.Context, agentID, userID, entityID string) error
	// DeleteEntities deletes multiple entities by ID. Also removes relations referencing them.
	// Returns the number of entities deleted.
	DeleteEntities(ctx context.Context, agentID, userID string, entityIDs []string) (int, error)
	ListEntities(ctx context.Context, agentID, userID string, opts EntityListOptions) ([]Entity, error)
	SearchEntities(ctx context.Context, agentID, userID, query string, limit int) ([]Entity, error)
	// SearchEntitiesByEventTime returns entities whose event_time falls within [fromTime, toTime].
	// Either bound may be nil for open-ended ranges. Only returns entities with non-NULL event_time.
	SearchEntitiesByEventTime(ctx context.Context, agentID, userID string, fromTime, toTime *time.Time, limit int) ([]Entity, error)

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

	// ClearAll deletes all KG data (entities, relations, dedup candidates) for an agent.
	// If userID is non-empty, only clears data for that user scope.
	// Returns the total number of entities deleted.
	ClearAll(ctx context.Context, agentID, userID string) (int, error)

	// Temporal queries (v3)
	ListEntitiesTemporal(ctx context.Context, agentID, userID string, opts EntityListOptions, temporal TemporalQueryOptions) ([]Entity, error)
	SupersedeEntity(ctx context.Context, old *Entity, replacement *Entity) error

	// SetEmbeddingProvider configures the embedding provider for semantic search.
	SetEmbeddingProvider(provider EmbeddingProvider)

	Close() error
}
