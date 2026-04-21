package bitrix24

import (
	"container/list"
	"sync"
	"time"
)

// dedupCache is a bounded LRU with per-entry TTL.
//
// Used by the webhook router to guarantee at-most-once event delivery on top
// of Bitrix24's at-least-once retry behaviour. Bitrix retries non-2xx up to
// three times; without dedup every retry would spawn a fresh agent pipeline
// run and double-bill tokens.
//
// Thread-safe. All public methods take an internal mutex.
//
// Eviction policy:
//   - Once `size > maxSize` the oldest entry is evicted immediately on insert.
//   - A background sweeper also removes entries whose TTL has elapsed; the
//     sweeper is optional — if ttl==0 entries never expire on time, only on
//     LRU pressure.
type dedupCache struct {
	mu      sync.Mutex
	seen    map[string]*list.Element
	order   *list.List
	maxSize int
	ttl     time.Duration

	now     func() time.Time
	stopCh  chan struct{}
	stopped bool
}

type dedupEntry struct {
	key     string
	addedAt time.Time
}

// newDedupCache builds a cache with the given capacity and TTL.
// ttl<=0 disables time-based eviction; entries are only purged when the
// cache fills up beyond maxSize.
func newDedupCache(maxSize int, ttl time.Duration) *dedupCache {
	if maxSize <= 0 {
		maxSize = 1
	}
	return &dedupCache{
		seen:    make(map[string]*list.Element, maxSize),
		order:   list.New(),
		maxSize: maxSize,
		ttl:     ttl,
		now:     time.Now,
		stopCh:  make(chan struct{}),
	}
}

// Seen returns true iff the key has already been observed inside its TTL.
// On the first sighting (or if the prior sighting aged out) it records the
// key and returns false.
//
// The semantics are "test-and-set": callers don't need to call a separate
// Mark method after a miss; Seen does it atomically under the lock.
func (d *dedupCache) Seen(key string) bool {
	if key == "" {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	now := d.now()

	if el, ok := d.seen[key]; ok {
		ent := el.Value.(*dedupEntry)
		// TTL-aged entry: treat as unseen and refresh in place.
		if d.ttl > 0 && now.Sub(ent.addedAt) >= d.ttl {
			ent.addedAt = now
			d.order.MoveToFront(el)
			return false
		}
		// Move to front to extend LRU residency.
		d.order.MoveToFront(el)
		return true
	}

	el := d.order.PushFront(&dedupEntry{key: key, addedAt: now})
	d.seen[key] = el

	// Enforce capacity after insert.
	for d.order.Len() > d.maxSize {
		oldest := d.order.Back()
		if oldest == nil {
			break
		}
		d.order.Remove(oldest)
		delete(d.seen, oldest.Value.(*dedupEntry).key)
	}
	return false
}

// Len returns the current number of entries. Test helper.
func (d *dedupCache) Len() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.order.Len()
}

// sweepExpired walks the oldest entries and removes anything past TTL.
// Called from the background goroutine — noop if ttl<=0.
func (d *dedupCache) sweepExpired() {
	if d.ttl <= 0 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	for {
		el := d.order.Back()
		if el == nil {
			return
		}
		ent := el.Value.(*dedupEntry)
		if now.Sub(ent.addedAt) < d.ttl {
			return
		}
		d.order.Remove(el)
		delete(d.seen, ent.key)
	}
}

// StartSweeper launches a goroutine that runs sweepExpired at interval.
// Safe to call once; subsequent calls are no-ops. Stop via close(cache.stopCh)
// or by calling Stop().
func (d *dedupCache) StartSweeper(interval time.Duration) {
	if d.ttl <= 0 || interval <= 0 {
		return
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-d.stopCh:
				return
			case <-t.C:
				d.sweepExpired()
			}
		}
	}()
}

// Stop terminates the background sweeper. Idempotent.
func (d *dedupCache) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped {
		return
	}
	d.stopped = true
	close(d.stopCh)
}
