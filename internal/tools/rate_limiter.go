package tools

import (
	"fmt"
	"sync"
	"time"
)

// ToolRateLimiter implements a sliding window rate limiter for tool executions.
// Tracks actions per key (typically agent:userID) within a configurable window.
type ToolRateLimiter struct {
	mu       sync.Mutex
	windows  map[string][]time.Time
	maxPerHr int
	window   time.Duration
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

// Allow checks if a tool execution is allowed for the given key.
// Returns nil if allowed, or an error describing the rate limit.
func (rl *ToolRateLimiter) Allow(key string) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	// Prune expired entries
	entries := rl.windows[key]
	start := 0
	for start < len(entries) && entries[start].Before(cutoff) {
		start++
	}
	entries = entries[start:]

	if len(entries) >= rl.maxPerHr {
		return fmt.Errorf("tool rate limit exceeded: %d actions/hour for key %s", rl.maxPerHr, key)
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
