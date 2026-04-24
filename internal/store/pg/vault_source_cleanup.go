package pg

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// DeleteLinksBySource removes vault_links rows whose metadata->>'source' equals
// the given source key (e.g. "task:{uuid}", "delegation:{uuid}"). Used by
// Phase 04/05 cleanup paths (DetachFileFromTask, DeleteTask, bulk deletion)
// to surgically remove auto-links without touching classify-owned links.
//
// Tenant isolation is enforced by joining vault_documents on tenant_id so a
// guessable source string can't delete another tenant's links.
func (s *PGVaultStore) DeleteLinksBySource(ctx context.Context, tenantID, source string) (int64, error) {
	tid, err := parseUUID(tenantID)
	if err != nil {
		return 0, fmt.Errorf("delete links by source: tenant: %w", err)
	}
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM vault_links vl
		USING vault_documents vd
		WHERE vl.metadata->>'source' = $1
		  AND vd.tenant_id = $2
		  AND (vl.from_doc_id = vd.id OR vl.to_doc_id = vd.id)
	`, source, tid)
	if err != nil {
		return 0, fmt.Errorf("delete links by source: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// BatchFindByDelegationIDs returns vault docs sharing any of the given
// delegation_ids, keyed by delegation_id. Each bucket is capped at `limit`.
// excludeDocIDs prevents self-link emission when the caller's source docs
// appear in the result set. Uses ROW_NUMBER() PARTITION BY delegation_id
// over the partial index idx_vault_docs_delegation added by migration 000048.
func (s *PGVaultStore) BatchFindByDelegationIDs(
	ctx context.Context,
	tenantID string,
	delegationIDs []string,
	limit int,
	excludeDocIDs []string,
) (map[string][]store.VaultDocument, error) {
	if len(delegationIDs) == 0 || limit <= 0 {
		return nil, nil
	}
	tid, err := parseUUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("batch find by delegation: tenant: %w", err)
	}

	// Exclude doc IDs are real UUIDs — parse strictly.
	excludeUUIDs := make([]string, 0, len(excludeDocIDs))
	for _, id := range excludeDocIDs {
		if id == "" {
			continue
		}
		docID, err := parseUUID(id)
		if err != nil {
			return nil, fmt.Errorf("batch find by delegation: exclude doc_id: %w", err)
		}
		excludeUUIDs = append(excludeUUIDs, docID.String())
	}

	q := `
WITH ranked AS (
  SELECT
    vd.id, vd.tenant_id, vd.agent_id, vd.team_id, vd.chat_id, vd.scope, vd.custom_scope,
    vd.path, vd.path_basename, vd.title, vd.doc_type, vd.content_hash,
    vd.summary, vd.metadata, vd.created_at, vd.updated_at,
    vd.metadata->>'delegation_id' AS deleg_id,
    ROW_NUMBER() OVER (
      PARTITION BY vd.metadata->>'delegation_id'
      ORDER BY vd.created_at DESC, vd.id DESC
    ) AS rn
  FROM vault_documents vd
  WHERE vd.tenant_id = $1
    AND vd.metadata ? 'delegation_id'
    AND vd.metadata->>'delegation_id' = ANY($2)
`
	args := []any{tid, pqStringArray(delegationIDs)}
	if len(excludeUUIDs) > 0 {
		q += fmt.Sprintf("    AND NOT (vd.id = ANY($%d::uuid[]))\n", len(args)+1)
		args = append(args, pqStringArray(excludeUUIDs))
	}
	q += `)
SELECT id, tenant_id, agent_id, team_id, chat_id, scope, custom_scope, path, path_basename,
       title, doc_type, content_hash, summary, metadata, created_at, updated_at, deleg_id
FROM ranked
WHERE rn <= $` + fmt.Sprintf("%d", len(args)+1) + `
ORDER BY deleg_id, created_at DESC
`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("batch find by delegation: %w", err)
	}
	defer rows.Close()

	out := make(map[string][]store.VaultDocument, len(delegationIDs))
	for rows.Next() {
		var (
			id, tenantIDVal      uuid.UUID
			agentID, teamID      *uuid.UUID
			chatID               *string
			customScope          *string
			metaJSON             []byte
			delegID              string
			path, pathBase       string
			scope, title, docTyp string
			contentHash, summary string
		)
		doc := store.VaultDocument{}
		if err := rows.Scan(
			&id, &tenantIDVal, &agentID, &teamID, &chatID, &scope, &customScope,
			&path, &pathBase, &title, &docTyp, &contentHash,
			&summary, &metaJSON, &doc.CreatedAt, &doc.UpdatedAt, &delegID,
		); err != nil {
			return nil, err
		}
		if chatID != nil {
			v := *chatID
			doc.ChatID = &v
		}
		doc.ID = id.String()
		doc.TenantID = tenantIDVal.String()
		if agentID != nil {
			v := agentID.String()
			doc.AgentID = &v
		}
		if teamID != nil {
			v := teamID.String()
			doc.TeamID = &v
		}
		doc.Scope = scope
		doc.CustomScope = customScope
		doc.Path = path
		doc.PathBasename = pathBase
		doc.Title = title
		doc.DocType = docTyp
		doc.ContentHash = contentHash
		doc.Summary = summary
		if len(metaJSON) > 0 {
			_ = json.Unmarshal(metaJSON, &doc.Metadata)
		}
		out[delegID] = append(out[delegID], doc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
