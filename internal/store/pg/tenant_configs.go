package pg

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// PGBuiltinToolTenantConfigStore implements store.BuiltinToolTenantConfigStore.
type PGBuiltinToolTenantConfigStore struct {
	db *sql.DB
}

func NewPGBuiltinToolTenantConfigStore(db *sql.DB) *PGBuiltinToolTenantConfigStore {
	return &PGBuiltinToolTenantConfigStore{db: db}
}

func (s *PGBuiltinToolTenantConfigStore) ListDisabled(ctx context.Context, tenantID uuid.UUID) ([]string, error) {
	type row struct {
		ToolName string `db:"tool_name"`
	}
	var rows []row
	if err := pkgSqlxDB.SelectContext(ctx, &rows,
		`SELECT tool_name FROM builtin_tool_tenant_configs WHERE tenant_id = $1 AND enabled = false`,
		tenantID,
	); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.ToolName)
	}
	return names, nil
}

func (s *PGBuiltinToolTenantConfigStore) ListAll(ctx context.Context, tenantID uuid.UUID) (map[string]bool, error) {
	type row struct {
		ToolName string `db:"tool_name"`
		Enabled  bool   `db:"enabled"`
	}
	var rows []row
	if err := pkgSqlxDB.SelectContext(ctx, &rows,
		`SELECT tool_name, enabled FROM builtin_tool_tenant_configs WHERE tenant_id = $1`,
		tenantID,
	); err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(rows))
	for _, r := range rows {
		result[r.ToolName] = r.Enabled
	}
	return result, nil
}

func (s *PGBuiltinToolTenantConfigStore) Set(ctx context.Context, tenantID uuid.UUID, toolName string, enabled bool) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO builtin_tool_tenant_configs (tool_name, tenant_id, enabled, updated_at)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (tool_name, tenant_id) DO UPDATE SET enabled = $3, updated_at = $4`,
		toolName, tenantID, enabled, time.Now(),
	)
	return err
}

func (s *PGBuiltinToolTenantConfigStore) Delete(ctx context.Context, tenantID uuid.UUID, toolName string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM builtin_tool_tenant_configs WHERE tool_name = $1 AND tenant_id = $2`,
		toolName, tenantID,
	)
	return err
}

// PGSkillTenantConfigStore implements store.SkillTenantConfigStore.
type PGSkillTenantConfigStore struct {
	db *sql.DB
}

func NewPGSkillTenantConfigStore(db *sql.DB) *PGSkillTenantConfigStore {
	return &PGSkillTenantConfigStore{db: db}
}

func (s *PGSkillTenantConfigStore) ListDisabledSkillIDs(ctx context.Context, tenantID uuid.UUID) ([]uuid.UUID, error) {
	type row struct {
		SkillID uuid.UUID `db:"skill_id"`
	}
	var rows []row
	if err := pkgSqlxDB.SelectContext(ctx, &rows,
		`SELECT skill_id FROM skill_tenant_configs WHERE tenant_id = $1 AND enabled = false`,
		tenantID,
	); err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.SkillID)
	}
	return ids, nil
}

func (s *PGSkillTenantConfigStore) ListAll(ctx context.Context, tenantID uuid.UUID) (map[uuid.UUID]bool, error) {
	type row struct {
		SkillID uuid.UUID `db:"skill_id"`
		Enabled bool      `db:"enabled"`
	}
	var rows []row
	if err := pkgSqlxDB.SelectContext(ctx, &rows,
		`SELECT skill_id, enabled FROM skill_tenant_configs WHERE tenant_id = $1`,
		tenantID,
	); err != nil {
		return nil, err
	}
	result := make(map[uuid.UUID]bool, len(rows))
	for _, r := range rows {
		result[r.SkillID] = r.Enabled
	}
	return result, nil
}

func (s *PGSkillTenantConfigStore) Set(ctx context.Context, tenantID uuid.UUID, skillID uuid.UUID, enabled bool) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO skill_tenant_configs (skill_id, tenant_id, enabled, updated_at)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (skill_id, tenant_id) DO UPDATE SET enabled = $3, updated_at = $4`,
		skillID, tenantID, enabled, time.Now(),
	)
	return err
}

func (s *PGSkillTenantConfigStore) Delete(ctx context.Context, tenantID uuid.UUID, skillID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM skill_tenant_configs WHERE skill_id = $1 AND tenant_id = $2`,
		skillID, tenantID,
	)
	return err
}
