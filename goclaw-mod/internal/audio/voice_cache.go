package audio

import (
	"container/list"
	"sync"
	"time"

	"github.com/google/uuid"
)

// VoiceCache is a thread-safe in-memory cache of TTS voices keyed by tenant
// UUID. Eviction policy: TTL (entries expire after a fixed duration) combined
// with LRU cap (oldest-accessed entry evicted when cap is exceeded). Both
// policies apply simultaneously — whichever fires first removes the entry.
//
// Multi-instance note: each gateway process maintains its own cache. Redis
// invalidation is deferred until scale-out is on the roadmap (P2-H1).
type VoiceCache struct {
	mu         sync.Mutex
	entries    map[uuid.UUID]*list.Element
	lru        *list.List // front = most recently used
	ttl        time.Duration
	maxTenants int
}

type voiceCacheEntry struct {
	tenantID  uuid.UUID
	voices    []Voice
	expiresAt time.Time
}

// NewVoiceCache creates a cache with the given TTL and LRU cap.
// ttl=0 means entries never expire; maxTenants=0 disables the LRU cap.
func NewVoiceCache(ttl time.Duration, maxTenants int) *VoiceCache {
	return &VoiceCache{
		entries:    make(map[uuid.UUID]*list.Element),
		lru:        list.New(),
		ttl:        ttl,
		maxTenants: maxTenants,
	}
}

// Get returns the cached voices for tenantID. Returns (nil, false) on miss
// (absent or TTL-expired). Moves the entry to the front of the LRU list on hit.
func (c *VoiceCache) Get(tenantID uuid.UUID) ([]Voice, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.entries[tenantID]
	if !ok {
		return nil, false
	}
	entry := el.Value.(*voiceCacheEntry)
	if c.ttl > 0 && time.Now().After(entry.expiresAt) {
		c.lru.Remove(el)
		delete(c.entries, tenantID)
		return nil, false
	}
	c.lru.MoveToFront(el)
	return entry.voices, true
}

// Set stores voices for tenantID. Refreshes TTL and LRU position when the
// tenant already has a cached entry. Evicts the LRU entry when at cap.
func (c *VoiceCache) Set(tenantID uuid.UUID, voices []Voice) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.entries[tenantID]; ok {
		entry := el.Value.(*voiceCacheEntry)
		entry.voices = voices
		if c.ttl > 0 {
			entry.expiresAt = time.Now().Add(c.ttl)
		}
		c.lru.MoveToFront(el)
		return
	}

	if c.maxTenants > 0 && c.lru.Len() >= c.maxTenants {
		oldest := c.lru.Back()
		if oldest != nil {
			evicted := oldest.Value.(*voiceCacheEntry)
			c.lru.Remove(oldest)
			delete(c.entries, evicted.tenantID)
		}
	}

	var exp time.Time
	if c.ttl > 0 {
		exp = time.Now().Add(c.ttl)
	}
	entry := &voiceCacheEntry{
		tenantID:  tenantID,
		voices:    voices,
		expiresAt: exp,
	}
	el := c.lru.PushFront(entry)
	c.entries[tenantID] = el
}

// Invalidate removes the cache entry for tenantID. No-op if absent.
// Called by POST /v1/voices/refresh to force a live refetch.
func (c *VoiceCache) Invalidate(tenantID uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.entries[tenantID]; ok {
		c.lru.Remove(el)
		delete(c.entries, tenantID)
	}
}
