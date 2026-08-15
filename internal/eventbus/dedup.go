package eventbus

import (
	"sync"
	"time"
)

// dedupSet tracks recently-seen SourceIDs to prevent duplicate event processing.
// Thread-safe. Entries expire after TTL.
type dedupSet struct {
	mu   sync.Mutex
	seen map[string]time.Time // sourceID -> expiry
	ttl  time.Duration
	stop chan struct{}
}

func newDedupSet(ttl time.Duration) *dedupSet {
	d := &dedupSet{
		seen: make(map[string]time.Time),
		ttl:  ttl,
		stop: make(chan struct{}),
	}
	go d.cleanup()
	return d
}

// Add returns true if sourceID is new (not seen), false if duplicate.
// Empty sourceID always returns true (no dedup).
func (d *dedupSet) Add(sourceID string) bool {
	if sourceID == "" {
		return true
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.seen[sourceID]; exists {
		return false
	}
	d.seen[sourceID] = time.Now().Add(d.ttl)
	return true
}

// cleanup sweeps expired entries periodically.
func (d *dedupSet) cleanup() {
	ticker := time.NewTicker(d.ttl / 2)
	defer ticker.Stop()
	for {
		select {
		case <-d.stop:
			return
		case now := <-ticker.C:
			d.mu.Lock()
			for k, expiry := range d.seen {
				if now.After(expiry) {
					delete(d.seen, k)
				}
			}
			d.mu.Unlock()
		}
	}
}

// Close stops the cleanup goroutine.
func (d *dedupSet) Close() {
	close(d.stop)
}
