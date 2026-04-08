//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// CreateLink inserts a vault link, updating context on conflict.
func (s *SQLiteVaultStore) CreateLink(ctx context.Context, link *store.VaultLink) error {
	id := uuid.Must(uuid.NewV7()).String()
	now := time.Now().UTC().Format(time.RFC3339Nano)

	err := s.db.QueryRowContext(ctx, `
		INSERT INTO vault_links (id, from_doc_id, to_doc_id, link_type, context, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (from_doc_id, to_doc_id, link_type) DO UPDATE SET
			context = excluded.context
		RETURNING id`,
		id, link.FromDocID, link.ToDocID, link.LinkType, link.Context, now,
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
		SELECT vl.id, vl.from_doc_id, vl.to_doc_id, vl.link_type, vl.context, vl.created_at
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

// GetBacklinks returns all links pointing to a document, scoped by tenant (F9).
func (s *SQLiteVaultStore) GetBacklinks(ctx context.Context, tenantID, docID string) ([]store.VaultLink, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT vl.id, vl.from_doc_id, vl.to_doc_id, vl.link_type, vl.context, vl.created_at
		FROM vault_links vl
		JOIN vault_documents d ON d.id = vl.to_doc_id
		WHERE vl.to_doc_id = ? AND d.tenant_id = ?
		ORDER BY vl.created_at`, docID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanVaultLinkRows(rows)
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

func scanVaultLinkRows(rows *sql.Rows) ([]store.VaultLink, error) {
	var links []store.VaultLink
	for rows.Next() {
		var l store.VaultLink
		ca := &sqliteTime{}
		if err := rows.Scan(&l.ID, &l.FromDocID, &l.ToDocID, &l.LinkType, &l.Context, ca); err != nil {
			return nil, err
		}
		l.CreatedAt = ca.Time
		links = append(links, l)
	}
	return links, rows.Err()
}
