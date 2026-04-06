package pg

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// CreateLink inserts a vault link, updating context on conflict.
func (s *PGVaultStore) CreateLink(ctx context.Context, link *store.VaultLink) error {
	id := uuid.Must(uuid.NewV7())
	now := time.Now().UTC()
	fromID := mustParseUUID(link.FromDocID)
	toID := mustParseUUID(link.ToDocID)

	var actualID uuid.UUID
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO vault_links (id, from_doc_id, to_doc_id, link_type, context, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (from_doc_id, to_doc_id, link_type) DO UPDATE SET
			context = EXCLUDED.context
		RETURNING id`,
		id, fromID, toID, link.LinkType, link.Context, now,
	).Scan(&actualID)
	if err != nil {
		return fmt.Errorf("vault create link: %w", err)
	}
	link.ID = actualID.String()
	return nil
}

// DeleteLink removes a vault link by ID, scoped by tenant via JOIN.
func (s *PGVaultStore) DeleteLink(ctx context.Context, tenantID, id string) error {
	uid := mustParseUUID(id)
	tid := mustParseUUID(tenantID)
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM vault_links vl
		USING vault_documents vd
		WHERE vl.id = $1 AND vl.from_doc_id = vd.id AND vd.tenant_id = $2`, uid, tid)
	return err
}

// GetOutLinks returns all links originating from a document, scoped by tenant.
func (s *PGVaultStore) GetOutLinks(ctx context.Context, tenantID, docID string) ([]store.VaultLink, error) {
	uid := mustParseUUID(docID)
	tid := mustParseUUID(tenantID)
	rows, err := s.db.QueryContext(ctx, `
		SELECT vl.id, vl.from_doc_id, vl.to_doc_id, vl.link_type, vl.context, vl.created_at
		FROM vault_links vl
		JOIN vault_documents vd ON vl.from_doc_id = vd.id
		WHERE vl.from_doc_id = $1 AND vd.tenant_id = $2
		ORDER BY vl.created_at`, uid, tid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanVaultLinks(rows)
}

// GetBacklinks returns all links pointing to a document, scoped by tenant.
func (s *PGVaultStore) GetBacklinks(ctx context.Context, tenantID, docID string) ([]store.VaultLink, error) {
	uid := mustParseUUID(docID)
	tid := mustParseUUID(tenantID)
	rows, err := s.db.QueryContext(ctx, `
		SELECT vl.id, vl.from_doc_id, vl.to_doc_id, vl.link_type, vl.context, vl.created_at
		FROM vault_links vl
		JOIN vault_documents vd ON vl.to_doc_id = vd.id
		WHERE vl.to_doc_id = $1 AND vd.tenant_id = $2
		ORDER BY vl.created_at`, uid, tid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanVaultLinks(rows)
}

// DeleteDocLinks removes all links from or to a document, scoped by tenant.
func (s *PGVaultStore) DeleteDocLinks(ctx context.Context, tenantID, docID string) error {
	uid := mustParseUUID(docID)
	tid := mustParseUUID(tenantID)
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM vault_links vl
		USING vault_documents vd
		WHERE (vl.from_doc_id = $1 OR vl.to_doc_id = $1)
			AND vd.id = vl.from_doc_id AND vd.tenant_id = $2`, uid, tid)
	return err
}

func scanVaultLinks(rows *sql.Rows) ([]store.VaultLink, error) {
	var links []store.VaultLink
	for rows.Next() {
		var l store.VaultLink
		var id, fromID, toID uuid.UUID
		if err := rows.Scan(&id, &fromID, &toID, &l.LinkType, &l.Context, &l.CreatedAt); err != nil {
			return nil, err
		}
		l.ID = id.String()
		l.FromDocID = fromID.String()
		l.ToDocID = toID.String()
		links = append(links, l)
	}
	return links, rows.Err()
}
