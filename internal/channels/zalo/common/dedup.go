package common

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// Dedup is a TTL cache of (instanceID, messageID) pairs with global and
// per-instance caps. Eviction on cap-hit removes the oldest entry — not
// strict LRU, since access doesn't refresh ordering. The per-instance cap
// prevents a single noisy tenant from monopolizing the global slot count.
type Dedup struct {
	mu             sync.Mutex
	ttl            time.Duration
	maxGlobal      int
	maxPerInstance int
	now            func() time.Time

	entries map[string]dedupEntry
	perInst map[uuid.UUID]int
}

type dedupEntry struct {
	addedAt    time.Time
	instanceID uuid.UUID
}

// NewDedup returns a Dedup with TTL and global cap. Per-instance cap is
// derived as max(maxGlobal/4, 1) so tenants can't starve each other.
func NewDedup(ttl time.Duration, maxGlobal int) *Dedup {
	perInst := maxGlobal / 4
	if perInst < 1 {
		perInst = 1
	}
	return &Dedup{
		ttl:            ttl,
		maxGlobal:      maxGlobal,
		maxPerInstance: perInst,
		now:            time.Now,
		entries:        make(map[string]dedupEntry),
		perInst:        make(map[uuid.UUID]int),
	}
}

// SeenOrAdd records the (instanceID, messageID) pair and reports whether
// it was already seen within TTL. Empty messageID is not-seen and not recorded.
func (d *Dedup) SeenOrAdd(instanceID uuid.UUID, messageID string) bool {
	if messageID == "" {
		return false
	}
	key := instanceID.String() + "|" + messageID

	d.mu.Lock()
	defer d.mu.Unlock()

	now := d.now()
	if e, ok := d.entries[key]; ok && now.Sub(e.addedAt) < d.ttl {
		return true
	}

	// Sweep only at-cap; TTL check above prevents stale false-positives meanwhile.
	if len(d.entries) >= d.maxGlobal || d.perInst[instanceID] >= d.maxPerInstance {
		d.evictExpired(now)
		if d.perInst[instanceID] >= d.maxPerInstance {
			d.evictOldestForInstance(instanceID)
		}
		if len(d.entries) >= d.maxGlobal {
			d.evictOldestGlobal()
		}
	}

	if _, exists := d.entries[key]; !exists {
		d.perInst[instanceID]++
	}
	d.entries[key] = dedupEntry{addedAt: now, instanceID: instanceID}
	return false
}

// Len reports the current entry count (live + not-yet-pruned).
func (d *Dedup) Len() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.entries)
}

func (d *Dedup) evictExpired(now time.Time) {
	for k, e := range d.entries {
		if now.Sub(e.addedAt) >= d.ttl {
			d.deleteKey(k)
		}
	}
}

func (d *Dedup) evictOldestGlobal() {
	var oldestKey string
	var oldestTime time.Time
	first := true
	for k, e := range d.entries {
		if first || e.addedAt.Before(oldestTime) {
			oldestKey = k
			oldestTime = e.addedAt
			first = false
		}
	}
	if !first {
		d.deleteKey(oldestKey)
	}
}

func (d *Dedup) evictOldestForInstance(id uuid.UUID) {
	var oldestKey string
	var oldestTime time.Time
	first := true
	for k, e := range d.entries {
		if e.instanceID != id {
			continue
		}
		if first || e.addedAt.Before(oldestTime) {
			oldestKey = k
			oldestTime = e.addedAt
			first = false
		}
	}
	if !first {
		d.deleteKey(oldestKey)
	}
}

func (d *Dedup) deleteKey(k string) {
	e, ok := d.entries[k]
	if !ok {
		return
	}
	delete(d.entries, k)
	if d.perInst[e.instanceID] > 0 {
		d.perInst[e.instanceID]--
		if d.perInst[e.instanceID] == 0 {
			delete(d.perInst, e.instanceID)
		}
	}
}
