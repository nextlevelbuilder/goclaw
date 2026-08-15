//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteVaultGraphStore implements store.VaultGraphStore backed by SQLite.
type SQLiteVaultGraphStore struct {
	db *sql.DB
}

// NewSQLiteVaultGraphStore creates a new SQLite-backed vault graph store.
func NewSQLiteVaultGraphStore(db *sql.DB) *SQLiteVaultGraphStore {
	return &SQLiteVaultGraphStore{db: db}
}

// ListGraphNodes returns lightweight vault nodes with pre-computed degree.
func (s *SQLiteVaultGraphStore) ListGraphNodes(ctx context.Context, tenantID, agentID string, opts store.VaultGraphListOptions) ([]store.GraphNode, int, error) {
	q := `SELECT vd.id, vd.title, vd.path, vd.doc_type,
		COALESCE(deg.cnt, 0) AS degree
		FROM vault_documents vd
		LEFT JOIN (
			SELECT doc_id, COUNT(*) AS cnt FROM (
				SELECT from_doc_id AS doc_id FROM vault_links vl
				JOIN vault_documents d ON d.id = vl.from_doc_id AND d.tenant_id = ?
				UNION ALL
				SELECT to_doc_id AS doc_id FROM vault_links vl
				JOIN vault_documents d ON d.id = vl.to_doc_id AND d.tenant_id = ?
			) sub GROUP BY doc_id
		) deg ON deg.doc_id = vd.id
		WHERE vd.tenant_id = ?`
	args := []any{tenantID, tenantID, tenantID}

	if agentID != "" {
		q += " AND vd.agent_id = ?"
		args = append(args, agentID)
	}

	q, args = sqliteAppendGraphTeamFilter(q, args, "vd", opts.TeamID, opts.TeamIDs)

	q += " ORDER BY degree DESC, vd.updated_at DESC"

	limit := opts.Limit
	if limit <= 0 {
		limit = 2000
	}
	if limit > 10000 {
		limit = 10000
	}
	q += " LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("vault graph nodes: %w", err)
	}
	defer rows.Close()

	var nodes []store.GraphNode
	for rows.Next() {
		var n store.GraphNode
		if err := rows.Scan(&n.ID, &n.Title, &n.Path, &n.DocType, &n.Degree); err != nil {
			return nil, 0, fmt.Errorf("vault graph nodes scan: %w", err)
		}
		nodes = append(nodes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("vault graph nodes rows: %w", err)
	}

	total, err := s.countGraphNodes(ctx, tenantID, agentID, opts)
	if err != nil {
		return nodes, len(nodes), nil
	}
	return nodes, total, nil
}

// ListGraphEdges returns lightweight vault edges for nodes in scope.
func (s *SQLiteVaultGraphStore) ListGraphEdges(ctx context.Context, tenantID, agentID string, opts store.VaultGraphListOptions) ([]store.GraphEdge, int, error) {
	// Build subquery for doc IDs in scope.
	subQ := `SELECT id FROM vault_documents WHERE tenant_id = ?`
	subArgs := []any{tenantID}

	if agentID != "" {
		subQ += " AND agent_id = ?"
		subArgs = append(subArgs, agentID)
	}

	subQ, subArgs = sqliteAppendGraphTeamFilter(subQ, subArgs, "", opts.TeamID, opts.TeamIDs)

	limit := opts.Limit
	if limit <= 0 {
		limit = 2000
	}
	if limit > 10000 {
		limit = 10000
	}
	subQ += " LIMIT ?"
	subArgs = append(subArgs, limit)

	q := fmt.Sprintf(`SELECT vl.id, vl.from_doc_id, vl.to_doc_id, vl.link_type
		FROM vault_links vl
		WHERE vl.from_doc_id IN (%s) AND vl.to_doc_id IN (%s)`, subQ, subQ)

	// Duplicate args for second IN subquery.
	args := make([]any, 0, len(subArgs)*2)
	args = append(args, subArgs...)
	args = append(args, subArgs...)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("vault graph edges: %w", err)
	}
	defer rows.Close()

	var edges []store.GraphEdge
	for rows.Next() {
		var e store.GraphEdge
		if err := rows.Scan(&e.ID, &e.FromID, &e.ToID, &e.LinkType); err != nil {
			return nil, 0, fmt.Errorf("vault graph edges scan: %w", err)
		}
		edges = append(edges, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("vault graph edges rows: %w", err)
	}
	return edges, len(edges), nil
}

func (s *SQLiteVaultGraphStore) countGraphNodes(ctx context.Context, tenantID, agentID string, opts store.VaultGraphListOptions) (int, error) {
	q := `SELECT COUNT(*) FROM vault_documents WHERE tenant_id = ?`
	args := []any{tenantID}

	if agentID != "" {
		q += " AND agent_id = ?"
		args = append(args, agentID)
	}
	q, args = sqliteAppendGraphTeamFilter(q, args, "", opts.TeamID, opts.TeamIDs)

	var count int
	if err := s.db.QueryRowContext(ctx, q, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// sqliteAppendGraphTeamFilter appends team_id clause for SQLite.
func sqliteAppendGraphTeamFilter(q string, args []any, tableAlias string, teamID *string, teamIDs []string) (string, []any) {
	col := "team_id"
	if tableAlias != "" {
		col = tableAlias + ".team_id"
	}

	if len(teamIDs) > 0 {
		ph := "?" + strings.Repeat(",?", len(teamIDs)-1)
		q += fmt.Sprintf(" AND (%s IS NULL OR %s IN (%s))", col, col, ph)
		for _, id := range teamIDs {
			args = append(args, id)
		}
	} else if teamID != nil {
		if *teamID != "" {
			q += fmt.Sprintf(" AND %s = ?", col)
			args = append(args, *teamID)
		} else {
			q += fmt.Sprintf(" AND %s IS NULL", col)
		}
	}
	return q, args
}

