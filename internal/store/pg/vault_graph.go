package pg

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGVaultGraphStore implements store.VaultGraphStore backed by PostgreSQL.
type PGVaultGraphStore struct {
	db *sql.DB
}

// NewPGVaultGraphStore creates a new PG-backed vault graph store.
func NewPGVaultGraphStore(db *sql.DB) *PGVaultGraphStore {
	return &PGVaultGraphStore{db: db}
}

// ListGraphNodes returns lightweight vault nodes with pre-computed degree.
func (s *PGVaultGraphStore) ListGraphNodes(ctx context.Context, tenantID, agentID string, opts store.VaultGraphListOptions) ([]store.GraphNode, int, error) {
	tid := parseUUIDOrNil(tenantID)

	q := `SELECT vd.id, vd.title, vd.path, vd.doc_type,
		COALESCE(deg.cnt, 0) AS degree
		FROM vault_documents vd
		LEFT JOIN (
			SELECT doc_id, COUNT(*) AS cnt FROM (
				SELECT from_doc_id AS doc_id FROM vault_links vl
				JOIN vault_documents d ON d.id = vl.from_doc_id AND d.tenant_id = $1
				UNION ALL
				SELECT to_doc_id AS doc_id FROM vault_links vl
				JOIN vault_documents d ON d.id = vl.to_doc_id AND d.tenant_id = $1
			) sub GROUP BY doc_id
		) deg ON deg.doc_id = vd.id
		WHERE vd.tenant_id = $1`
	args := []any{tid}
	p := 2

	if agentID != "" {
		aid := parseUUIDOrNil(agentID)
		q += fmt.Sprintf(" AND vd.agent_id = $%d", p)
		args = append(args, aid)
		p++
	}

	q, args, p = appendGraphTeamFilter(q, args, p, "vd", opts.TeamID, opts.TeamIDs)

	q += " ORDER BY degree DESC, vd.updated_at DESC"

	limit := opts.Limit
	if limit <= 0 {
		limit = 2000
	}
	if limit > 10000 {
		limit = 10000
	}
	q += fmt.Sprintf(" LIMIT $%d", p)
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

	// Count total nodes in scope (without LIMIT).
	total, err := s.countGraphNodes(ctx, tid, agentID, opts)
	if err != nil {
		return nodes, len(nodes), nil // non-fatal: return what we have
	}
	return nodes, total, nil
}

// ListGraphEdges returns lightweight vault edges for nodes in scope.
func (s *PGVaultGraphStore) ListGraphEdges(ctx context.Context, tenantID, agentID string, opts store.VaultGraphListOptions) ([]store.GraphEdge, int, error) {
	tid := parseUUIDOrNil(tenantID)

	// Subquery selects doc IDs in scope (same filters as ListGraphNodes).
	subQ := `SELECT id FROM vault_documents WHERE tenant_id = $1`
	args := []any{tid}
	p := 2

	if agentID != "" {
		aid := parseUUIDOrNil(agentID)
		subQ += fmt.Sprintf(" AND agent_id = $%d", p)
		args = append(args, aid)
		p++
	}

	subQ, args, p = appendGraphTeamFilter(subQ, args, p, "", opts.TeamID, opts.TeamIDs)

	limit := opts.Limit
	if limit <= 0 {
		limit = 2000
	}
	if limit > 10000 {
		limit = 10000
	}
	subQ += fmt.Sprintf(" LIMIT $%d", p)
	args = append(args, limit)
	p++

	q := fmt.Sprintf(`SELECT vl.id, vl.from_doc_id, vl.to_doc_id, vl.link_type
		FROM vault_links vl
		WHERE vl.from_doc_id IN (%s) AND vl.to_doc_id IN (%s)`, subQ, subQ)

	// PG positional params ($1,$2..) are reused across both IN subqueries — no duplication needed.
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

func (s *PGVaultGraphStore) countGraphNodes(ctx context.Context, tid any, agentID string, opts store.VaultGraphListOptions) (int, error) {
	q := `SELECT COUNT(*) FROM vault_documents WHERE tenant_id = $1`
	args := []any{tid}
	p := 2

	if agentID != "" {
		aid := parseUUIDOrNil(agentID)
		q += fmt.Sprintf(" AND agent_id = $%d", p)
		args = append(args, aid)
		p++
	}
	q, args, _ = appendGraphTeamFilter(q, args, p, "", opts.TeamID, opts.TeamIDs)

	var count int
	if err := s.db.QueryRowContext(ctx, q, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// appendGraphTeamFilter appends team_id clause. Optional tableAlias prefix (e.g. "vd").
func appendGraphTeamFilter(q string, args []any, p int, tableAlias string, teamID *string, teamIDs []string) (string, []any, int) {
	col := "team_id"
	if tableAlias != "" {
		col = tableAlias + ".team_id"
	}

	if len(teamIDs) > 0 {
		ph := make([]string, len(teamIDs))
		for i, id := range teamIDs {
			ph[i] = fmt.Sprintf("$%d", p)
			args = append(args, parseUUIDOrNil(id))
			p++
		}
		q += fmt.Sprintf(" AND (%s IS NULL OR %s IN (%s))", col, col, strings.Join(ph, ","))
	} else if teamID != nil {
		if *teamID != "" {
			q += fmt.Sprintf(" AND %s = $%d", col, p)
			args = append(args, parseUUIDOrNil(*teamID))
			p++
		} else {
			q += fmt.Sprintf(" AND %s IS NULL", col)
		}
	}
	return q, args, p
}
