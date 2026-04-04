package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// MasterTenantID is the fixed UUID v7 for the default/master tenant.
// All existing data defaults to this tenant during migration.
var MasterTenantID = uuid.MustParse("0193a5b0-7000-7000-8000-000000000001")

// Tenant status constants.
const (
	TenantStatusActive    = "active"
	TenantStatusSuspended = "suspended"
	TenantStatusArchived  = "archived"
)

// Tenant role constants (hierarchy: owner > admin > operator > member > viewer).
const (
	TenantRoleOwner    = "owner"
	TenantRoleAdmin    = "admin"
	TenantRoleOperator = "operator"
	TenantRoleMember   = "member"
	TenantRoleViewer   = "viewer"
)

// TenantData represents a tenant in the database.
type TenantData struct {
	ID        uuid.UUID       `json:"id"`
	Name      string          `json:"name"`
	Slug      string          `json:"slug"`
	Status    string          `json:"status"`
	Settings  json.RawMessage `json:"settings,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// TenantUserData represents a user's membership in a tenant.
type TenantUserData struct {
	ID          uuid.UUID       `json:"id"`
	TenantID    uuid.UUID       `json:"tenant_id"`
	UserID      string          `json:"user_id"`
	DisplayName *string         `json:"display_name,omitempty"`
	Role        string          `json:"role"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// TenantStore manages tenants and tenant-user membership.
type TenantStore interface {
	// Tenant CRUD
	CreateTenant(ctx context.Context, tenant *TenantData) error
	GetTenant(ctx context.Context, id uuid.UUID) (*TenantData, error)
	GetTenantBySlug(ctx context.Context, slug string) (*TenantData, error)
	ListTenants(ctx context.Context) ([]TenantData, error)
	UpdateTenant(ctx context.Context, id uuid.UUID, updates map[string]any) error

	// Tenant-user membership
	AddUser(ctx context.Context, tenantID uuid.UUID, userID, role string) error
	RemoveUser(ctx context.Context, tenantID uuid.UUID, userID string) error
	GetUserRole(ctx context.Context, tenantID uuid.UUID, userID string) (string, error)
	ListUsers(ctx context.Context, tenantID uuid.UUID) ([]TenantUserData, error)
	ListUserTenants(ctx context.Context, userID string) ([]TenantUserData, error)

	// ResolveUserTenant returns the tenant_id for a user.
	// If user belongs to multiple tenants, returns the first (by created_at).
	// If no membership, returns MasterTenantID (backward compat).
	ResolveUserTenant(ctx context.Context, userID string) (uuid.UUID, error)

	// GetTenantUser returns a single tenant_user by primary key.
	GetTenantUser(ctx context.Context, id uuid.UUID) (*TenantUserData, error)

	// CreateTenantUserReturning creates a tenant_user and returns the row.
	// On conflict (tenant_id, user_id), updates role/display_name and returns existing row.
	CreateTenantUserReturning(ctx context.Context, tenantID uuid.UUID, userID, displayName, role string) (*TenantUserData, error)
}
