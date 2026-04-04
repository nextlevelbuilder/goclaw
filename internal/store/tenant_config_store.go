package store

import (
	"context"

	"github.com/google/uuid"
)

// BuiltinToolTenantConfig represents a per-tenant override for a builtin tool.
type BuiltinToolTenantConfig struct {
	ToolName string `json:"tool_name"`
	TenantID uuid.UUID `json:"tenant_id"`
	Enabled  *bool  `json:"enabled,omitempty"` // nil = use default, false = disabled, true = enabled
}

// BuiltinToolTenantConfigStore manages per-tenant builtin tool overrides.
type BuiltinToolTenantConfigStore interface {
	// ListDisabled returns tool names disabled for a tenant.
	ListDisabled(ctx context.Context, tenantID uuid.UUID) ([]string, error)
	// ListAll returns all tenant overrides (tool_name → enabled) for a tenant.
	ListAll(ctx context.Context, tenantID uuid.UUID) (map[string]bool, error)
	// Set creates or updates a tenant tool config.
	Set(ctx context.Context, tenantID uuid.UUID, toolName string, enabled bool) error
	// Delete removes a tenant tool config (reverts to default).
	Delete(ctx context.Context, tenantID uuid.UUID, toolName string) error
}

// SkillTenantConfig represents a per-tenant override for a skill.
type SkillTenantConfig struct {
	SkillID  uuid.UUID `json:"skill_id"`
	TenantID uuid.UUID `json:"tenant_id"`
	Enabled  bool      `json:"enabled"`
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
