package personal

import (
	"strings"
	"sync"
	"time"
)

const (
	defaultDedupeTTL     = 5 * time.Minute
	defaultDedupeMaxKeys = 2048
)

// dedupeCache suppresses replays of the same inbound message after a listener
// reconnect. It keys on both msgId and cliMsgId because Zalo clients may send
// either one as the stable identity.
type dedupeCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	maxKeys int
	now     func() time.Time
	seen    map[string]time.Time
	order   []string
}

func newDedupeCache(ttl time.Duration, maxKeys int, now func() time.Time) *dedupeCache {
	if ttl <= 0 {
		ttl = defaultDedupeTTL
	}
	if maxKeys <= 0 {
		maxKeys = defaultDedupeMaxKeys
	}
	if now == nil {
		now = time.Now
	}
	return &dedupeCache{
		ttl:     ttl,
		maxKeys: maxKeys,
		now:     now,
		seen:    make(map[string]time.Time),
		order:   make([]string, 0, maxKeys),
	}
}

// Seen reports whether any non-empty key was already observed. On first sight
// it records every key with the same expiry, so a later frame carrying only a
// sibling identifier is still suppressed.
func (d *dedupeCache) Seen(keys ...string) bool {
	normalized := normalizeDedupeKeys(keys)
	if len(normalized) == 0 {
		return false
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	now := d.now()
	for _, key := range normalized {
		if expiry, ok := d.seen[key]; ok {
			if now.Before(expiry) {
				return true
			}
			delete(d.seen, key)
		}
	}

	d.evictExpiredLocked(now)
	for len(d.seen)+len(normalized) > d.maxKeys && len(d.order) > 0 {
		oldest := d.order[0]
		d.order = d.order[1:]
		delete(d.seen, oldest)
	}

	expiry := now.Add(d.ttl)
	for _, key := range normalized {
		if _, exists := d.seen[key]; exists {
			continue
		}
		d.seen[key] = expiry
		d.order = append(d.order, key)
	}
	return false
}

func (d *dedupeCache) evictExpiredLocked(now time.Time) {
	if len(d.order) == 0 {
		return
	}
	kept := d.order[:0]
	for _, key := range d.order {
		expiry, ok := d.seen[key]
		if !ok || !now.Before(expiry) {
			delete(d.seen, key)
			continue
		}
		kept = append(kept, key)
	}
	d.order = kept
}

func normalizeDedupeKeys(keys []string) []string {
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key != "" {
			out = append(out, key)
		}
	}
	return out
}
