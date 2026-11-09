package personal

import (
	"strings"
	"sync"
	"time"
)

const (
	defaultDedupeTTL     = 10 * time.Minute
	defaultDedupeMaxKeys = 10000
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

	if len(d.seen) >= d.maxKeys {
		d.evictExpiredLocked(now)
	}
	// Still full of live entries: drop arbitrary oldest entries rather than
	// growing without bound. Map iteration order is random, which is fine for
	// a best-effort replay guard.
	for key := range d.seen {
		if len(d.seen) < d.maxKeys {
			break
		}
		delete(d.seen, key)
	}

	expiry := now.Add(d.ttl)
	for _, key := range normalized {
		d.seen[key] = expiry
	}
	return false
}

func (d *dedupeCache) evictExpiredLocked(now time.Time) {
	for key, expiry := range d.seen {
		if !now.Before(expiry) {
			delete(d.seen, key)
		}
	}
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
