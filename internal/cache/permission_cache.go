package cache

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// agentAccessEntry caches agent access check results.
type agentAccessEntry struct {
	Allowed bool
	Role    string
}

// PermissionCache provides short-TTL caching for hot permission lookups.
// Uses InMemoryCache[V] caches with pubsub invalidation.
type PermissionCache struct {
	tenantResolve *InMemoryCache[uuid.UUID]
	tenantRole    *InMemoryCache[string]
	agentAccess   *InMemoryCache[agentAccessEntry]
	teamAccess    *InMemoryCache[bool]
}

// NewPermissionCache creates a new permission cache.
func NewPermissionCache() *PermissionCache {
	return &PermissionCache{
		tenantResolve: NewInMemoryCache[uuid.UUID](),
		tenantRole:    NewInMemoryCache[string](),
		agentAccess:   NewInMemoryCache[agentAccessEntry](),
		teamAccess:    NewInMemoryCache[bool](),
	}
}

const (
	tenantResolveTTL = 60 * time.Second
	tenantRoleTTL    = 30 * time.Second
	agentAccessTTL   = 30 * time.Second
	teamAccessTTL    = 30 * time.Second
)

// --- Tenant Resolution ---

func (pc *PermissionCache) GetTenantResolve(ctx context.Context, userID string) (uuid.UUID, bool) {
	return pc.tenantResolve.Get(ctx, userID)
}

func (pc *PermissionCache) SetTenantResolve(ctx context.Context, userID string, tenantID uuid.UUID) {
	pc.tenantResolve.Set(ctx, userID, tenantID, tenantResolveTTL)
}

// --- Tenant Role ---

func (pc *PermissionCache) GetTenantRole(ctx context.Context, tenantID uuid.UUID, userID string) (string, bool) {
	return pc.tenantRole.Get(ctx, tenantID.String()+":"+userID)
}

func (pc *PermissionCache) SetTenantRole(ctx context.Context, tenantID uuid.UUID, userID, role string) {
	pc.tenantRole.Set(ctx, tenantID.String()+":"+userID, role, tenantRoleTTL)
}

// --- Agent Access ---

func (pc *PermissionCache) GetAgentAccess(ctx context.Context, agentID uuid.UUID, userID string) (bool, string, bool) {
	entry, ok := pc.agentAccess.Get(ctx, agentID.String()+":"+userID)
	if !ok {
		return false, "", false
	}
	return entry.Allowed, entry.Role, true
}

func (pc *PermissionCache) SetAgentAccess(ctx context.Context, agentID uuid.UUID, userID string, allowed bool, role string) {
	pc.agentAccess.Set(ctx, agentID.String()+":"+userID, agentAccessEntry{Allowed: allowed, Role: role}, agentAccessTTL)
}

// --- Team Access ---

func (pc *PermissionCache) GetTeamAccess(ctx context.Context, teamID uuid.UUID, userID string) (bool, bool) {
	return pc.teamAccess.Get(ctx, teamID.String()+":"+userID)
}

func (pc *PermissionCache) SetTeamAccess(ctx context.Context, teamID uuid.UUID, userID string, allowed bool) {
	pc.teamAccess.Set(ctx, teamID.String()+":"+userID, allowed, teamAccessTTL)
}

// --- Invalidation ---

// HandleInvalidation processes a cache invalidation event from the bus.
func (pc *PermissionCache) HandleInvalidation(p bus.CacheInvalidatePayload) {
	slog.Debug("perm_cache.invalidated", "kind", string(p.Kind), "key", p.Key)
	ctx := context.Background()
	switch p.Kind {
	case bus.CacheKindTenantUsers:
		// Key is userID — invalidate resolve + all tenant roles for this user.
		// Can't efficiently delete all tenantRole entries for a user by prefix,
		// so clear all tenant roles (short TTL makes this acceptable).
		if p.Key != "" {
			pc.tenantResolve.Delete(ctx, p.Key)
			pc.tenantRole.Clear(ctx)
		} else {
			pc.tenantResolve.Clear(ctx)
			pc.tenantRole.Clear(ctx)
		}
	case bus.CacheKindAgentAccess:
		// Key is agentID — delete all access entries for this agent.
		if p.Key != "" {
			pc.agentAccess.DeleteByPrefix(ctx, p.Key+":")
		} else {
			pc.agentAccess.Clear(ctx)
		}
	case bus.CacheKindTeamAccess:
		// Key is teamID — delete all access entries for this team.
		if p.Key != "" {
			pc.teamAccess.DeleteByPrefix(ctx, p.Key+":")
		} else {
			pc.teamAccess.Clear(ctx)
		}
	}
}
