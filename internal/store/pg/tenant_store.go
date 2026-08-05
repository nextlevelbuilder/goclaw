package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

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
	var d store.TenantData
	err := pkgSqlxDB.GetContext(ctx, &d,
		`SELECT id, name, slug, status, settings, created_at, updated_at
		 FROM tenants WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *PGTenantStore) GetTenantBySlug(ctx context.Context, slug string) (*store.TenantData, error) {
	var d store.TenantData
	err := pkgSqlxDB.GetContext(ctx, &d,
		`SELECT id, name, slug, status, settings, created_at, updated_at
		 FROM tenants WHERE slug = $1`, slug)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// GetTenantByExternalOrgID does a JSONB lookup against
// settings->>'external_org_id'. Backed by idx_tenants_external_org_id
// (migration 000075). Returns (nil, nil) on no match — callers
// distinguish "not found" from "query failed" via the nil pointer.
func (s *PGTenantStore) GetTenantByExternalOrgID(ctx context.Context, externalOrgID string) (*store.TenantData, error) {
	if externalOrgID == "" {
		return nil, nil
	}
	var d store.TenantData
	err := pkgSqlxDB.GetContext(ctx, &d,
		`SELECT id, name, slug, status, settings, created_at, updated_at
		 FROM tenants
		 WHERE settings->>'external_org_id' = $1
		 LIMIT 1`, externalOrgID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *PGTenantStore) ListTenants(ctx context.Context) ([]store.TenantData, error) {
	var tenants []store.TenantData
	err := pkgSqlxDB.SelectContext(ctx, &tenants,
		`SELECT id, name, slug, status, settings, created_at, updated_at
		 FROM tenants ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	return tenants, nil
}

func (s *PGTenantStore) GetTenantsByIDs(ctx context.Context, ids []uuid.UUID) ([]store.TenantData, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	// Chunk by 500 to stay well within PG param limits.
	const chunkSize = 500
	var all []store.TenantData
	for start := 0; start < len(ids); start += chunkSize {
		end := min(start+chunkSize, len(ids))
		var chunk []store.TenantData
		if err := pkgSqlxDB.SelectContext(ctx, &chunk,
			`SELECT id, name, slug, status, settings, created_at, updated_at
			 FROM tenants WHERE id = ANY($1)`,
			pq.Array(ids[start:end])); err != nil {
			return nil, err
		}
		all = append(all, chunk...)
	}
	return all, nil
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
	var d store.TenantUserData
	err := pkgSqlxDB.GetContext(ctx, &d,
		`SELECT id, tenant_id, user_id, display_name, role, metadata, created_at, updated_at
		 FROM tenant_users WHERE id = $1`, id)
	if err != nil {
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

// RemoveUser removes a member from a tenant AND hands their agents to the
// workspace, in one transaction.
//
// The two halves belong together. Personal agents are invisible to everyone but
// their owner, so removing a member without this leaves rows that NOBODY can see
// and that keep firing on whatever schedule they were armed with — the worst of
// both worlds. Flipping them to 'org' means the workspace inherits what it was
// already paying for, and can review, re-own or delete it.
//
// Enforced HERE rather than in the two callers (HTTP + WS) because it is an
// invariant, not a policy: there is no correct way to remove a member and skip it,
// and a third caller added later would otherwise silently reintroduce the orphan.
func (s *PGTenantStore) RemoveUser(ctx context.Context, tenantID uuid.UUID, userID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`UPDATE agents SET visibility = 'org', updated_at = NOW()
		 WHERE tenant_id = $1 AND owner_id = $2
		   AND visibility <> 'org' AND deleted_at IS NULL`,
		tenantID, userID,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM tenant_users WHERE tenant_id = $1 AND user_id = $2`,
		tenantID, userID,
	); err != nil {
		return err
	}
	return tx.Commit()
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

// IsOwnerOrAdmin reports whether the user is allowed to perform team-wide
// writes (e.g. publish a public skill) in the given tenant. Personal tenants
// — those with exactly one membership row — are treated as implicitly admin
// for their single member so single-user installs never get blocked.
// Otherwise the user must have role 'owner' or 'admin'.
func (s *PGTenantStore) IsOwnerOrAdmin(ctx context.Context, tenantID uuid.UUID, userID string) (bool, error) {
	var memberCount int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tenant_users WHERE tenant_id = $1`,
		tenantID,
	).Scan(&memberCount); err != nil {
		return false, err
	}
	if memberCount <= 1 {
		// Personal tenant: the single member is implicitly the owner.
		// (0 covers legacy data where the personal owner has no row at all.)
		return true, nil
	}
	role, err := s.GetUserRole(ctx, tenantID, userID)
	if err != nil {
		return false, err
	}
	return role == store.TenantRoleOwner || role == store.TenantRoleAdmin, nil
}

func (s *PGTenantStore) ListUsers(ctx context.Context, tenantID uuid.UUID) ([]store.TenantUserData, error) {
	var result []store.TenantUserData
	err := pkgSqlxDB.SelectContext(ctx, &result,
		`SELECT id, tenant_id, user_id, display_name, role, metadata, created_at, updated_at
		 FROM tenant_users WHERE tenant_id = $1 ORDER BY created_at`, tenantID)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *PGTenantStore) ListUserTenants(ctx context.Context, userID string) ([]store.TenantUserData, error) {
	var result []store.TenantUserData
	err := pkgSqlxDB.SelectContext(ctx, &result,
		`SELECT id, tenant_id, user_id, display_name, role, metadata, created_at, updated_at
		 FROM tenant_users WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	return result, nil
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
