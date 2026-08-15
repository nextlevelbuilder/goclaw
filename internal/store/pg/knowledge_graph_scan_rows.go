package pg

import (
	"encoding/json"
	"time"

	"github.com/lib/pq"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// entityRow is an sqlx scan struct for kg_entities SELECT queries.
// Handles jsonb→json.RawMessage and timestamptz→time.Time conversion.
type entityRow struct {
	ID          string          `db:"id"`
	AgentID     string          `db:"agent_id"`
	UserID      string          `db:"user_id"`
	ExternalID  string          `db:"external_id"`
	Name        string          `db:"name"`
	EntityType  string          `db:"entity_type"`
	Description string          `db:"description"`
	Properties  json.RawMessage `db:"properties"`
	SourceID    string          `db:"source_id"`
	Confidence  float64         `db:"confidence"`
	CreatedAt   time.Time       `db:"created_at"`
	UpdatedAt   time.Time       `db:"updated_at"`
}

// toEntity converts an entityRow to store.Entity, unmarshaling properties and converting timestamps.
func (r *entityRow) toEntity() store.Entity {
	e := store.Entity{
		ID:          r.ID,
		AgentID:     r.AgentID,
		UserID:      r.UserID,
		ExternalID:  r.ExternalID,
		Name:        r.Name,
		EntityType:  r.EntityType,
		Description: r.Description,
		SourceID:    r.SourceID,
		Confidence:  r.Confidence,
		CreatedAt:   r.CreatedAt.UnixMilli(),
		UpdatedAt:   r.UpdatedAt.UnixMilli(),
	}
	if len(r.Properties) > 0 {
		_ = json.Unmarshal(r.Properties, &e.Properties)
	}
	return e
}

// scoredEntityRow extends entityRow with a score column for FTS/vector search results.
type scoredEntityRow struct {
	entityRow
	Score float64 `db:"score"`
}

// entityTemporalRow extends entityRow with valid_from/valid_until for temporal queries.
type entityTemporalRow struct {
	entityRow
	ValidFrom  *time.Time `db:"valid_from"`
	ValidUntil *time.Time `db:"valid_until"`
}

// toEntity converts an entityTemporalRow to store.Entity including temporal fields.
func (r *entityTemporalRow) toEntity() store.Entity {
	e := r.entityRow.toEntity()
	e.ValidFrom = r.ValidFrom
	e.ValidUntil = r.ValidUntil
	return e
}

// relationRow is an sqlx scan struct for kg_relations SELECT queries.
type relationRow struct {
	ID             string          `db:"id"`
	AgentID        string          `db:"agent_id"`
	UserID         string          `db:"user_id"`
	SourceEntityID string          `db:"source_entity_id"`
	RelationType   string          `db:"relation_type"`
	TargetEntityID string          `db:"target_entity_id"`
	Confidence     float64         `db:"confidence"`
	Properties     json.RawMessage `db:"properties"`
	CreatedAt      time.Time       `db:"created_at"`
}

// toRelation converts a relationRow to store.Relation.
func (r *relationRow) toRelation() store.Relation {
	rel := store.Relation{
		ID:             r.ID,
		AgentID:        r.AgentID,
		UserID:         r.UserID,
		SourceEntityID: r.SourceEntityID,
		RelationType:   r.RelationType,
		TargetEntityID: r.TargetEntityID,
		Confidence:     r.Confidence,
		CreatedAt:      r.CreatedAt.UnixMilli(),
	}
	if len(r.Properties) > 0 {
		_ = json.Unmarshal(r.Properties, &rel.Properties)
	}
	return rel
}

// relationExportRow extends relationRow with valid_from/valid_until for export queries.
type relationExportRow struct {
	relationRow
	ValidFrom  *time.Time `db:"valid_from"`
	ValidUntil *time.Time `db:"valid_until"`
}

// toRelation converts a relationExportRow to store.Relation including temporal fields.
func (r *relationExportRow) toRelation() store.Relation {
	rel := r.relationRow.toRelation()
	rel.ValidFrom = r.ValidFrom
	rel.ValidUntil = r.ValidUntil
	return rel
}

// traversalRow is an sqlx scan struct for the recursive CTE traversal query.
type traversalRow struct {
	entityRow
	Depth int            `db:"depth"`
	Path  pq.StringArray `db:"path"`
	Via   string         `db:"via"`
}

// toTraversalResult converts a traversalRow to store.TraversalResult.
func (r *traversalRow) toTraversalResult() store.TraversalResult {
	return store.TraversalResult{
		Entity: r.entityRow.toEntity(),
		Depth:  r.Depth,
		Path:   []string(r.Path),
		Via:    r.Via,
	}
}

// dedupCandidateRow is an sqlx scan struct for the kg_dedup_candidates JOIN query.
type dedupCandidateRow struct {
	ID         string          `db:"id"`
	Similarity float64         `db:"similarity"`
	Status     string          `db:"status"`
	CreatedAt  time.Time       `db:"created_at"`
	AID        string          `db:"a_id"`
	AAgentID   string          `db:"a_agent_id"`
	AUserID    string          `db:"a_user_id"`
	AExtID     string          `db:"a_external_id"`
	AName      string          `db:"a_entity_name"`
	AType      string          `db:"a_entity_type"`
	ADesc      string          `db:"a_description"`
	AProps     json.RawMessage `db:"a_properties"`
	ASourceID  string          `db:"a_source_id"`
	AConf      float64         `db:"a_confidence"`
	ACreatedAt time.Time       `db:"a_created_at"`
	AUpdatedAt time.Time       `db:"a_updated_at"`
	BID        string          `db:"b_id"`
	BAgentID   string          `db:"b_agent_id"`
	BUserID    string          `db:"b_user_id"`
	BExtID     string          `db:"b_external_id"`
	BName      string          `db:"b_entity_name"`
	BType      string          `db:"b_entity_type"`
	BDesc      string          `db:"b_description"`
	BProps     json.RawMessage `db:"b_properties"`
	BSourceID  string          `db:"b_source_id"`
	BConf      float64         `db:"b_confidence"`
	BCreatedAt time.Time       `db:"b_created_at"`
	BUpdatedAt time.Time       `db:"b_updated_at"`
}

// toDedupCandidate converts a dedupCandidateRow to store.DedupCandidate.
func (r *dedupCandidateRow) toDedupCandidate() store.DedupCandidate {
	dc := store.DedupCandidate{
		ID:         r.ID,
		Similarity: r.Similarity,
		Status:     r.Status,
		CreatedAt:  r.CreatedAt.UnixMilli(),
	}
	dc.EntityA = store.Entity{
		ID: r.AID, AgentID: r.AAgentID, UserID: r.AUserID, ExternalID: r.AExtID,
		Name: r.AName, EntityType: r.AType, Description: r.ADesc,
		SourceID: r.ASourceID, Confidence: r.AConf,
		CreatedAt: r.ACreatedAt.UnixMilli(), UpdatedAt: r.AUpdatedAt.UnixMilli(),
	}
	if len(r.AProps) > 0 {
		_ = json.Unmarshal(r.AProps, &dc.EntityA.Properties)
	}
	dc.EntityB = store.Entity{
		ID: r.BID, AgentID: r.BAgentID, UserID: r.BUserID, ExternalID: r.BExtID,
		Name: r.BName, EntityType: r.BType, Description: r.BDesc,
		SourceID: r.BSourceID, Confidence: r.BConf,
		CreatedAt: r.BCreatedAt.UnixMilli(), UpdatedAt: r.BUpdatedAt.UnixMilli(),
	}
	if len(r.BProps) > 0 {
		_ = json.Unmarshal(r.BProps, &dc.EntityB.Properties)
	}
	return dc
}

// knnNeighborRow is an sqlx scan struct for KNN neighbor queries in dedup.
type knnNeighborRow struct {
	ID         string  `db:"id"`
	Name       string  `db:"name"`
	Confidence float64 `db:"confidence"`
	Similarity float64 `db:"similarity"`
}
