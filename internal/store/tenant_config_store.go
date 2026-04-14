package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

// ErrInvalidTenant is returned when a store method requires an explicit tenant
// but receives uuid.Nil. Callers can check via errors.Is(err, store.ErrInvalidTenant).
var ErrInvalidTenant = errors.New("tenant_id cannot be nil")

// BuiltinToolTenantConfig represents a per-tenant override for a builtin tool.
//
// Two independent override dimensions:
//   - Enabled: tenant admin can force-enable or force-disable the tool (nil = use default).
//   - Settings: tenant admin can override the tool's config JSON blob (nil = no override,
//     falls back to global builtin_tools.settings).
//
// The two columns are managed by distinct Set*/Get* methods so a write to one never
// clobbers the other (see PG/SQLite impl: upsert uses explicit column list on conflict).
type BuiltinToolTenantConfig struct {
	ToolName string          `json:"tool_name" db:"tool_name"`
	TenantID uuid.UUID       `json:"tenant_id" db:"tenant_id"`
	Enabled  *bool           `json:"enabled,omitempty" db:"enabled"`   // nil = use default, false = disabled, true = enabled
	Settings json.RawMessage `json:"settings,omitempty" db:"settings"` // nil = no override; tool uses global/hardcoded default
}

// BuiltinToolTenantConfigStore manages per-tenant builtin tool overrides.
//
// All methods require an explicit tenantID parameter — passing uuid.Nil returns
// ErrInvalidTenant. This avoids silent master-default leaks via fallback logic.
type BuiltinToolTenantConfigStore interface {
	// ListDisabled returns tool names disabled for a tenant.
	ListDisabled(ctx context.Context, tenantID uuid.UUID) ([]string, error)
	// ListAll returns all tenant enabled overrides (tool_name → enabled) for a tenant.
	ListAll(ctx context.Context, tenantID uuid.UUID) (map[string]bool, error)
	// Set creates or updates a tenant tool's enabled override. Preserves settings column.
	Set(ctx context.Context, tenantID uuid.UUID, toolName string, enabled bool) error
	// Delete removes a tenant tool config row entirely (reverts both enabled + settings).
	Delete(ctx context.Context, tenantID uuid.UUID, toolName string) error

	// GetSettings returns the raw tenant settings JSON for a tool.
	// Returns (nil, nil) when the row doesn't exist or settings column is NULL.
	GetSettings(ctx context.Context, tenantID uuid.UUID, toolName string) (json.RawMessage, error)
	// SetSettings upserts the tenant settings JSON for a tool. Preserves enabled column.
	// Passing nil writes SQL NULL (clears the override without deleting the row).
	SetSettings(ctx context.Context, tenantID uuid.UUID, toolName string, settings json.RawMessage) error
	// ListAllSettings returns tool_name → settings JSON for every row where settings IS NOT NULL.
	// Used by the agent resolver to bulk-load tenant settings at Loop construction.
	ListAllSettings(ctx context.Context, tenantID uuid.UUID) (map[string]json.RawMessage, error)
}

// SkillTenantConfig represents a per-tenant override for a skill.
type SkillTenantConfig struct {
	SkillID  uuid.UUID `json:"skill_id" db:"skill_id"`
	TenantID uuid.UUID `json:"tenant_id" db:"tenant_id"`
	Enabled  bool      `json:"enabled" db:"enabled"`
}

// SkillTenantConfigStore manages per-tenant skill visibility.
type SkillTenantConfigStore interface {
	// ListDisabledSkillIDs returns skill IDs disabled for a tenant.
	ListDisabledSkillIDs(ctx context.Context, tenantID uuid.UUID) ([]uuid.UUID, error)
	// ListAll returns all tenant overrides (skillID → enabled) for a tenant.
	ListAll(ctx context.Context, tenantID uuid.UUID) (map[uuid.UUID]bool, error)
	// Set creates or updates a tenant skill config.
	Set(ctx context.Context, tenantID uuid.UUID, skillID uuid.UUID, enabled bool) error
	// Delete removes a tenant skill config (reverts to default).
	Delete(ctx context.Context, tenantID uuid.UUID, skillID uuid.UUID) error
}
