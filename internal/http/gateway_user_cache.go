package http

import (
	"context"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// gwUserCacheEntry holds a cached gateway user lookup result.
type gwUserCacheEntry struct {
	user      *store.GatewayUserData // nil = negative cache (token not found)
	role      permissions.Role
	fetchedAt time.Time
}

// gatewayUserCache is a TTL cache for gateway user token lookups.
type gatewayUserCache struct {
	mu      sync.RWMutex
	entries map[string]*gwUserCacheEntry // keyed by token
	ttl     time.Duration
	store   store.GatewayUserStore
}

func newGatewayUserCache(s store.GatewayUserStore, ttl time.Duration) *gatewayUserCache {
	return &gatewayUserCache{
		entries: make(map[string]*gwUserCacheEntry),
		ttl:     ttl,
		store:   s,
	}
}

// getOrFetch returns a cached gateway user or fetches from the store on cache miss.
func (c *gatewayUserCache) getOrFetch(ctx context.Context, token string) (*store.GatewayUserData, permissions.Role) {
	c.mu.RLock()
	entry, ok := c.entries[token]
	if ok && time.Since(entry.fetchedAt) <= c.ttl {
		c.mu.RUnlock()
		return entry.user, entry.role
	}
	c.mu.RUnlock()

	// Cache miss — fetch from DB
	user, err := c.store.GetByToken(ctx, token)
	if err != nil || user == nil {
		// Negative cache
		c.mu.Lock()
		if len(c.entries) < maxNegativeCacheEntries {
			c.entries[token] = &gwUserCacheEntry{fetchedAt: time.Now()}
		}
		c.mu.Unlock()
		return nil, ""
	}

	role := gwRoleToPermRole(user.Role)

	c.mu.Lock()
	c.entries[token] = &gwUserCacheEntry{
		user:      user,
		role:      role,
		fetchedAt: time.Now(),
	}
	c.mu.Unlock()

	return user, role
}

// invalidateAll clears all cached entries.
func (c *gatewayUserCache) invalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*gwUserCacheEntry)
}

// gwRoleToPermRole maps gateway_users.role to permissions.Role.
func gwRoleToPermRole(role string) permissions.Role {
	switch role {
	case "root":
		return permissions.RoleAdmin
	case "admin":
		return permissions.RoleAdmin
	default:
		return permissions.RoleViewer
	}
}
