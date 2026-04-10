//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteTenantStore implements store.TenantStore backed by SQLite.
type SQLiteTenantStore struct {
	db *sql.DB
}

func NewSQLiteTenantStore(db *sql.DB) *SQLiteTenantStore {
	return &SQLiteTenantStore{db: db}
}

// ============================================================
// Tenant CRUD
// ============================================================

func (s *SQLiteTenantStore) CreateTenant(ctx context.Context, tenant *store.TenantData) error {
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
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		tenant.ID, tenant.Name, tenant.Slug, tenant.Status, settings, now, now,
	)
	return err
}

const tenantSelectCols = `id, name, slug, status, settings, created_at, updated_at`

func (s *SQLiteTenantStore) GetTenant(ctx context.Context, id uuid.UUID) (*store.TenantData, error) {
	var row tenantRow
	err := pkgSqlxDB.GetContext(ctx, &row,
		`SELECT `+tenantSelectCols+` FROM tenants WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	d := row.toTenantData()
	return &d, nil
}

func (s *SQLiteTenantStore) GetTenantBySlug(ctx context.Context, slug string) (*store.TenantData, error) {
	var row tenantRow
	err := pkgSqlxDB.GetContext(ctx, &row,
		`SELECT `+tenantSelectCols+` FROM tenants WHERE slug = ?`, slug)
	if err != nil {
		return nil, err
	}
	d := row.toTenantData()
	return &d, nil
}

func (s *SQLiteTenantStore) ListTenants(ctx context.Context) ([]store.TenantData, error) {
	var rows []tenantRow
	err := pkgSqlxDB.SelectContext(ctx, &rows,
		`SELECT `+tenantSelectCols+` FROM tenants ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	result := make([]store.TenantData, 0, len(rows))
	for _, r := range rows {
		result = append(result, r.toTenantData())
	}
	return result, nil
}

func (s *SQLiteTenantStore) GetTenantsByIDs(ctx context.Context, ids []uuid.UUID) ([]store.TenantData, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	const chunkSize = 500
	var all []store.TenantData
	for start := 0; start < len(ids); start += chunkSize {
		end := min(start+chunkSize, len(ids))
		chunk := ids[start:end]
		ph := make([]string, len(chunk))
		args := make([]any, len(chunk))
		for i, id := range chunk {
			ph[i] = "?"
			args[i] = id.String()
		}
		q := `SELECT ` + tenantSelectCols + ` FROM tenants WHERE id IN (` + strings.Join(ph, ",") + `)`
		var rows []tenantRow
		if err := pkgSqlxDB.SelectContext(ctx, &rows, q, args...); err != nil {
			return nil, err
		}
		for _, r := range rows {
			all = append(all, r.toTenantData())
		}
	}
	return all, nil
}

func (s *SQLiteTenantStore) UpdateTenant(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	return execMapUpdate(ctx, s.db, "tenants", id, updates)
}

// ============================================================
// Tenant-user membership
// ============================================================

func (s *SQLiteTenantStore) AddUser(ctx context.Context, tenantID uuid.UUID, userID, role string) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tenant_users (id, tenant_id, user_id, role, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT (tenant_id, user_id) DO UPDATE SET role = excluded.role, updated_at = excluded.updated_at`,
		store.GenNewID(), tenantID, userID, role, now, now,
	)
	return err
}

const tenantUserSelectCols = `id, tenant_id, user_id, display_name, role, metadata, created_at, updated_at`

func (s *SQLiteTenantStore) GetTenantUser(ctx context.Context, id uuid.UUID) (*store.TenantUserData, error) {
	var row tenantUserRow
	err := pkgSqlxDB.GetContext(ctx, &row,
		`SELECT `+tenantUserSelectCols+` FROM tenant_users WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	d := row.toTenantUserData()
	return &d, nil
}

func (s *SQLiteTenantStore) CreateTenantUserReturning(ctx context.Context, tenantID uuid.UUID, userID, displayName, role string) (*store.TenantUserData, error) {
	now := time.Now()
	var dn *string
	if displayName != "" {
		dn = &displayName
	}
	// SQLite 3.35+ supports RETURNING.
	row := s.db.QueryRowContext(ctx,
		`INSERT INTO tenant_users (id, tenant_id, user_id, display_name, role, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (tenant_id, user_id) DO UPDATE SET
		   display_name = COALESCE(excluded.display_name, tenant_users.display_name),
		   updated_at = excluded.updated_at
		 RETURNING id, tenant_id, user_id, display_name, role, metadata, created_at, updated_at`,
		store.GenNewID(), tenantID, userID, dn, role, now, now,
	)
	var d store.TenantUserData
	createdAt, updatedAt := scanTimePair()
	if err := row.Scan(&d.ID, &d.TenantID, &d.UserID, &d.DisplayName, &d.Role, &d.Metadata, createdAt, updatedAt); err != nil {
		return nil, err
	}
	d.CreatedAt = createdAt.Time
	d.UpdatedAt = updatedAt.Time
	return &d, nil
}

func (s *SQLiteTenantStore) RemoveUser(ctx context.Context, tenantID uuid.UUID, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM tenant_users WHERE tenant_id = ? AND user_id = ?`,
		tenantID, userID,
	)
	return err
}

func (s *SQLiteTenantStore) GetUserRole(ctx context.Context, tenantID uuid.UUID, userID string) (string, error) {
	var role string
	err := s.db.QueryRowContext(ctx,
		`SELECT role FROM tenant_users WHERE tenant_id = ? AND user_id = ?`,
		tenantID, userID,
	).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return role, err
}

func (s *SQLiteTenantStore) ListUsers(ctx context.Context, tenantID uuid.UUID) ([]store.TenantUserData, error) {
	var rows []tenantUserRow
	err := pkgSqlxDB.SelectContext(ctx, &rows,
		`SELECT `+tenantUserSelectCols+` FROM tenant_users WHERE tenant_id = ? ORDER BY created_at`, tenantID)
	if err != nil {
		return nil, err
	}
	return convertTenantUserRows(rows), nil
}

func (s *SQLiteTenantStore) ListUserTenants(ctx context.Context, userID string) ([]store.TenantUserData, error) {
	var rows []tenantUserRow
	err := pkgSqlxDB.SelectContext(ctx, &rows,
		`SELECT `+tenantUserSelectCols+` FROM tenant_users WHERE user_id = ? ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	return convertTenantUserRows(rows), nil
}

func (s *SQLiteTenantStore) ResolveUserTenant(ctx context.Context, userID string) (uuid.UUID, error) {
	var tenantID uuid.UUID
	err := s.db.QueryRowContext(ctx,
		`SELECT tenant_id FROM tenant_users WHERE user_id = ? ORDER BY created_at LIMIT 1`,
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
// Conversion helpers
// ============================================================

func convertTenantUserRows(rows []tenantUserRow) []store.TenantUserData {
	result := make([]store.TenantUserData, 0, len(rows))
	for _, r := range rows {
		result = append(result, r.toTenantUserData())
	}
	return result
}
