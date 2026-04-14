package pg

import (
	"context"
	"fmt"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ListUnenrichedDocs returns documents with empty summary for re-enrichment.
// limit=0 means no limit.
func (s *PGVaultStore) ListUnenrichedDocs(ctx context.Context, tenantID string, limit int) ([]store.VaultDocument, error) {
	tid, err := parseUUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("vault list unenriched: tenant: %w", err)
	}

	q := `SELECT id, tenant_id, agent_id, team_id, scope, custom_scope, path, path_basename, title, doc_type,
			content_hash, summary, metadata, created_at, updated_at
		FROM vault_documents
		WHERE tenant_id = $1 AND (summary IS NULL OR summary = '')
		ORDER BY created_at ASC`
	args := []any{tid}

	if limit > 0 {
		q += " LIMIT $2"
		args = append(args, limit)
	}

	var rows []vaultDocRow
	if err := pkgSqlxDB.SelectContext(ctx, &rows, q, args...); err != nil {
		return nil, fmt.Errorf("vault.list_unenriched: %w", err)
	}
	return vaultDocRowsToDocs(rows), nil
}

// UpdateSummaryAndReembed updates the document summary and re-embeds the combined text.
func (s *PGVaultStore) UpdateSummaryAndReembed(ctx context.Context, tenantID, docID, summary string) error {
	tid, err := parseUUID(tenantID)
	if err != nil {
		return fmt.Errorf("vault update summary: tenant: %w", err)
	}
	did, err := parseUUID(docID)
	if err != nil {
		return fmt.Errorf("vault update summary: doc: %w", err)
	}

	// Fetch title+path to build embed text.
	var title, path string
	err = s.db.QueryRowContext(ctx,
		`SELECT title, path FROM vault_documents WHERE id = $1 AND tenant_id = $2`,
		did, tid,
	).Scan(&title, &path)
	if err != nil {
		return fmt.Errorf("vault.update_summary: fetch doc: %w", err)
	}

	var embStr *string
	if s.embProvider != nil {
		embedText := title + " " + path + " " + summary
		vecs, embErr := s.embProvider.Embed(ctx, []string{embedText})
		if embErr == nil && len(vecs) > 0 {
			v := vectorToString(vecs[0])
			embStr = &v
		}
	}

	_, err = s.db.ExecContext(ctx, `
		UPDATE vault_documents
		SET summary = $1, embedding = COALESCE($2, embedding), updated_at = $3
		WHERE id = $4 AND tenant_id = $5`,
		summary, embStr, time.Now().UTC(), did, tid,
	)
	return err
}

// FindSimilarDocs finds documents with similar embeddings to the given docID.
// Returns top-N neighbors excluding the source doc itself.
// Empty agentID means no agent filter.
func (s *PGVaultStore) FindSimilarDocs(ctx context.Context, tenantID, agentID, docID string, limit int) ([]store.VaultSearchResult, error) {
	tid, err := parseUUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("find similar: tenant: %w", err)
	}
	aid, err := optAgentUUID(&agentID)
	if err != nil {
		return nil, fmt.Errorf("find similar: agent: %w", err)
	}
	did, err := parseUUID(docID)
	if err != nil {
		return nil, fmt.Errorf("find similar: doc: %w", err)
	}

	// Fetch source embedding.
	var embStr *string
	err = s.db.QueryRowContext(ctx,
		`SELECT embedding::text FROM vault_documents WHERE id = $1 AND tenant_id = $2`,
		did, tid,
	).Scan(&embStr)
	if err != nil || embStr == nil {
		return nil, nil // no embedding = no neighbors
	}

	q := `SELECT id, tenant_id, agent_id, team_id, scope, custom_scope, path, path_basename, title, doc_type,
			content_hash, summary, metadata, created_at, updated_at,
			1 - (embedding <=> $1::vector) AS score
		FROM vault_documents
		WHERE tenant_id = $2 AND id != $3 AND embedding IS NOT NULL`
	args := []any{*embStr, tid, did}
	p := 4

	if aid != nil {
		q += fmt.Sprintf(" AND agent_id = $%d", p)
		args = append(args, *aid)
		p++
	}
	q += fmt.Sprintf(" ORDER BY embedding <=> $1::vector LIMIT $%d", p)
	args = append(args, limit)

	var scanned []vaultSearchRow
	if err := pkgSqlxDB.SelectContext(ctx, &scanned, q, args...); err != nil {
		return nil, fmt.Errorf("vault.find_similar: %w", err)
	}
	return vaultSearchRowsToResults(scanned, "vault"), nil
}
