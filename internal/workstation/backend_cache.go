package workstation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// cachedBackend holds a Backend with its last-used timestamp.
type cachedBackend struct {
	backend  Backend
	lastUsed time.Time
}

// BackendCache is a TTL-based in-memory cache of Backend instances keyed by workstation UUID.
// On cache miss it opens a new Backend via the registered factory (workstation.Open).
// Invalidate(id) must be called on workstation update/delete to evict stale entries.
// sync.Mutex (not RWMutex) is used because lastUsed is mutated on every read-path hit,
// making an RWMutex unsafe — writes under RLock cause a data race.
type BackendCache struct {
	wsStore store.WorkstationStore
	cache   map[uuid.UUID]*cachedBackend
	ttl     time.Duration
	mu      sync.Mutex
}

// NewBackendCache creates a BackendCache with the given TTL.
// A TTL of 10 minutes is recommended for production use.
func NewBackendCache(wsStore store.WorkstationStore, ttl time.Duration) *BackendCache {
	return &BackendCache{
		wsStore: wsStore,
		cache:   make(map[uuid.UUID]*cachedBackend),
		ttl:     ttl,
	}
}

// Get returns a cached Backend for wsID, or opens a new one via Open() on miss.
// Thread-safe. Uses a full Mutex (not RWMutex) because lastUsed is updated on cache hit,
// and mutating a field under RLock is a data race.
func (c *BackendCache) Get(ctx context.Context, wsID uuid.UUID) (Backend, error) {
	// Fast path: lock for cache hit and lastUsed update.
	c.mu.Lock()
	if cb, ok := c.cache[wsID]; ok && time.Since(cb.lastUsed) < c.ttl {
		cb.lastUsed = time.Now()
		b := cb.backend
		c.mu.Unlock()
		return b, nil
	}
	c.mu.Unlock()

	// Slow path: fetch from store and open backend.
	ws, err := c.wsStore.GetByID(ctx, wsID)
	if err != nil {
		return nil, fmt.Errorf("workstation lookup: %w", err)
	}
	if !ws.Active {
		return nil, fmt.Errorf("workstation inactive: %s", wsID)
	}
	b, err := Open(ws)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	// Double-check: another goroutine may have populated the entry while we held no lock.
	if cb, ok := c.cache[wsID]; ok && time.Since(cb.lastUsed) < c.ttl {
		// Lost the race — close our backend to stop its background goroutine.
		_ = b.Close()
		return cb.backend, nil
	}
	c.cache[wsID] = &cachedBackend{backend: b, lastUsed: time.Now()}
	return b, nil
}

// Invalidate evicts the cache entry for wsID.
// Should be called when a workstation is updated or deleted.
func (c *BackendCache) Invalidate(wsID uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.cache, wsID)
}

// InvalidateAll clears the entire cache.
func (c *BackendCache) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = make(map[uuid.UUID]*cachedBackend)
}
