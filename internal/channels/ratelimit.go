package channels

import (
	"sync"
	"time"
)

const (
	// maxTrackedKeys caps the number of tracked rate-limit keys to prevent
	// memory exhaustion from attackers rotating source IPs/keys.
	maxTrackedKeys = 4096

	// rateLimitWindow is the sliding window duration for rate counting.
	rateLimitWindow = 60 * time.Second

	// rateLimitMaxHits is the max requests per key within a window.
	rateLimitMaxHits = 30
)

type rateLimitEntry struct {
	windowStart time.Time
	count       int
}

// WebhookRateLimiter bounds the number of tracked rate-limit keys
// to prevent memory exhaustion from rotating source keys (DoS).
// Safe for concurrent use.
type WebhookRateLimiter struct {
	mu      sync.Mutex
	entries map[string]*rateLimitEntry
}

// NewWebhookRateLimiter creates a bounded webhook rate limiter.
func NewWebhookRateLimiter() *WebhookRateLimiter {
	return &WebhookRateLimiter{entries: make(map[string]*rateLimitEntry)}
}

// Allow returns true if the key is within rate limits.
// Automatically prunes stale entries and enforces a hard cap on tracked keys.
func (r *WebhookRateLimiter) Allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()

	// Prune stale entries when approaching the cap
	if len(r.entries) >= maxTrackedKeys {
		for k, e := range r.entries {
			if now.Sub(e.windowStart) >= rateLimitWindow {
				delete(r.entries, k)
			}
		}
		// Hard eviction if still at cap (FIFO-ish via map iteration)
		for len(r.entries) >= maxTrackedKeys {
			for k := range r.entries {
				delete(r.entries, k)
				break
			}
		}
	}

	e, ok := r.entries[key]
	if !ok || now.Sub(e.windowStart) >= rateLimitWindow {
		r.entries[key] = &rateLimitEntry{windowStart: now, count: 1}
		return true
	}

	e.count++
	return e.count <= rateLimitMaxHits
}
