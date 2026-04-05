package pg

import (
	"context"
	"database/sql"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type pgBrowserExtensionStore struct {
	db *sql.DB
}

func NewBrowserExtensionStore(db *sql.DB) *pgBrowserExtensionStore {
	return &pgBrowserExtensionStore{db: db}
}

func (s *pgBrowserExtensionStore) List(ctx context.Context, tenantID string) ([]*store.BrowserExtension, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, name, path, enabled, created_at
		 FROM browser_extensions WHERE tenant_id = $1
		 ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanExtensions(rows)
}

func (s *pgBrowserExtensionStore) Create(ctx context.Context, e *store.BrowserExtension) error {
	if e.TenantID == "" {
		e.TenantID = tenantIDForInsert(ctx).String()
	}
	return s.db.QueryRowContext(ctx,
		`INSERT INTO browser_extensions (tenant_id, name, path, enabled)
		 VALUES ($1,$2,$3,$4)
		 RETURNING id, created_at`,
		e.TenantID, e.Name, e.Path, e.Enabled,
	).Scan(&e.ID, &e.CreatedAt)
}

func (s *pgBrowserExtensionStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM browser_extensions WHERE id = $1`, id)
	return err
}

func (s *pgBrowserExtensionStore) ListEnabled(ctx context.Context, tenantID string) ([]*store.BrowserExtension, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, name, path, enabled, created_at
		 FROM browser_extensions WHERE tenant_id = $1 AND enabled = true
		 ORDER BY created_at`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanExtensions(rows)
}

func scanExtensions(rows *sql.Rows) ([]*store.BrowserExtension, error) {
	var exts []*store.BrowserExtension
	for rows.Next() {
		var e store.BrowserExtension
		if err := rows.Scan(&e.ID, &e.TenantID, &e.Name, &e.Path, &e.Enabled, &e.CreatedAt); err != nil {
			return nil, err
		}
		exts = append(exts, &e)
	}
	return exts, rows.Err()
}
