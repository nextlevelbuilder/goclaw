package pg

import (
	"context"
	"fmt"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// UpdateSummaryAndReembed updates the document summary and re-embeds the combined text.
func (s *PGVaultStore) UpdateSummaryAndReembed(ctx context.Context, tenantID, docID, summary string) error {
	tid := mustParseUUID(tenantID)
	did := mustParseUUID(docID)

	// Fetch title+path to build embed text.
	var title, path string
	err := s.db.QueryRowContext(ctx,
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
func (s *PGVaultStore) FindSimilarDocs(ctx context.Context, tenantID, agentID, docID string, limit int) ([]store.VaultSearchResult, error) {
	tid := mustParseUUID(tenantID)
	aid := mustParseUUID(agentID)
	did := mustParseUUID(docID)

	// Fetch source embedding.
	var embStr *string
	err := s.db.QueryRowContext(ctx,
		`SELECT embedding::text FROM vault_documents WHERE id = $1 AND tenant_id = $2`,
		did, tid,
	).Scan(&embStr)
	if err != nil || embStr == nil {
		return nil, nil // no embedding = no neighbors
	}

	q := `SELECT id, tenant_id, agent_id, team_id, scope, custom_scope, path, title, doc_type,
			content_hash, summary, metadata, created_at, updated_at,
			1 - (embedding <=> $1::vector) AS score
		FROM vault_documents
		WHERE tenant_id = $2 AND agent_id = $3 AND id != $4 AND embedding IS NOT NULL
		ORDER BY embedding <=> $1::vector
		LIMIT $5`

	var scanned []vaultSearchRow
	if err := pkgSqlxDB.SelectContext(ctx, &scanned, q, *embStr, tid, aid, did, limit); err != nil {
		return nil, fmt.Errorf("vault.find_similar: %w", err)
	}
	return vaultSearchRowsToResults(scanned, "vault"), nil
}
