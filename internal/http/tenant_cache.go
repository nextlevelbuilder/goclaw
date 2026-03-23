package http

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// tenantCacheEntry holds a cached tenant lookup result.
type tenantCacheEntry struct {
	tenant    *store.TenantData
	fetchedAt time.Time
}

// tenantCache is a TTL cache for tenant lookups by UUID and slug.
// Invalidated via bus CacheKindTenants events.
type tenantCache struct {
	mu      sync.RWMutex
	byID    map[uuid.UUID]*tenantCacheEntry
	bySlug  map[string]*tenantCacheEntry
	ttl     time.Duration
	store   store.TenantStore
}

func newTenantCache(s store.TenantStore, ttl time.Duration) *tenantCache {
	return &tenantCache{
		byID:   make(map[uuid.UUID]*tenantCacheEntry),
		bySlug: make(map[string]*tenantCacheEntry),
		ttl:    ttl,
		store:  s,
	}
}

// GetTenant returns a tenant by UUID, using cache when available.
func (c *tenantCache) GetTenant(ctx context.Context, id uuid.UUID) (*store.TenantData, error) {
	c.mu.RLock()
	if e, ok := c.byID[id]; ok && time.Since(e.fetchedAt) <= c.ttl {
		c.mu.RUnlock()
		slog.Debug("tenant_cache.hit", "id", id)
		return e.tenant, nil
	}
	c.mu.RUnlock()
	slog.Debug("tenant_cache.miss", "id", id)

	t, err := c.store.GetTenant(ctx, id)
	if err != nil {
		return nil, err
	}
	if t != nil {
		c.put(t)
	}
	return t, nil
}

// GetTenantBySlug returns a tenant by slug, using cache when available.
func (c *tenantCache) GetTenantBySlug(ctx context.Context, slug string) (*store.TenantData, error) {
	c.mu.RLock()
	if e, ok := c.bySlug[slug]; ok && time.Since(e.fetchedAt) <= c.ttl {
		c.mu.RUnlock()
		slog.Debug("tenant_cache.hit", "slug", slug)
		return e.tenant, nil
	}
	c.mu.RUnlock()
	slog.Debug("tenant_cache.miss", "slug", slug)

	t, err := c.store.GetTenantBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	if t != nil {
		c.put(t)
	}
	return t, nil
}

func (c *tenantCache) put(t *store.TenantData) {
	entry := &tenantCacheEntry{tenant: t, fetchedAt: time.Now()}
	c.mu.Lock()
	c.byID[t.ID] = entry
	c.bySlug[t.Slug] = entry
	c.mu.Unlock()
}

// invalidateAll clears all cached entries. Called on bus cache.invalidate events.
func (c *tenantCache) invalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	slog.Debug("tenant_cache.invalidated", "entries", len(c.byID))
	c.byID = make(map[uuid.UUID]*tenantCacheEntry)
	c.bySlug = make(map[string]*tenantCacheEntry)
}
