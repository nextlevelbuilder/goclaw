package pg

import (
	"context"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Traverse walks the knowledge graph from startEntityID up to maxDepth hops
// using a recursive CTE. Returns all reachable entities (excluding the start node).
// A 5-second statement timeout is applied for safety.
func (s *PGKnowledgeGraphStore) Traverse(ctx context.Context, agentID, userID, startEntityID string, maxDepth int) ([]store.TraversalResult, error) {
	if maxDepth <= 0 {
		maxDepth = 3
	}

	aid := mustParseUUID(agentID)
	startID := mustParseUUID(startEntityID)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `SET LOCAL statement_timeout = '5000'`); err != nil {
		return nil, err
	}

	var q string
	var args []any
	if store.IsSharedKG(ctx) {
		// fixed params: $1=startID, $2=aid; tenant at $3 (if needed); maxDepth last
		tc, tcArgs, _, tcErr := scopeClause(ctx, 3)
		if tcErr != nil {
			return nil, tcErr
		}
		depthN := 3 + len(tcArgs)
		q = fmt.Sprintf(`
		WITH RECURSIVE paths AS (
			SELECT
				e.id, e.agent_id, e.user_id, e.external_id,
				e.name, e.entity_type, e.description,
				e.properties, e.source_id, e.confidence,
				e.created_at, e.updated_at,
				1 AS depth,
				ARRAY[e.id::text] AS path,
				''::text AS via
			FROM kg_entities e
			WHERE e.id = $1 AND e.agent_id = $2 AND e.valid_until IS NULL%s

			UNION ALL

			SELECT
				e.id, e.agent_id, e.user_id, e.external_id,
				e.name, e.entity_type, e.description,
				e.properties, e.source_id, e.confidence,
				e.created_at, e.updated_at,
				p.depth + 1,
				p.path || e.id::text,
				CASE WHEN r.source_entity_id = p.id
					THEN r.relation_type
					ELSE '~' || r.relation_type
				END
			FROM paths p
			JOIN kg_relations r ON (r.source_entity_id = p.id OR r.target_entity_id = p.id) AND r.agent_id = $2 AND r.valid_until IS NULL
			JOIN kg_entities  e ON e.id = (CASE WHEN r.source_entity_id = p.id THEN r.target_entity_id ELSE r.source_entity_id END) AND e.agent_id = $2 AND e.valid_until IS NULL
			WHERE p.depth < $%d
			  AND NOT e.id::text = ANY(p.path)
		)
		SELECT
			id, agent_id, user_id, external_id,
			name, entity_type, description,
			properties, source_id, confidence,
			created_at, updated_at,
			depth, path, via
		FROM paths WHERE depth > 1`, tc, depthN)
		args = append([]any{startID, aid}, tcArgs...)
		args = append(args, maxDepth)
	} else {
		// fixed params: $1=startID, $2=aid, $3=userID; tenant at $4 (if needed); maxDepth last
		tc, tcArgs, _, tcErr := scopeClause(ctx, 4)
		if tcErr != nil {
			return nil, tcErr
		}
		depthN := 4 + len(tcArgs)
		q = fmt.Sprintf(`
		WITH RECURSIVE paths AS (
			SELECT
				e.id, e.agent_id, e.user_id, e.external_id,
				e.name, e.entity_type, e.description,
				e.properties, e.source_id, e.confidence,
				e.created_at, e.updated_at,
				1 AS depth,
				ARRAY[e.id::text] AS path,
				''::text AS via
			FROM kg_entities e
			WHERE e.id = $1 AND e.agent_id = $2 AND e.user_id = $3 AND e.valid_until IS NULL%s

			UNION ALL

			SELECT
				e.id, e.agent_id, e.user_id, e.external_id,
				e.name, e.entity_type, e.description,
				e.properties, e.source_id, e.confidence,
				e.created_at, e.updated_at,
				p.depth + 1,
				p.path || e.id::text,
				CASE WHEN r.source_entity_id = p.id
					THEN r.relation_type
					ELSE '~' || r.relation_type
				END
			FROM paths p
			JOIN kg_relations r ON (r.source_entity_id = p.id OR r.target_entity_id = p.id) AND r.user_id = $3 AND r.valid_until IS NULL
			JOIN kg_entities  e ON e.id = (CASE WHEN r.source_entity_id = p.id THEN r.target_entity_id ELSE r.source_entity_id END) AND e.user_id = $3 AND e.valid_until IS NULL
			WHERE p.depth < $%d
			  AND NOT e.id::text = ANY(p.path)
		)
		SELECT
			id, agent_id, user_id, external_id,
			name, entity_type, description,
			properties, source_id, confidence,
			created_at, updated_at,
			depth, path, via
		FROM paths WHERE depth > 1`, tc, depthN)
		args = append([]any{startID, aid, userID}, tcArgs...)
		args = append(args, maxDepth)
	}

	// Use sqlx on the transaction for struct scanning with pq.StringArray support.
	txSqlx := sqlxTx(tx)
	var tRows []traversalRow
	if err = txSqlx.SelectContext(ctx, &tRows, q, args...); err != nil {
		return nil, err
	}
	results := make([]store.TraversalResult, len(tRows))
	for i := range tRows {
		results[i] = tRows[i].toTraversalResult()
	}
	return results, tx.Commit()
}
