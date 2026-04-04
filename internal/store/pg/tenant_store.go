package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGTenantStore implements store.TenantStore backed by Postgres.
type PGTenantStore struct {
	db *sql.DB
}

// NewPGTenantStore creates a new PostgreSQL-backed tenant store.
func NewPGTenantStore(db *sql.DB) *PGTenantStore {
	return &PGTenantStore{db: db}
}

// ============================================================
// Tenant CRUD
// ============================================================

func (s *PGTenantStore) CreateTenant(ctx context.Context, tenant *store.TenantData) error {
	if tenant.ID == uuid.Nil {
		tenant.ID = store.GenNewID()
	}
	now := time.Now()
	tenant.CreatedAt = now
	tenant.UpdatedAt = now

	settings := tenant.Settings
	if len(settings) == 0 {
		settings = json.RawMessage(`{}`)
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tenants (id, name, slug, status, settings, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		tenant.ID, tenant.Name, tenant.Slug, tenant.Status, settings, now, now,
	)
	return err
}

func (s *PGTenantStore) GetTenant(ctx context.Context, id uuid.UUID) (*store.TenantData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, slug, status, settings, created_at, updated_at
		 FROM tenants WHERE id = $1`, id)
	return scanTenantRow(row)
}

func (s *PGTenantStore) GetTenantBySlug(ctx context.Context, slug string) (*store.TenantData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, slug, status, settings, created_at, updated_at
		 FROM tenants WHERE slug = $1`, slug)
	return scanTenantRow(row)
}

func (s *PGTenantStore) ListTenants(ctx context.Context) ([]store.TenantData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, slug, status, settings, created_at, updated_at
		 FROM tenants ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tenants []store.TenantData
	for rows.Next() {
		d, err := scanTenantRows(rows)
		if err != nil {
			return nil, err
		}
		tenants = append(tenants, *d)
	}
	return tenants, rows.Err()
}

func (s *PGTenantStore) UpdateTenant(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	return execMapUpdate(ctx, s.db, "tenants", id, updates)
}

// ============================================================
// Tenant-user membership
// ============================================================

func (s *PGTenantStore) AddUser(ctx context.Context, tenantID uuid.UUID, userID, role string) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tenant_users (id, tenant_id, user_id, role, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (tenant_id, user_id) DO UPDATE SET role = EXCLUDED.role, updated_at = EXCLUDED.updated_at`,
		store.GenNewID(), tenantID, userID, role, now, now,
	)
	return err
}

func (s *PGTenantStore) GetTenantUser(ctx context.Context, id uuid.UUID) (*store.TenantUserData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, user_id, display_name, role, metadata, created_at, updated_at
		 FROM tenant_users WHERE id = $1`, id)
	var d store.TenantUserData
	if err := row.Scan(&d.ID, &d.TenantID, &d.UserID, &d.DisplayName, &d.Role, &d.Metadata, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *PGTenantStore) CreateTenantUserReturning(ctx context.Context, tenantID uuid.UUID, userID, displayName, role string) (*store.TenantUserData, error) {
	now := time.Now()
	var dn *string
	if displayName != "" {
		dn = &displayName
	}
	row := s.db.QueryRowContext(ctx,
		`INSERT INTO tenant_users (id, tenant_id, user_id, display_name, role, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (tenant_id, user_id) DO UPDATE SET
		   display_name = COALESCE(EXCLUDED.display_name, tenant_users.display_name),
		   updated_at = EXCLUDED.updated_at
		 RETURNING id, tenant_id, user_id, display_name, role, metadata, created_at, updated_at`,
		store.GenNewID(), tenantID, userID, dn, role, now, now,
	)
	var d store.TenantUserData
	if err := row.Scan(&d.ID, &d.TenantID, &d.UserID, &d.DisplayName, &d.Role, &d.Metadata, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *PGTenantStore) RemoveUser(ctx context.Context, tenantID uuid.UUID, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM tenant_users WHERE tenant_id = $1 AND user_id = $2`,
		tenantID, userID,
	)
	return err
}

func (s *PGTenantStore) GetUserRole(ctx context.Context, tenantID uuid.UUID, userID string) (string, error) {
	var role string
	err := s.db.QueryRowContext(ctx,
		`SELECT role FROM tenant_users WHERE tenant_id = $1 AND user_id = $2`,
		tenantID, userID,
	).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return role, err
}

func (s *PGTenantStore) ListUsers(ctx context.Context, tenantID uuid.UUID) ([]store.TenantUserData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, user_id, display_name, role, metadata, created_at, updated_at
		 FROM tenant_users WHERE tenant_id = $1 ORDER BY created_at`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTenantUserRows(rows)
}

func (s *PGTenantStore) ListUserTenants(ctx context.Context, userID string) ([]store.TenantUserData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, user_id, display_name, role, metadata, created_at, updated_at
		 FROM tenant_users WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTenantUserRows(rows)
}

func (s *PGTenantStore) ResolveUserTenant(ctx context.Context, userID string) (uuid.UUID, error) {
	var tenantID uuid.UUID
	err := s.db.QueryRowContext(ctx,
		`SELECT tenant_id FROM tenant_users WHERE user_id = $1 ORDER BY created_at LIMIT 1`,
		userID,
	).Scan(&tenantID)
	if errors.Is(err, sql.ErrNoRows) {
		return store.MasterTenantID, nil
	}
	if err != nil {
		return uuid.Nil, err
	}
	return tenantID, nil
}

// ============================================================
// Scan helpers
// ============================================================

func scanTenantRow(row *sql.Row) (*store.TenantData, error) {
	var d store.TenantData
	err := row.Scan(&d.ID, &d.Name, &d.Slug, &d.Status, &d.Settings, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

type tenantRowScanner interface {
	Scan(dest ...any) error
}

func scanTenantRows(row tenantRowScanner) (*store.TenantData, error) {
	var d store.TenantData
	err := row.Scan(&d.ID, &d.Name, &d.Slug, &d.Status, &d.Settings, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func scanTenantUserRows(rows *sql.Rows) ([]store.TenantUserData, error) {
	var result []store.TenantUserData
	for rows.Next() {
		var d store.TenantUserData
		if err := rows.Scan(&d.ID, &d.TenantID, &d.UserID, &d.DisplayName, &d.Role, &d.Metadata, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, rows.Err()
}
