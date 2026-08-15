package pg

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const contactResolveCacheTTL = 60 * time.Second

// contactResolveEntry holds a cached tenant-user resolution result.
type contactResolveEntry struct {
	tenantUserID string // empty = not merged
	fetched      time.Time
}

// contactResolveCache is a TTL cache for contact→tenant-user resolution.
// Mirrors the pattern in config_permissions.go (permCacheTTL).
type contactResolveCache struct {
	mu    sync.RWMutex
	items map[string]contactResolveEntry // key: "tenantID:channelType:senderID"
}

func newContactResolveCache() *contactResolveCache {
	return &contactResolveCache{items: make(map[string]contactResolveEntry)}
}

func (c *contactResolveCache) get(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if entry, ok := c.items[key]; ok && time.Since(entry.fetched) < contactResolveCacheTTL {
		return entry.tenantUserID, true
	}
	return "", false
}

func (c *contactResolveCache) set(key, tenantUserID string) {
	c.mu.Lock()
	c.items[key] = contactResolveEntry{tenantUserID: tenantUserID, fetched: time.Now()}
	c.mu.Unlock()
}

// InvalidateContactResolveCache clears all cached contact→tenant-user resolutions.
// Call after merge/unmerge operations.
func (s *PGContactStore) InvalidateContactResolveCache() {
	if s.resolveCache == nil {
		return
	}
	s.resolveCache.mu.Lock()
	s.resolveCache.items = make(map[string]contactResolveEntry)
	s.resolveCache.mu.Unlock()
}

// ResolveTenantUserID looks up a contact's merged tenant-user identity.
// Uses an in-memory cache with 60s TTL to avoid per-message DB queries.
func (s *PGContactStore) ResolveTenantUserID(ctx context.Context, channelType, senderID string) (string, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return "", nil
	}
	cacheKey := tid.String() + ":" + channelType + ":" + senderID

	// Check cache.
	if s.resolveCache != nil {
		if resolved, ok := s.resolveCache.get(cacheKey); ok {
			return resolved, nil
		}
	}

	// Query DB: join channel_contacts → tenant_users via merged_id.
	var tenantUserID string
	err := s.db.QueryRowContext(ctx,
		`SELECT tu.user_id FROM channel_contacts cc
		 JOIN tenant_users tu ON cc.merged_id = tu.id
		 WHERE cc.tenant_id = $1 AND cc.channel_type = $2 AND cc.sender_id = $3
		 AND cc.merged_id IS NOT NULL`,
		tid, channelType, senderID,
	).Scan(&tenantUserID)

	if errors.Is(err, sql.ErrNoRows) {
		// Not merged — cache the negative result too.
		if s.resolveCache != nil {
			s.resolveCache.set(cacheKey, "")
		}
		return "", nil
	}
	if err != nil {
		return "", err
	}

	// Cache positive result.
	if s.resolveCache != nil {
		s.resolveCache.set(cacheKey, tenantUserID)
	}
	return tenantUserID, nil
}
