//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// SQLiteBuiltinToolTenantConfigStore implements store.BuiltinToolTenantConfigStore.
type SQLiteBuiltinToolTenantConfigStore struct {
	db *sql.DB
}

func NewSQLiteBuiltinToolTenantConfigStore(db *sql.DB) *SQLiteBuiltinToolTenantConfigStore {
	return &SQLiteBuiltinToolTenantConfigStore{db: db}
}

func (s *SQLiteBuiltinToolTenantConfigStore) ListDisabled(ctx context.Context, tenantID uuid.UUID) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT tool_name FROM builtin_tool_tenant_configs WHERE tenant_id = ? AND enabled = 0`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func (s *SQLiteBuiltinToolTenantConfigStore) ListAll(ctx context.Context, tenantID uuid.UUID) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT tool_name, enabled FROM builtin_tool_tenant_configs WHERE tenant_id = ?`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]bool)
	for rows.Next() {
		var name string
		var enabled bool
		if err := rows.Scan(&name, &enabled); err != nil {
			return nil, err
		}
		result[name] = enabled
	}
	return result, rows.Err()
}

func (s *SQLiteBuiltinToolTenantConfigStore) Set(ctx context.Context, tenantID uuid.UUID, toolName string, enabled bool) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO builtin_tool_tenant_configs (tool_name, tenant_id, enabled, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT (tool_name, tenant_id) DO UPDATE SET enabled = excluded.enabled, updated_at = excluded.updated_at`,
		toolName, tenantID, enabled, time.Now().UTC(),
	)
	return err
}

func (s *SQLiteBuiltinToolTenantConfigStore) Delete(ctx context.Context, tenantID uuid.UUID, toolName string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM builtin_tool_tenant_configs WHERE tool_name = ? AND tenant_id = ?`,
		toolName, tenantID,
	)
	return err
}

// SQLiteSkillTenantConfigStore implements store.SkillTenantConfigStore.
type SQLiteSkillTenantConfigStore struct {
	db *sql.DB
}

func NewSQLiteSkillTenantConfigStore(db *sql.DB) *SQLiteSkillTenantConfigStore {
	return &SQLiteSkillTenantConfigStore{db: db}
}

func (s *SQLiteSkillTenantConfigStore) ListDisabledSkillIDs(ctx context.Context, tenantID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT skill_id FROM skill_tenant_configs WHERE tenant_id = ? AND enabled = 0`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *SQLiteSkillTenantConfigStore) ListAll(ctx context.Context, tenantID uuid.UUID) (map[uuid.UUID]bool, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT skill_id, enabled FROM skill_tenant_configs WHERE tenant_id = ?`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[uuid.UUID]bool)
	for rows.Next() {
		var id uuid.UUID
		var enabled bool
		if err := rows.Scan(&id, &enabled); err != nil {
			return nil, err
		}
		result[id] = enabled
	}
	return result, rows.Err()
}

func (s *SQLiteSkillTenantConfigStore) Set(ctx context.Context, tenantID uuid.UUID, skillID uuid.UUID, enabled bool) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO skill_tenant_configs (skill_id, tenant_id, enabled, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT (skill_id, tenant_id) DO UPDATE SET enabled = excluded.enabled, updated_at = excluded.updated_at`,
		skillID, tenantID, enabled, time.Now().UTC(),
	)
	return err
}

func (s *SQLiteSkillTenantConfigStore) Delete(ctx context.Context, tenantID uuid.UUID, skillID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM skill_tenant_configs WHERE skill_id = ? AND tenant_id = ?`,
		skillID, tenantID,
	)
	return err
}
