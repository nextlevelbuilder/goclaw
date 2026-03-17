package http

import (
	"context"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// cacheEntry holds a cached API key lookup result.
type cacheEntry struct {
	key       *store.APIKeyData // nil = negative cache (key not found)
	role      permissions.Role
	fetchedAt time.Time
}

// maxNegativeCacheEntries caps the number of negative cache entries to prevent
// memory exhaustion from token spraying attacks.
const maxNegativeCacheEntries = 10000

// apiKeyCache is a TTL cache for API key lookups, invalidated via pubsub.
type apiKeyCache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry // keyed by SHA-256 hash
	ttl     time.Duration
	store   store.APIKeyStore
}

func newAPIKeyCache(s store.APIKeyStore, ttl time.Duration) *apiKeyCache {
	return &apiKeyCache{
		entries: make(map[string]*cacheEntry),
		ttl:     ttl,
		store:   s,
	}
}

// get returns a cached entry if it exists and is not expired.
func (c *apiKeyCache) get(hash string) (*store.APIKeyData, permissions.Role, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[hash]
	if !ok || time.Since(entry.fetchedAt) > c.ttl {
		return nil, "", false
	}
	return entry.key, entry.role, true
}

// getOrFetch returns a cached entry or fetches from the store on cache miss.
func (c *apiKeyCache) getOrFetch(ctx context.Context, hash string) (*store.APIKeyData, permissions.Role) {
	if key, role, ok := c.get(hash); ok {
		return key, role
	}

	// Cache miss — fetch from DB
	keyData, err := c.store.GetByHash(ctx, hash)
	if err != nil || keyData == nil {
		// Negative cache: avoid repeated DB misses for invalid tokens.
		// Cap size to prevent memory exhaustion from token spraying.
		c.mu.Lock()
		if len(c.entries) < maxNegativeCacheEntries {
			c.entries[hash] = &cacheEntry{fetchedAt: time.Now()}
		}
		c.mu.Unlock()
		return nil, ""
	}

	scopes := make([]permissions.Scope, len(keyData.Scopes))
	for i, s := range keyData.Scopes {
		scopes[i] = permissions.Scope(s)
	}
	role := permissions.RoleFromScopes(scopes)

	c.mu.Lock()
	c.entries[hash] = &cacheEntry{
		key:       keyData,
		role:      role,
		fetchedAt: time.Now(),
	}
	c.mu.Unlock()

	// Touch last-used in background (fire-and-forget with timeout)
	go func() {
		tctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		c.store.TouchLastUsed(tctx, keyData.ID)
	}()

	return keyData, role
}

// invalidateAll clears all cached entries. Called on pubsub cache.invalidate events.
func (c *apiKeyCache) invalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*cacheEntry)
}
