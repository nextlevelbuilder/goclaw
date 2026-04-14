//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// CreateLinks batch-inserts vault links with tenant validation.
func (s *SQLiteVaultStore) CreateLinks(ctx context.Context, links []store.VaultLink) error {
	if len(links) == 0 {
		return nil
	}

	// Collect all unique doc IDs for tenant validation.
	docIDSet := make(map[string]struct{})
	for _, l := range links {
		docIDSet[l.FromDocID] = struct{}{}
		docIDSet[l.ToDocID] = struct{}{}
	}
	docIDs := make([]string, 0, len(docIDSet))
	for id := range docIDSet {
		docIDs = append(docIDs, id)
	}

	// Batch-fetch tenant_id for all referenced docs.
	type docMeta struct {
		tenantID string
		teamID   *string
	}
	docMap := make(map[string]docMeta, len(docIDs))
	ph := make([]string, len(docIDs))
	args := make([]any, len(docIDs))
	for i, id := range docIDs {
		ph[i] = "?"
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, team_id FROM vault_documents WHERE id IN (`+strings.Join(ph, ",")+`)`, args...)
	if err != nil {
		return fmt.Errorf("vault batch link: fetch docs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, tid string
		var teamID *string
		if err := rows.Scan(&id, &tid, &teamID); err != nil {
			return fmt.Errorf("vault batch link: scan doc: %w", err)
		}
		docMap[id] = docMeta{tenantID: tid, teamID: teamID}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("vault batch link: rows err: %w", err)
	}

	// Validate and insert (chunk by 500).
	const chunkSize = 500
	var valid []store.VaultLink
	for _, l := range links {
		from, fromOK := docMap[l.FromDocID]
		to, toOK := docMap[l.ToDocID]
		if !fromOK || !toOK {
			continue
		}
		if from.tenantID != to.tenantID {
			continue
		}
		if from.teamID != nil && to.teamID != nil && *from.teamID != *to.teamID {
			continue
		}
		valid = append(valid, l)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	for start := 0; start < len(valid); start += chunkSize {
		end := min(start+chunkSize, len(valid))
		chunk := valid[start:end]
		// id, from_doc_id, to_doc_id, link_type, context, metadata, created_at
		const cols = 7
		iArgs := make([]any, 0, len(chunk)*cols)
		iPh := make([]string, 0, len(chunk))
		for _, l := range chunk {
			metaJSON, mErr := json.Marshal(l.Metadata)
			if mErr != nil || len(metaJSON) == 0 {
				metaJSON = []byte("{}")
			}
			iPh = append(iPh, "(?,?,?,?,?,?,?)")
			iArgs = append(iArgs,
				uuid.Must(uuid.NewV7()).String(),
				l.FromDocID, l.ToDocID,
				l.LinkType, l.Context, string(metaJSON), now)
		}
		q := `INSERT INTO vault_links (id, from_doc_id, to_doc_id, link_type, context, metadata, created_at)
			VALUES ` + strings.Join(iPh, ",") + `
			ON CONFLICT (from_doc_id, to_doc_id, link_type) DO UPDATE SET
				context = excluded.context,
				metadata = excluded.metadata`
		if _, err := s.db.ExecContext(ctx, q, iArgs...); err != nil {
			return fmt.Errorf("vault batch create links: %w", err)
		}
	}
	return nil
}

// CreateLink inserts a vault link, updating context+metadata on conflict.
// Validates same-tenant + same-team boundary before insert.
func (s *SQLiteVaultStore) CreateLink(ctx context.Context, link *store.VaultLink) error {
	// Verify both docs exist and belong to same tenant + team boundary.
	var fromTenant, toTenant string
	var fromTeamID, toTeamID *string
	err := s.db.QueryRowContext(ctx,
		`SELECT tenant_id, team_id FROM vault_documents WHERE id = ?`, link.FromDocID,
	).Scan(&fromTenant, &fromTeamID)
	if err != nil {
		return fmt.Errorf("vault link: source doc not found: %w", err)
	}
	err = s.db.QueryRowContext(ctx,
		`SELECT tenant_id, team_id FROM vault_documents WHERE id = ?`, link.ToDocID,
	).Scan(&toTenant, &toTeamID)
	if err != nil {
		return fmt.Errorf("vault link: target doc not found: %w", err)
	}
	if fromTenant != toTenant {
		return fmt.Errorf("vault link: documents belong to different tenants")
	}
	if fromTeamID != nil && toTeamID != nil && *fromTeamID != *toTeamID {
		return fmt.Errorf("vault link: documents belong to different teams")
	}

	metaJSON, mErr := json.Marshal(link.Metadata)
	if mErr != nil || len(metaJSON) == 0 {
		metaJSON = []byte("{}")
	}
	id := uuid.Must(uuid.NewV7()).String()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO vault_links (id, from_doc_id, to_doc_id, link_type, context, metadata, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (from_doc_id, to_doc_id, link_type) DO UPDATE SET
			context = excluded.context,
			metadata = excluded.metadata
		RETURNING id`,
		id, link.FromDocID, link.ToDocID, link.LinkType, link.Context, string(metaJSON), now,
	).Scan(&link.ID)
	if err != nil {
		return fmt.Errorf("vault create link: %w", err)
	}
	return nil
}

// DeleteLink removes a vault link by ID, scoped by tenant via JOIN to vault_documents (F9).
func (s *SQLiteVaultStore) DeleteLink(ctx context.Context, tenantID, id string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM vault_links WHERE id = ?
		AND from_doc_id IN (SELECT id FROM vault_documents WHERE tenant_id = ?)`,
		id, tenantID)
	return err
}

// GetOutLinks returns all links originating from a document, scoped by tenant (F9).
func (s *SQLiteVaultStore) GetOutLinks(ctx context.Context, tenantID, docID string) ([]store.VaultLink, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT vl.id, vl.from_doc_id, vl.to_doc_id, vl.link_type, vl.context, vl.metadata, vl.created_at
		FROM vault_links vl
		JOIN vault_documents d ON d.id = vl.from_doc_id
		WHERE vl.from_doc_id = ? AND d.tenant_id = ?
		ORDER BY vl.created_at`, docID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanVaultLinkRows(rows)
}

// GetOutLinksBatch returns all outlinks for multiple doc IDs in a single query.
func (s *SQLiteVaultStore) GetOutLinksBatch(ctx context.Context, tenantID string, docIDs []string) ([]store.VaultLink, error) {
	if len(docIDs) == 0 {
		return nil, nil
	}
	ph := strings.Repeat("?,", len(docIDs)-1) + "?"
	args := make([]any, 0, len(docIDs)+1)
	for _, id := range docIDs {
		args = append(args, id)
	}
	args = append(args, tenantID)
	rows, err := s.db.QueryContext(ctx,
		`SELECT vl.id, vl.from_doc_id, vl.to_doc_id, vl.link_type, vl.context, vl.metadata, vl.created_at
		FROM vault_links vl
		JOIN vault_documents d ON d.id = vl.from_doc_id
		WHERE vl.from_doc_id IN (`+ph+`) AND d.tenant_id = ?
		ORDER BY vl.created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanVaultLinkRows(rows)
}

// GetBacklinks returns enriched backlinks pointing to a document (single JOIN, LIMIT 100).
func (s *SQLiteVaultStore) GetBacklinks(ctx context.Context, tenantID, docID string) ([]store.VaultBacklink, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT vl.from_doc_id, vl.context, vd.title, vd.path, vd.team_id
		FROM vault_links vl
		JOIN vault_documents vd ON vd.id = vl.from_doc_id
		WHERE vl.to_doc_id = ? AND vd.tenant_id = ?
		ORDER BY vd.updated_at DESC
		LIMIT 100`, docID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var backlinks []store.VaultBacklink
	for rows.Next() {
		var bl store.VaultBacklink
		if err := rows.Scan(&bl.FromDocID, &bl.Context, &bl.Title, &bl.Path, &bl.TeamID); err != nil {
			return nil, err
		}
		backlinks = append(backlinks, bl)
	}
	return backlinks, rows.Err()
}

// DeleteDocLinks removes all links from or to a document, scoped by tenant (F9).
func (s *SQLiteVaultStore) DeleteDocLinks(ctx context.Context, tenantID, docID string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM vault_links
		WHERE (from_doc_id = ? OR to_doc_id = ?)
		  AND from_doc_id IN (SELECT id FROM vault_documents WHERE tenant_id = ?)`,
		docID, docID, tenantID)
	return err
}

// DeleteDocLinksByType removes outbound links of a specific type from a document, scoped by tenant.
func (s *SQLiteVaultStore) DeleteDocLinksByType(ctx context.Context, tenantID, docID, linkType string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM vault_links
		WHERE from_doc_id = ?
		  AND link_type = ?
		  AND from_doc_id IN (SELECT id FROM vault_documents WHERE tenant_id = ?)`,
		docID, linkType, tenantID)
	return err
}

// DeleteDocLinksByTypes removes outbound links matching any of the given types from a document.
func (s *SQLiteVaultStore) DeleteDocLinksByTypes(ctx context.Context, tenantID, docID string, types []string) error {
	if len(types) == 0 {
		return nil
	}
	// Build IN clause with ? placeholders.
	params := []any{docID}
	placeholders := make([]string, len(types))
	for i, t := range types {
		params = append(params, t)
		placeholders[i] = "?"
	}
	params = append(params, tenantID)
	q := fmt.Sprintf(`
		DELETE FROM vault_links
		WHERE from_doc_id = ?
		  AND link_type IN (%s)
		  AND from_doc_id IN (SELECT id FROM vault_documents WHERE tenant_id = ?)`,
		strings.Join(placeholders, ","))
	_, err := s.db.ExecContext(ctx, q, params...)
	return err
}

func scanVaultLinkRows(rows *sql.Rows) ([]store.VaultLink, error) {
	var links []store.VaultLink
	for rows.Next() {
		var l store.VaultLink
		var metaJSON []byte
		ca := &sqliteTime{}
		if err := rows.Scan(&l.ID, &l.FromDocID, &l.ToDocID, &l.LinkType, &l.Context, &metaJSON, ca); err != nil {
			return nil, err
		}
		l.CreatedAt = ca.Time
		if len(metaJSON) > 2 {
			_ = json.Unmarshal(metaJSON, &l.Metadata)
		}
		links = append(links, l)
	}
	return links, rows.Err()
}
