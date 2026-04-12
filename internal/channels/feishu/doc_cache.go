package feishu

import (
	"container/list"
	"sync"
	"time"
)

// docCacheEntry holds one cached document body plus its insertion time for
// TTL-based expiry. We store a *list.Element alongside so that Get can promote
// the entry to the front in constant time.
type docCacheEntry struct {
	key       string
	content   string
	expiresAt time.Time
}

// docCache is a small LRU+TTL cache keyed by Lark document ID. Stores the
// already-truncated doc content as plain strings so retrieval is a map lookup
// with no allocation on the hot path. Safe for concurrent use.
//
// Scope is intentionally per-channel instance (not shared across tenants) so
// one tenant cannot spy on another tenant's doc cache hits via timing.
type docCache struct {
	mu       sync.Mutex
	items    map[string]*list.Element
	order    *list.List // front = most recently used
	capacity int
	ttl      time.Duration
}

// newDocCache returns an LRU cache with the given max entry count and TTL.
// Size is enforced on Set; TTL is enforced lazily on Get.
func newDocCache(capacity int, ttl time.Duration) *docCache {
	if capacity <= 0 {
		capacity = 1
	}
	return &docCache{
		items:    make(map[string]*list.Element, capacity),
		order:    list.New(),
		capacity: capacity,
		ttl:      ttl,
	}
}

// Get returns the cached content and true on hit, or ("", false) on miss or
// TTL expiry. A successful hit promotes the entry to the most-recently-used
// position.
func (c *docCache) Get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.items[key]
	if !ok {
		return "", false
	}
	entry := elem.Value.(*docCacheEntry)
	if time.Now().After(entry.expiresAt) {
		c.order.Remove(elem)
		delete(c.items, key)
		return "", false
	}
	c.order.MoveToFront(elem)
	return entry.content, true
}

// Set inserts or updates the cache entry for key. Updating an existing key
// refreshes both its value and its position. Eviction of the LRU entry runs
// when capacity is exceeded.
func (c *docCache) Set(key, content string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		entry := elem.Value.(*docCacheEntry)
		entry.content = content
		entry.expiresAt = time.Now().Add(c.ttl)
		c.order.MoveToFront(elem)
		return
	}
	entry := &docCacheEntry{
		key:       key,
		content:   content,
		expiresAt: time.Now().Add(c.ttl),
	}
	elem := c.order.PushFront(entry)
	c.items[key] = elem

	for c.order.Len() > c.capacity {
		lru := c.order.Back()
		if lru == nil {
			return
		}
		c.order.Remove(lru)
		delete(c.items, lru.Value.(*docCacheEntry).key)
	}
}
