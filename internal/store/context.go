package store

import (
	"context"
	"encoding/json"
	"strings"

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
	// SharedSessionsKey indicates sessions should be shared across all users (no per-group scoping).
	SharedSessionsKey contextKey = "goclaw_shared_sessions"
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
	// CredentialUserIDKey holds the resolved tenant user identity for credential lookups.
	// Falls back to UserIDFromContext if not set.
	CredentialUserIDKey contextKey = "goclaw_credential_user_id"
	// SenderNameKey is the display name from channel metadata (for bootstrap auto-contact).
	SenderNameKey contextKey = "goclaw_sender_name"
	// AgentAudioKey carries the immutable agent audio snapshot for TTS tool dispatch.
	AgentAudioKey contextKey = "goclaw_agent_audio"
)

// AgentAudioSnapshot is an immutable snapshot of agent audio config carried through
// the tool-dispatch context. OtherConfig is a defensive byte copy taken at insertion
// time — callers MUST NOT mutate it after calling WithAgentAudio.
type AgentAudioSnapshot struct {
	AgentID     uuid.UUID
	OtherConfig json.RawMessage // immutable byte copy — never mutate after insertion
}

// WithAgentAudio returns a new context with the given agent audio snapshot.
// The snapshot is stored as-is — the CALLER is responsible for making a defensive
// copy of OtherConfig before calling this function (use append([]byte(nil), src...)).
func WithAgentAudio(ctx context.Context, snap AgentAudioSnapshot) context.Context {
	return context.WithValue(ctx, AgentAudioKey, snap)
}

// AgentAudioFromCtx extracts the agent audio snapshot from context.
// Returns ok=true only when the key is present AND AgentID != uuid.Nil.
func AgentAudioFromCtx(ctx context.Context) (AgentAudioSnapshot, bool) {
	snap, ok := ctx.Value(AgentAudioKey).(AgentAudioSnapshot)
	if !ok || snap.AgentID == uuid.Nil {
		return AgentAudioSnapshot{}, false
	}
	return snap, true
}

// WithShellDenyGroups returns a new context with shell deny group overrides.
func WithShellDenyGroups(ctx context.Context, groups map[string]bool) context.Context {
	return context.WithValue(ctx, ShellDenyGroupsKey, groups)
}

// ShellDenyGroupsFromContext returns shell deny group overrides from the context, or nil.
func ShellDenyGroupsFromContext(ctx context.Context) map[string]bool {
	if v, _ := ctx.Value(ShellDenyGroupsKey).(map[string]bool); v != nil {
		return v
	}
	if rc := RunContextFromCtx(ctx); rc != nil {
		return rc.ShellDenyGroups
	}
	return nil
}

// WithUserID returns a new context with the given user ID.
func WithUserID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, UserIDKey, id)
}

// UserIDFromContext extracts the user ID from context. Returns "" if not set.
func UserIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(UserIDKey).(string); ok && v != "" {
		return v
	}
	if rc := RunContextFromCtx(ctx); rc != nil {
		return rc.UserID
	}
	return ""
}

// WithCredentialUserID returns a new context with the resolved tenant user identity for credential lookups.
func WithCredentialUserID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, CredentialUserIDKey, id)
}

// CredentialUserIDFromContext returns the resolved identity for credential lookups.
// Falls back to RunContext.CredentialUserID, then UserIDFromContext.
func CredentialUserIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(CredentialUserIDKey).(string); ok && v != "" {
		return v
	}
	if rc := RunContextFromCtx(ctx); rc != nil && rc.CredentialUserID != "" {
		return rc.CredentialUserID
	}
	return UserIDFromContext(ctx)
}

// WithAgentID returns a new context with the given agent UUID.
func WithAgentID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, AgentIDKey, id)
}

// AgentIDFromContext extracts the agent UUID from context. Returns uuid.Nil if not set.
func AgentIDFromContext(ctx context.Context) uuid.UUID {
	if v, ok := ctx.Value(AgentIDKey).(uuid.UUID); ok && v != uuid.Nil {
		return v
	}
	if rc := RunContextFromCtx(ctx); rc != nil {
		return rc.AgentID
	}
	return uuid.Nil
}

// WithAgentType returns a new context with the given agent type.
func WithAgentType(ctx context.Context, t string) context.Context {
	return context.WithValue(ctx, AgentTypeKey, t)
}

// AgentTypeFromContext extracts the agent type from context. Returns "" if not set.
func AgentTypeFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(AgentTypeKey).(string); ok && v != "" {
		return v
	}
	if rc := RunContextFromCtx(ctx); rc != nil {
		return rc.AgentType
	}
	return ""
}

// WithAgentKey returns a new context with the agent key/name (string identifier).
func WithAgentKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, AgentKeyKey, key)
}

// AgentKeyFromContext extracts the agent key from context. Returns "" if not set.
func AgentKeyFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(AgentKeyKey).(string); ok && v != "" {
		return v
	}
	if rc := RunContextFromCtx(ctx); rc != nil {
		return rc.AgentKey
	}
	return ""
}

// WithSenderID returns a new context with the original individual sender ID.
func WithSenderID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, SenderIDKey, id)
}

// WithSenderName returns a new context with the sender display name from channel metadata.
func WithSenderName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, SenderNameKey, name)
}

// SenderNameFromContext extracts the sender display name. Returns "" if not set.
func SenderNameFromContext(ctx context.Context) string {
	v, _ := ctx.Value(SenderNameKey).(string)
	return v
}

// SenderIDFromContext extracts the sender ID from context. Returns "" if not set.
func SenderIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(SenderIDKey).(string); ok && v != "" {
		return v
	}
	if rc := RunContextFromCtx(ctx); rc != nil {
		return rc.SenderID
	}
	return ""
}

// ActorIDFromContext returns the acting principal — the entity performing
// the action. The resolution is context-aware:
//
//   - Group / guild scope (UserID has "group:" or "guild:" prefix):
//     returns SenderID (the individual sender) when set, else falls back
//     to UserID. SenderID is the only stable actor identity in a group
//     because UserID is the shared group principal.
//
//   - DM / HTTP / cron / anywhere UserID is a real user identifier:
//     returns UserID. This preserves tenant-user merging — the gateway
//     consumer rewrites UserID to the merged tenant identity (e.g.
//     "viettx") for DMs via ContactCollector.ResolveTenantUserID, so
//     ownership/audit records use the cross-channel stable identity
//     rather than a channel-specific raw sender (e.g. "386246614").
//     SenderID is used only as a fallback when UserID is empty.
//
// Use for:
//   - permission checks (file writers, role gates)
//   - audit trails (initiated_by, owner_id)
//   - ownership fields (skill publisher, cron owner)
//
// DO NOT use for:
//   - memory / KG / session scope (use UserIDFromContext / MemoryUserID / KGUserID)
//   - file-system or per-scope isolation (scope = group principal on purpose)
func ActorIDFromContext(ctx context.Context) string {
	uid := UserIDFromContext(ctx)
	// Group/guild: UserID is the scope namespace, not an actor. Prefer SenderID.
	if strings.HasPrefix(uid, "group:") || strings.HasPrefix(uid, "guild:") {
		if sid := SenderIDFromContext(ctx); sid != "" {
			return sid
		}
		return uid
	}
	// DM / HTTP / cron: UserID is (possibly merged) actor identity.
	if uid != "" {
		return uid
	}
	return SenderIDFromContext(ctx)
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
	if rc := RunContextFromCtx(ctx); rc != nil {
		return rc.SelfEvolve
	}
	return false
}

// WithSharedMemory returns a context flagged for shared memory (skip per-user scoping).
func WithSharedMemory(ctx context.Context) context.Context {
	return context.WithValue(ctx, SharedMemoryKey, true)
}

// IsSharedMemory returns true if memory should be shared across users.
func IsSharedMemory(ctx context.Context) bool {
	if v, ok := ctx.Value(SharedMemoryKey).(bool); ok {
		return v
	}
	if rc := RunContextFromCtx(ctx); rc != nil {
		return rc.SharedMemory
	}
	return false
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
	if v, ok := ctx.Value(SharedKGKey).(bool); ok {
		return v
	}
	if rc := RunContextFromCtx(ctx); rc != nil {
		return rc.SharedKG
	}
	return false
}

// WithSharedSessions returns a context flagged for shared sessions (skip per-group scoping).
func WithSharedSessions(ctx context.Context) context.Context {
	return context.WithValue(ctx, SharedSessionsKey, true)
}

// IsSharedSessions returns true if sessions should be shared across users/groups.
func IsSharedSessions(ctx context.Context) bool {
	if v, ok := ctx.Value(SharedSessionsKey).(bool); ok {
		return v
	}
	if rc := RunContextFromCtx(ctx); rc != nil {
		return rc.SharedSessions
	}
	return false
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
	if v, ok := ctx.Value(TenantIDKey).(uuid.UUID); ok && v != uuid.Nil {
		return v
	}
	if rc := RunContextFromCtx(ctx); rc != nil {
		return rc.TenantID
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

// IsMasterScope reports whether ctx should be treated as master-scope:
//
//	(a) system owner role (IsOwnerRole bypass-all), or
//	(b) tenant id is unset (uuid.Nil — legacy / system callers), or
//	(c) tenant id equals MasterTenantID.
//
// Used by both WS config.* methods and HTTP admin routes that write to
// global (non-tenant-scoped) tables or execute server-wide side effects
// (shell, filesystem). Centralises the Phase 1 / Phase 0b hotfix rule
// so every layer shares one predicate — no drift.
func IsMasterScope(ctx context.Context) bool {
	if IsOwnerRole(ctx) {
		return true
	}
	tid := TenantIDFromContext(ctx)
	return tid == uuid.Nil || tid == MasterTenantID
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
