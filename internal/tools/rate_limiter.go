package tools

import (
	"fmt"
	"sync"
	"time"
)

// ToolRateLimiter implements a sliding window rate limiter for tool executions.
// Tracks actions per key (typically agent:userID) within a configurable window.
type ToolRateLimiter struct {
	mu        sync.Mutex
	windows   map[string][]time.Time
	maxPerHr  int
	window    time.Duration
}

// NewToolRateLimiter creates a rate limiter with the given max actions per hour.
// Pass 0 to disable rate limiting.
func NewToolRateLimiter(maxPerHour int) *ToolRateLimiter {
	if maxPerHour <= 0 {
		return nil
	}
	return &ToolRateLimiter{
		windows:  make(map[string][]time.Time),
		maxPerHr: maxPerHour,
		window:   time.Hour,
	}
}

// Allow checks if a tool execution is allowed for the given key against the
// limiter's configured max. Returns nil if allowed, or an error.
func (rl *ToolRateLimiter) Allow(key string) error {
	return rl.AllowWithLimit(key, 0)
}

// AllowWithLimit is Allow with a per-call max override (calls/hour). When
// maxOverride > 0 it replaces the configured max for this check — used for the
// per-agent tools.rate_limit_per_hour. maxOverride <= 0 uses the configured max.
func (rl *ToolRateLimiter) AllowWithLimit(key string, maxOverride int) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	max := rl.maxPerHr
	if maxOverride > 0 {
		max = maxOverride
	}

	now := time.Now()
	cutoff := now.Add(-rl.window)

	// Prune expired entries
	entries := rl.windows[key]
	start := 0
	for start < len(entries) && entries[start].Before(cutoff) {
		start++
	}
	entries = entries[start:]

	if len(entries) >= max {
		return fmt.Errorf("tool rate limit exceeded: %d actions/hour for key %s", max, key)
	}

	// Record this action
	rl.windows[key] = append(entries, now)
	return nil
}

// Cleanup removes stale entries older than the window. Call periodically to prevent memory growth.
func (rl *ToolRateLimiter) Cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-rl.window)
	for key, entries := range rl.windows {
		start := 0
		for start < len(entries) && entries[start].Before(cutoff) {
			start++
		}
		if start == len(entries) {
			delete(rl.windows, key)
		} else {
			rl.windows[key] = entries[start:]
		}
	}
}
