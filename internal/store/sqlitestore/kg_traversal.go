//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Traverse walks the knowledge graph from startEntityID up to maxDepth hops
// using a recursive CTE with comma-delimited path for cycle detection.
// Clamps maxDepth to 5. Returns all reachable entities (depth > 1).
func (s *SQLiteKnowledgeGraphStore) Traverse(ctx context.Context, agentID, userID, startEntityID string, maxDepth int) ([]store.TraversalResult, error) {
	if maxDepth <= 0 {
		maxDepth = 3
	}
	if maxDepth > 5 {
		maxDepth = 5
	}

	sc, scArgs, _ := scopeClause(ctx)

	var q string
	var args []any

	if store.IsSharedKG(ctx) {
		// Shared: filter by agent_id only, no user_id restriction
		q = `
		WITH RECURSIVE paths AS (
			SELECT
				e.id, e.agent_id, e.user_id, e.external_id,
				e.name, e.entity_type, e.description,
				e.properties, e.source_id, e.confidence,
				e.created_at, e.updated_at,
				1 AS depth,
				',' || e.id || ',' AS path,
				'' AS via
			FROM kg_entities e
			WHERE e.id = ? AND e.agent_id = ? AND e.valid_until IS NULL` + sc + `

			UNION ALL

			SELECT
				e.id, e.agent_id, e.user_id, e.external_id,
				e.name, e.entity_type, e.description,
				e.properties, e.source_id, e.confidence,
				e.created_at, e.updated_at,
				p.depth + 1,
				p.path || e.id || ',',
				CASE WHEN r.source_entity_id = p.id
					THEN r.relation_type
					ELSE '~' || r.relation_type
				END
			FROM paths p
			JOIN kg_relations r ON (r.source_entity_id = p.id OR r.target_entity_id = p.id)
				AND r.agent_id = ? AND r.valid_until IS NULL
			JOIN kg_entities e ON e.id = (CASE WHEN r.source_entity_id = p.id
				THEN r.target_entity_id ELSE r.source_entity_id END)
				AND e.agent_id = ? AND e.valid_until IS NULL
			WHERE p.depth < ?
			  AND p.path NOT LIKE '%,' || e.id || ',%'
		)
		SELECT id, agent_id, user_id, external_id, name, entity_type, description,
		       properties, source_id, confidence, created_at, updated_at,
		       depth, path, via
		FROM paths WHERE depth > 1
		LIMIT 500`

		args = append([]any{startEntityID, agentID}, scArgs...)
		args = append(args, agentID, agentID, maxDepth)
	} else {
		// User-scoped: filter by agent_id + user_id
		q = `
		WITH RECURSIVE paths AS (
			SELECT
				e.id, e.agent_id, e.user_id, e.external_id,
				e.name, e.entity_type, e.description,
				e.properties, e.source_id, e.confidence,
				e.created_at, e.updated_at,
				1 AS depth,
				',' || e.id || ',' AS path,
				'' AS via
			FROM kg_entities e
			WHERE e.id = ? AND e.agent_id = ? AND e.user_id = ? AND e.valid_until IS NULL` + sc + `

			UNION ALL

			SELECT
				e.id, e.agent_id, e.user_id, e.external_id,
				e.name, e.entity_type, e.description,
				e.properties, e.source_id, e.confidence,
				e.created_at, e.updated_at,
				p.depth + 1,
				p.path || e.id || ',',
				CASE WHEN r.source_entity_id = p.id
					THEN r.relation_type
					ELSE '~' || r.relation_type
				END
			FROM paths p
			JOIN kg_relations r ON (r.source_entity_id = p.id OR r.target_entity_id = p.id)
				AND r.user_id = ? AND r.valid_until IS NULL
			JOIN kg_entities e ON e.id = (CASE WHEN r.source_entity_id = p.id
				THEN r.target_entity_id ELSE r.source_entity_id END)
				AND e.user_id = ? AND e.valid_until IS NULL
			WHERE p.depth < ?
			  AND p.path NOT LIKE '%,' || e.id || ',%'
		)
		SELECT id, agent_id, user_id, external_id, name, entity_type, description,
		       properties, source_id, confidence, created_at, updated_at,
		       depth, path, via
		FROM paths WHERE depth > 1
		LIMIT 500`

		args = append([]any{startEntityID, agentID, userID}, scArgs...)
		args = append(args, userID, userID, maxDepth)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []store.TraversalResult
	for rows.Next() {
		var e store.Entity
		var props []byte
		var createdAt, updatedAt any
		var depth int
		var pathStr, via string

		err := rows.Scan(
			&e.ID, &e.AgentID, &e.UserID, &e.ExternalID,
			&e.Name, &e.EntityType, &e.Description,
			&props, &e.SourceID, &e.Confidence,
			&createdAt, &updatedAt,
			&depth, &pathStr, &via,
		)
		if err != nil {
			return nil, err
		}
		e.CreatedAt = scanUnixTimestamp(createdAt)
		e.UpdatedAt = scanUnixTimestamp(updatedAt)
		if len(props) > 0 {
			p, _ := scanJSONStringMap(props)
			e.Properties = p
		}

		results = append(results, store.TraversalResult{
			Entity: e,
			Depth:  depth,
			Path:   parsePathString(pathStr),
			Via:    via,
		})
	}
	return results, rows.Err()
}

// parsePathString converts a comma-delimited path like ",id1,id2,id3," to []string{"id1","id2","id3"}.
// Entity IDs are UUIDs (no commas), so splitting on comma is safe.
func parsePathString(path string) []string {
	// Strip leading/trailing commas then split
	path = strings.Trim(path, ",")
	if path == "" {
		return nil
	}
	return strings.Split(path, ",")
}
