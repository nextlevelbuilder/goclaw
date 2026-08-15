package pg

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGKGGraphStore implements store.KGGraphStore backed by PostgreSQL.
type PGKGGraphStore struct {
	db *sql.DB
}

// NewPGKGGraphStore creates a new PG-backed KG graph store.
func NewPGKGGraphStore(db *sql.DB) *PGKGGraphStore {
	return &PGKGGraphStore{db: db}
}

// ListKGGraphNodes returns lightweight KG entities for graph visualization.
func (s *PGKGGraphStore) ListKGGraphNodes(ctx context.Context, agentID, userID string, limit int) ([]store.KGGraphNode, int, error) {
	aid := parseUUIDOrNil(agentID)
	if limit <= 0 {
		limit = 2000
	}
	if limit > 10000 {
		limit = 10000
	}

	tid := store.TenantIDFromContext(ctx)

	q := `SELECT id, name, entity_type, confidence
		FROM kg_entities WHERE agent_id = $1 AND tenant_id = $2 AND valid_until IS NULL`
	args := []any{aid, tid}
	p := 3

	if !store.IsSharedKG(ctx) && userID != "" {
		q += fmt.Sprintf(" AND user_id = $%d", p)
		args = append(args, userID)
		p++
	}

	q += " ORDER BY updated_at DESC"
	q += fmt.Sprintf(" LIMIT $%d", p)
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("kg graph nodes: %w", err)
	}
	defer rows.Close()

	var nodes []store.KGGraphNode
	for rows.Next() {
		var n store.KGGraphNode
		if err := rows.Scan(&n.ID, &n.Name, &n.EntityType, &n.Confidence); err != nil {
			return nil, 0, fmt.Errorf("kg graph nodes scan: %w", err)
		}
		nodes = append(nodes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("kg graph nodes rows: %w", err)
	}
	return nodes, len(nodes), nil
}

// ListKGGraphEdges returns lightweight KG relations for graph visualization.
func (s *PGKGGraphStore) ListKGGraphEdges(ctx context.Context, agentID, userID string, limit int) ([]store.KGGraphEdge, int, error) {
	aid := parseUUIDOrNil(agentID)
	if limit <= 0 {
		limit = 6000
	}
	if limit > 30000 {
		limit = 30000
	}

	tid := store.TenantIDFromContext(ctx)

	q := `SELECT id, source_entity_id, target_entity_id, relation_type
		FROM kg_relations WHERE agent_id = $1 AND tenant_id = $2 AND valid_until IS NULL`
	args := []any{aid, tid}
	p := 3

	if !store.IsSharedKG(ctx) && userID != "" {
		q += fmt.Sprintf(" AND user_id = $%d", p)
		args = append(args, userID)
		p++
	}

	q += fmt.Sprintf(" LIMIT $%d", p)
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("kg graph edges: %w", err)
	}
	defer rows.Close()

	var edges []store.KGGraphEdge
	for rows.Next() {
		var e store.KGGraphEdge
		if err := rows.Scan(&e.ID, &e.SourceID, &e.TargetID, &e.RelationType); err != nil {
			return nil, 0, fmt.Errorf("kg graph edges scan: %w", err)
		}
		edges = append(edges, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("kg graph edges rows: %w", err)
	}
	return edges, len(edges), nil
}
