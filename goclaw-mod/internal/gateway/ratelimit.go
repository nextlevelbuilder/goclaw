// Package gateway — per-user rate limiter for WebSocket and HTTP endpoints.
package gateway

import (
	"log/slog"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiter enforces per-key (user/IP) request rate limits using token bucket.
type RateLimiter struct {
	limiters sync.Map   // key → *limiterEntry
	r        rate.Limit // refill rate (requests per second)
	burst    int        // max burst size
}

type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewRateLimiter creates a rate limiter.
// rpm is requests per minute, burst is the max burst allowed.
// If rpm <= 0, the rate limiter is effectively disabled (always allows).
func NewRateLimiter(rpm, burst int) *RateLimiter {
	if burst <= 0 {
		burst = 5
	}
	r := rate.Limit(0)
	if rpm > 0 {
		r = rate.Limit(float64(rpm) / 60.0)
	}
	rl := &RateLimiter{r: r, burst: burst}

	// Periodic cleanup of stale entries (every 5 minutes)
	go rl.cleanupLoop()

	return rl
}

// Allow checks if a request from the given key is allowed.
// Returns true if allowed, false if rate limited.
func (rl *RateLimiter) Allow(key string) bool {
	if rl.r == 0 {
		return true // disabled
	}
	entry := rl.getOrCreate(key)
	if !entry.limiter.Allow() {
		slog.Warn("security.rate_limited", "key", key)
		return false
	}
	entry.lastSeen = time.Now()
	return true
}

// Enabled returns true if the rate limiter is active.
func (rl *RateLimiter) Enabled() bool {
	return rl.r > 0
}

func (rl *RateLimiter) getOrCreate(key string) *limiterEntry {
	if v, ok := rl.limiters.Load(key); ok {
		return v.(*limiterEntry)
	}
	entry := &limiterEntry{
		limiter:  rate.NewLimiter(rl.r, rl.burst),
		lastSeen: time.Now(),
	}
	actual, _ := rl.limiters.LoadOrStore(key, entry)
	return actual.(*limiterEntry)
}

func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		rl.cleanup()
	}
}

func (rl *RateLimiter) cleanup() {
	cutoff := time.Now().Add(-10 * time.Minute)
	rl.limiters.Range(func(key, value any) bool {
		entry := value.(*limiterEntry)
		if entry.lastSeen.Before(cutoff) {
			rl.limiters.Delete(key)
		}
		return true
	})
}
