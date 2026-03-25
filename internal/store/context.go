package store

import (
	"context"

	"github.com/google/uuid"
)

type contextKey string

const (
	// UserIDKey is the context key for the external user ID (TEXT, free-form).
	UserIDKey contextKey = "goclaw_user_id"
	// AgentIDKey is the context key for the agent UUID.
	AgentIDKey contextKey = "goclaw_agent_id"
	// AgentTypeKey is the context key for the agent type ("open" or "predefined").
	AgentTypeKey contextKey = "goclaw_agent_type"
	// SenderIDKey is the original individual sender's ID (not group-scoped).
	// In group chats, UserIDKey is group-scoped but SenderIDKey preserves
	// the actual person who sent the message.
	SenderIDKey contextKey = "goclaw_sender_id"
	// SelfEvolveKey indicates whether a predefined agent can update its SOUL.md.
	SelfEvolveKey contextKey = "goclaw_self_evolve"
	// LocaleKey is the context key for the user's preferred locale (e.g. "en", "vi", "zh").
	LocaleKey contextKey = "goclaw_locale"
	// SharedMemoryKey indicates memory should be shared (no per-user scoping).
	SharedMemoryKey contextKey = "goclaw_shared_memory"
	// SharedKGKey indicates KG should be shared across all users of the agent (no per-user scoping).
	SharedKGKey contextKey = "goclaw_shared_kg"
	// ShellDenyGroupsKey holds per-agent shell deny group overrides.
	ShellDenyGroupsKey contextKey = "goclaw_shell_deny_groups"
	// AgentKeyKey is the context key for the agent key/name (string identifier, e.g. "default").
	AgentKeyKey contextKey = "goclaw_agent_key"
	// TenantIDKey is the context key for the tenant UUID.
	TenantIDKey contextKey = "goclaw_tenant_id"
	// CrossTenantKey indicates the caller has cross-tenant access (owner/system admin).
	CrossTenantKey contextKey = "goclaw_cross_tenant"
	// TenantSlugKey stores the tenant's URL-safe slug for filesystem paths.
	TenantSlugKey contextKey = "goclaw_tenant_slug"
	// RoleKey is the context key for the caller's permission role (e.g. "admin", "operator", "viewer").
	RoleKey contextKey = "goclaw_role"
)

// WithShellDenyGroups returns a new context with shell deny group overrides.
func WithShellDenyGroups(ctx context.Context, groups map[string]bool) context.Context {
	return context.WithValue(ctx, ShellDenyGroupsKey, groups)
}

// ShellDenyGroupsFromContext returns shell deny group overrides from the context, or nil.
func ShellDenyGroupsFromContext(ctx context.Context) map[string]bool {
	v, _ := ctx.Value(ShellDenyGroupsKey).(map[string]bool)
	return v
}

// WithUserID returns a new context with the given user ID.
func WithUserID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, UserIDKey, id)
}

// UserIDFromContext extracts the user ID from context. Returns "" if not set.
func UserIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(UserIDKey).(string); ok {
		return v
	}
	return ""
}

// WithAgentID returns a new context with the given agent UUID.
func WithAgentID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, AgentIDKey, id)
}

// AgentIDFromContext extracts the agent UUID from context. Returns uuid.Nil if not set.
func AgentIDFromContext(ctx context.Context) uuid.UUID {
	if v, ok := ctx.Value(AgentIDKey).(uuid.UUID); ok {
		return v
	}
	return uuid.Nil
}

// WithAgentType returns a new context with the given agent type.
func WithAgentType(ctx context.Context, t string) context.Context {
	return context.WithValue(ctx, AgentTypeKey, t)
}

// AgentTypeFromContext extracts the agent type from context. Returns "" if not set.
func AgentTypeFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(AgentTypeKey).(string); ok {
		return v
	}
	return ""
}

// WithAgentKey returns a new context with the agent key/name (string identifier).
func WithAgentKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, AgentKeyKey, key)
}

// AgentKeyFromContext extracts the agent key from context. Returns "" if not set.
func AgentKeyFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(AgentKeyKey).(string); ok {
		return v
	}
	return ""
}

// WithSenderID returns a new context with the original individual sender ID.
func WithSenderID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, SenderIDKey, id)
}

// SenderIDFromContext extracts the sender ID from context. Returns "" if not set.
func SenderIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(SenderIDKey).(string); ok {
		return v
	}
	return ""
}

// WithSelfEvolve returns a new context with the self-evolve flag.
func WithSelfEvolve(ctx context.Context, v bool) context.Context {
	return context.WithValue(ctx, SelfEvolveKey, v)
}

// SelfEvolveFromContext extracts the self-evolve flag from context. Returns false if not set.
func SelfEvolveFromContext(ctx context.Context) bool {
	if v, ok := ctx.Value(SelfEvolveKey).(bool); ok {
		return v
	}
	return false
}

// WithSharedMemory returns a context flagged for shared memory (skip per-user scoping).
func WithSharedMemory(ctx context.Context) context.Context {
	return context.WithValue(ctx, SharedMemoryKey, true)
}

// IsSharedMemory returns true if memory should be shared across users.
func IsSharedMemory(ctx context.Context) bool {
	v, _ := ctx.Value(SharedMemoryKey).(bool)
	return v
}

// MemoryUserID returns the userID to use for memory operations.
// Returns "" (shared/global) when shared memory is active, otherwise the per-user ID.
func MemoryUserID(ctx context.Context) string {
	if IsSharedMemory(ctx) {
		return ""
	}
	return UserIDFromContext(ctx)
}

// KGUserID returns the userID to use for knowledge graph operations.
// Returns "" (agent-level scope) when shared KG is active, otherwise the per-user ID.
func KGUserID(ctx context.Context) string {
	if IsSharedKG(ctx) {
		return ""
	}
	return UserIDFromContext(ctx)
}

// WithSharedKG returns a context flagged for shared knowledge graph (agent-level, no per-user scoping).
func WithSharedKG(ctx context.Context) context.Context {
	return context.WithValue(ctx, SharedKGKey, true)
}

// IsSharedKG returns true if the knowledge graph should be shared across users.
func IsSharedKG(ctx context.Context) bool {
	v, _ := ctx.Value(SharedKGKey).(bool)
	return v
}

// WithLocale returns a new context with the given locale.
func WithLocale(ctx context.Context, locale string) context.Context {
	return context.WithValue(ctx, LocaleKey, locale)
}

// LocaleFromContext extracts the locale from context. Returns "en" if not set.
func LocaleFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(LocaleKey).(string); ok && v != "" {
		return v
	}
	return "en"
}

// WithTenantID returns a new context with the given tenant UUID.
func WithTenantID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, TenantIDKey, id)
}

// TenantIDFromContext extracts the tenant UUID from context.
// Returns uuid.Nil if not set (fail-closed — callers must check).
func TenantIDFromContext(ctx context.Context) uuid.UUID {
	if v, ok := ctx.Value(TenantIDKey).(uuid.UUID); ok {
		return v
	}
	return uuid.Nil
}

// WithCrossTenant returns a context flagged for cross-tenant access.
// Deprecated: Only used by skills store (is_system dual-visibility pattern).
// All other callers must use explicit tenant context or unscoped store methods.
func WithCrossTenant(ctx context.Context) context.Context {
	return context.WithValue(ctx, CrossTenantKey, true)
}

// IsCrossTenant returns true if the caller has cross-tenant access.
// Deprecated: Only used by skills store and inline pg/*.go tenant checks.
// Permission guards should use IsOwnerRole(). SQL queries use tenantClauseN() (no bypass).
func IsCrossTenant(ctx context.Context) bool {
	v, _ := ctx.Value(CrossTenantKey).(bool)
	return v
}

// IsOwnerRole returns true if the caller has the "owner" role.
// Replaces IsCrossTenant for permission guards.
func IsOwnerRole(ctx context.Context) bool {
	return RoleFromContext(ctx) == string(RoleOwner)
}

// RoleOwner is the owner role constant for context checks.
// Must match permissions.RoleOwner.
const RoleOwner = "owner"

// WithTenantSlug returns a new context with the given tenant slug.
func WithTenantSlug(ctx context.Context, slug string) context.Context {
	return context.WithValue(ctx, TenantSlugKey, slug)
}

// TenantSlugFromContext extracts the tenant slug from context. Returns "" if not set.
func TenantSlugFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(TenantSlugKey).(string); ok {
		return v
	}
	return ""
}

// WithRole returns a new context with the caller's permission role.
func WithRole(ctx context.Context, role string) context.Context {
	return context.WithValue(ctx, RoleKey, role)
}

// RoleFromContext extracts the permission role from context. Returns "" if not set.
func RoleFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(RoleKey).(string); ok {
		return v
	}
	return ""
}
