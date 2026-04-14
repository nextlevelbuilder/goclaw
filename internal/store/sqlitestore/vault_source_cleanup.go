//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// DeleteLinksBySource removes vault_links rows whose metadata->>'source'
// equals the given source key. Mirrors PG variant — tenant isolation via
// subquery on vault_documents since SQLite DELETE lacks USING.
func (s *SQLiteVaultStore) DeleteLinksBySource(ctx context.Context, tenantID, source string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM vault_links
		WHERE json_extract(metadata, '$.source') = ?
		  AND (
		    from_doc_id IN (SELECT id FROM vault_documents WHERE tenant_id = ?)
		    OR to_doc_id IN (SELECT id FROM vault_documents WHERE tenant_id = ?)
		  )
	`, source, tenantID, tenantID)
	if err != nil {
		return 0, fmt.Errorf("delete links by source: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// BatchFindByDelegationIDs returns vault docs sharing any of the given
// delegation_ids, grouped by delegation_id, capped per bucket at `limit`.
// Uses the SQLite partial index idx_vault_docs_delegation added by the v15
// → v16 migration (json_extract based).
func (s *SQLiteVaultStore) BatchFindByDelegationIDs(
	ctx context.Context,
	tenantID string,
	delegationIDs []string,
	limit int,
	excludeDocIDs []string,
) (map[string][]store.VaultDocument, error) {
	if len(delegationIDs) == 0 || limit <= 0 {
		return nil, nil
	}

	delegPH := make([]string, len(delegationIDs))
	args := []any{tenantID}
	for i, d := range delegationIDs {
		delegPH[i] = "?"
		args = append(args, d)
	}
	var excludeClause string
	if len(excludeDocIDs) > 0 {
		excludePH := make([]string, len(excludeDocIDs))
		for i, id := range excludeDocIDs {
			excludePH[i] = "?"
			args = append(args, id)
		}
		excludeClause = " AND id NOT IN (" + strings.Join(excludePH, ",") + ")"
	}
	args = append(args, limit)

	q := fmt.Sprintf(`
WITH ranked AS (
  SELECT
    id, tenant_id, agent_id, team_id, scope, custom_scope, path, path_basename,
    title, doc_type, content_hash, summary, metadata, created_at, updated_at,
    json_extract(metadata, '$.delegation_id') AS deleg_id,
    ROW_NUMBER() OVER (
      PARTITION BY json_extract(metadata, '$.delegation_id')
      ORDER BY created_at DESC, id DESC
    ) AS rn
  FROM vault_documents
  WHERE tenant_id = ?
    AND json_extract(metadata, '$.delegation_id') IN (%s)
    %s
)
SELECT id, tenant_id, agent_id, team_id, scope, custom_scope, path, path_basename,
       title, doc_type, content_hash, summary, metadata, created_at, updated_at, deleg_id
FROM ranked
WHERE rn <= ?
ORDER BY deleg_id, created_at DESC
`, strings.Join(delegPH, ","), excludeClause)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("batch find by delegation: %w", err)
	}
	defer rows.Close()

	out := make(map[string][]store.VaultDocument, len(delegationIDs))
	for rows.Next() {
		var doc store.VaultDocument
		var agentID *string
		var metaJSON []byte
		var delegID string
		ca, ua := &sqliteTime{}, &sqliteTime{}
		if err := rows.Scan(
			&doc.ID, &doc.TenantID, &agentID, &doc.TeamID, &doc.Scope, &doc.CustomScope,
			&doc.Path, &doc.PathBasename, &doc.Title, &doc.DocType, &doc.ContentHash,
			&doc.Summary, &metaJSON, ca, ua, &delegID,
		); err != nil {
			return nil, err
		}
		doc.AgentID = agentID
		doc.CreatedAt = ca.Time
		doc.UpdatedAt = ua.Time
		if len(metaJSON) > 2 {
			_ = json.Unmarshal(metaJSON, &doc.Metadata)
		}
		out[delegID] = append(out[delegID], doc)
	}
	return out, rows.Err()
}
