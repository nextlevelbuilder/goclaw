package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGBuiltinToolTenantConfigStore implements store.BuiltinToolTenantConfigStore.
type PGBuiltinToolTenantConfigStore struct {
	db *sql.DB
}

func NewPGBuiltinToolTenantConfigStore(db *sql.DB) *PGBuiltinToolTenantConfigStore {
	return &PGBuiltinToolTenantConfigStore{db: db}
}

func (s *PGBuiltinToolTenantConfigStore) ListDisabled(ctx context.Context, tenantID uuid.UUID) ([]string, error) {
	if tenantID == uuid.Nil {
		return nil, store.ErrInvalidTenant
	}
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
	if tenantID == uuid.Nil {
		return nil, store.ErrInvalidTenant
	}
	type row struct {
		ToolName string `db:"tool_name"`
		Enabled  *bool  `db:"enabled"`
	}
	var rows []row
	if err := pkgSqlxDB.SelectContext(ctx, &rows,
		`SELECT tool_name, enabled FROM builtin_tool_tenant_configs WHERE tenant_id = $1 AND enabled IS NOT NULL`,
		tenantID,
	); err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(rows))
	for _, r := range rows {
		if r.Enabled != nil {
			result[r.ToolName] = *r.Enabled
		}
	}
	return result, nil
}

// Set upserts the enabled flag. Preserves the settings column via explicit
// column list in DO UPDATE SET — a concurrent settings write is untouched.
func (s *PGBuiltinToolTenantConfigStore) Set(ctx context.Context, tenantID uuid.UUID, toolName string, enabled bool) error {
	if tenantID == uuid.Nil {
		return store.ErrInvalidTenant
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO builtin_tool_tenant_configs (tool_name, tenant_id, enabled, updated_at)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (tool_name, tenant_id)
		 DO UPDATE SET enabled = EXCLUDED.enabled, updated_at = EXCLUDED.updated_at`,
		toolName, tenantID, enabled, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("set tenant tool enabled: %w", err)
	}
	return nil
}

func (s *PGBuiltinToolTenantConfigStore) Delete(ctx context.Context, tenantID uuid.UUID, toolName string) error {
	if tenantID == uuid.Nil {
		return store.ErrInvalidTenant
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM builtin_tool_tenant_configs WHERE tool_name = $1 AND tenant_id = $2`,
		toolName, tenantID,
	)
	return err
}

// GetSettings returns the raw settings JSON for a tool.
// Missing row or NULL column → (nil, nil) so callers fall back to global/hardcoded defaults.
func (s *PGBuiltinToolTenantConfigStore) GetSettings(ctx context.Context, tenantID uuid.UUID, toolName string) (json.RawMessage, error) {
	if tenantID == uuid.Nil {
		return nil, store.ErrInvalidTenant
	}
	var raw []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT settings FROM builtin_tool_tenant_configs
		 WHERE tool_name = $1 AND tenant_id = $2`,
		toolName, tenantID,
	).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get tenant tool settings: %w", err)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	return json.RawMessage(raw), nil
}

// SetSettings upserts the settings JSON. Preserves enabled column via explicit
// column list in DO UPDATE SET. Passing settings=nil writes SQL NULL (clears override).
func (s *PGBuiltinToolTenantConfigStore) SetSettings(ctx context.Context, tenantID uuid.UUID, toolName string, settings json.RawMessage) error {
	if tenantID == uuid.Nil {
		return store.ErrInvalidTenant
	}
	var settingsArg any
	if settings != nil {
		settingsArg = []byte(settings)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO builtin_tool_tenant_configs (tool_name, tenant_id, settings, updated_at)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (tool_name, tenant_id)
		 DO UPDATE SET settings = EXCLUDED.settings, updated_at = EXCLUDED.updated_at`,
		toolName, tenantID, settingsArg, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("set tenant tool settings: %w", err)
	}
	return nil
}

// ListAllSettings returns tool_name → raw settings JSON for every tenant row
// with a non-null settings column. Used by the agent resolver to bulk-load
// tenant settings at Loop construction.
func (s *PGBuiltinToolTenantConfigStore) ListAllSettings(ctx context.Context, tenantID uuid.UUID) (map[string]json.RawMessage, error) {
	if tenantID == uuid.Nil {
		return nil, store.ErrInvalidTenant
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT tool_name, settings FROM builtin_tool_tenant_configs
		 WHERE tenant_id = $1 AND settings IS NOT NULL`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("list tenant tool settings: %w", err)
	}
	defer rows.Close()
	result := make(map[string]json.RawMessage)
	for rows.Next() {
		var name string
		var raw []byte
		if err := rows.Scan(&name, &raw); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			result[name] = json.RawMessage(raw)
		}
	}
	return result, rows.Err()
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
