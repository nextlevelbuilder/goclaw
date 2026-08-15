package bus

import (
	"sync"
	"time"
)

// DedupeCache is a TTL-based deduplication cache for inbound messages.
// Matching TS src/infra/dedupe.ts createDedupeCache().
//
// check() returns true if the key has been seen before (duplicate).
// Entries expire after TTL and are pruned lazily on each check.
type DedupeCache struct {
	mu      sync.Mutex
	entries map[string]int64 // key → unix millis
	ttl     time.Duration
	maxSize int
}

// NewDedupeCache creates a new dedup cache.
// Matching TS defaults: ttl=20min, maxSize=5000.
func NewDedupeCache(ttl time.Duration, maxSize int) *DedupeCache {
	return &DedupeCache{
		entries: make(map[string]int64, 256),
		ttl:     ttl,
		maxSize: maxSize,
	}
}

// IsDuplicate returns true if key was already seen within the TTL window.
// If not a duplicate, records the key for future checks.
func (d *DedupeCache) IsDuplicate(key string) bool {
	now := time.Now().UnixMilli()
	cutoff := now - d.ttl.Milliseconds()

	d.mu.Lock()
	defer d.mu.Unlock()

	// Check if key exists and is still valid
	if ts, ok := d.entries[key]; ok && ts >= cutoff {
		return true
	}

	// Prune expired entries
	d.cleanup(cutoff)

	// Record this key
	d.entries[key] = now
	return false
}

// cleanup removes expired entries and evicts oldest if over maxSize.
// Must be called with d.mu held.
func (d *DedupeCache) cleanup(cutoff int64) {
	// Remove expired
	for k, ts := range d.entries {
		if ts < cutoff {
			delete(d.entries, k)
		}
	}

	// Evict oldest if still over max (map iteration is random, but sufficient)
	if d.maxSize > 0 && len(d.entries) >= d.maxSize {
		excess := len(d.entries) - d.maxSize + 1
		for k := range d.entries {
			if excess <= 0 {
				break
			}
			delete(d.entries, k)
			excess--
		}
	}
}
