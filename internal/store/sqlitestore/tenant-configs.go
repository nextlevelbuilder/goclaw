//go:build sqlite || sqliteonly

package sqlitestore

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

// SQLiteBuiltinToolTenantConfigStore implements store.BuiltinToolTenantConfigStore.
type SQLiteBuiltinToolTenantConfigStore struct {
	db *sql.DB
}

func NewSQLiteBuiltinToolTenantConfigStore(db *sql.DB) *SQLiteBuiltinToolTenantConfigStore {
	return &SQLiteBuiltinToolTenantConfigStore{db: db}
}

func (s *SQLiteBuiltinToolTenantConfigStore) ListDisabled(ctx context.Context, tenantID uuid.UUID) ([]string, error) {
	if tenantID == uuid.Nil {
		return nil, store.ErrInvalidTenant
	}
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
	if tenantID == uuid.Nil {
		return nil, store.ErrInvalidTenant
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT tool_name, enabled FROM builtin_tool_tenant_configs WHERE tenant_id = ? AND enabled IS NOT NULL`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]bool)
	for rows.Next() {
		var name string
		var enabled sql.NullBool
		if err := rows.Scan(&name, &enabled); err != nil {
			return nil, err
		}
		if enabled.Valid {
			result[name] = enabled.Bool
		}
	}
	return result, rows.Err()
}

// Set upserts the enabled flag. Preserves the settings column.
func (s *SQLiteBuiltinToolTenantConfigStore) Set(ctx context.Context, tenantID uuid.UUID, toolName string, enabled bool) error {
	if tenantID == uuid.Nil {
		return store.ErrInvalidTenant
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO builtin_tool_tenant_configs (tool_name, tenant_id, enabled, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT (tool_name, tenant_id)
		 DO UPDATE SET enabled = excluded.enabled, updated_at = excluded.updated_at`,
		toolName, tenantID, enabled, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("set tenant tool enabled: %w", err)
	}
	return nil
}

func (s *SQLiteBuiltinToolTenantConfigStore) Delete(ctx context.Context, tenantID uuid.UUID, toolName string) error {
	if tenantID == uuid.Nil {
		return store.ErrInvalidTenant
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM builtin_tool_tenant_configs WHERE tool_name = ? AND tenant_id = ?`,
		toolName, tenantID,
	)
	return err
}

// GetSettings returns the raw settings JSON for a tool.
// Missing row or NULL column → (nil, nil).
func (s *SQLiteBuiltinToolTenantConfigStore) GetSettings(ctx context.Context, tenantID uuid.UUID, toolName string) (json.RawMessage, error) {
	if tenantID == uuid.Nil {
		return nil, store.ErrInvalidTenant
	}
	var raw sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT settings FROM builtin_tool_tenant_configs
		 WHERE tool_name = ? AND tenant_id = ?`,
		toolName, tenantID,
	).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get tenant tool settings: %w", err)
	}
	if !raw.Valid || len(raw.String) == 0 {
		return nil, nil
	}
	return json.RawMessage(raw.String), nil
}

// SetSettings upserts the settings JSON. Preserves enabled column. nil → SQL NULL.
func (s *SQLiteBuiltinToolTenantConfigStore) SetSettings(ctx context.Context, tenantID uuid.UUID, toolName string, settings json.RawMessage) error {
	if tenantID == uuid.Nil {
		return store.ErrInvalidTenant
	}
	var settingsArg any
	if settings != nil {
		// SQLite stores JSON as TEXT — pass as string for portable NULL handling.
		settingsArg = string(settings)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO builtin_tool_tenant_configs (tool_name, tenant_id, settings, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT (tool_name, tenant_id)
		 DO UPDATE SET settings = excluded.settings, updated_at = excluded.updated_at`,
		toolName, tenantID, settingsArg, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("set tenant tool settings: %w", err)
	}
	return nil
}

// ListAllSettings returns tool_name → raw settings JSON for every tenant row
// with a non-null settings column.
func (s *SQLiteBuiltinToolTenantConfigStore) ListAllSettings(ctx context.Context, tenantID uuid.UUID) (map[string]json.RawMessage, error) {
	if tenantID == uuid.Nil {
		return nil, store.ErrInvalidTenant
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT tool_name, settings FROM builtin_tool_tenant_configs
		 WHERE tenant_id = ? AND settings IS NOT NULL`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("list tenant tool settings: %w", err)
	}
	defer rows.Close()
	result := make(map[string]json.RawMessage)
	for rows.Next() {
		var name string
		var raw sql.NullString
		if err := rows.Scan(&name, &raw); err != nil {
			return nil, err
		}
		if raw.Valid && len(raw.String) > 0 {
			result[name] = json.RawMessage(raw.String)
		}
	}
	return result, rows.Err()
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
