//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteKGGraphStore implements store.KGGraphStore backed by SQLite.
type SQLiteKGGraphStore struct {
	db *sql.DB
}

// NewSQLiteKGGraphStore creates a new SQLite-backed KG graph store.
func NewSQLiteKGGraphStore(db *sql.DB) *SQLiteKGGraphStore {
	return &SQLiteKGGraphStore{db: db}
}

// ListKGGraphNodes returns lightweight KG entities for graph visualization.
func (s *SQLiteKGGraphStore) ListKGGraphNodes(ctx context.Context, agentID, userID string, limit int) ([]store.KGGraphNode, int, error) {
	if limit <= 0 {
		limit = 2000
	}
	if limit > 10000 {
		limit = 10000
	}

	q := `SELECT id, name, entity_type, confidence
		FROM kg_entities WHERE agent_id = ? AND valid_until IS NULL`
	args := []any{agentID}

	userClause, userArgs := kgUserClauseFor(ctx, userID)
	if userClause != "" {
		q += userClause
		args = append(args, userArgs...)
	}

	q += " ORDER BY updated_at DESC LIMIT ?"
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
func (s *SQLiteKGGraphStore) ListKGGraphEdges(ctx context.Context, agentID, userID string, limit int) ([]store.KGGraphEdge, int, error) {
	if limit <= 0 {
		limit = 6000
	}
	if limit > 30000 {
		limit = 30000
	}

	q := `SELECT id, source_entity_id, target_entity_id, relation_type
		FROM kg_relations WHERE agent_id = ? AND valid_until IS NULL`
	args := []any{agentID}

	userClause, userArgs := kgUserClauseFor(ctx, userID)
	if userClause != "" {
		q += userClause
		args = append(args, userArgs...)
	}

	q += " LIMIT ?"
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
