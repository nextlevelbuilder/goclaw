package providers

import (
	"sync"
	"time"
)

// CooldownTracker tracks per-provider:model failure state with decay and probe intervals.
// Thread-safe. In-memory only — state does not survive restart.
type CooldownTracker struct {
	mu          sync.Mutex
	entries     map[string]*cooldownEntry
	maxKeys     int
	lastCleanup time.Time             // amortize TTL cleanup
	nowFn       func() time.Time      // for testing; defaults to time.Now
}

type cooldownEntry struct {
	reason         FailoverReason
	cooldownUntil  time.Time
	lastProbe      time.Time
	failureCount   int
	overloadStreak int // consecutive overloaded failures (resets on different reason)
	createdAt      time.Time
}

// Cooldown durations by failure reason.
var cooldownDurations = map[FailoverReason]time.Duration{
	FailoverRateLimit:     30 * time.Second,
	FailoverOverloaded:    60 * time.Second,
	FailoverBilling:       5 * time.Minute,
	FailoverAuth:          10 * time.Minute,
	FailoverAuthPermanent: 1 * time.Hour,
	FailoverTimeout:       15 * time.Second,
	FailoverModelNotFound: 1 * time.Hour,
	FailoverFormat:        5 * time.Minute,
	FailoverUnknown:       30 * time.Second,
}

const (
	minProbeInterval      = 30 * time.Second
	stateTTL              = 24 * time.Hour
	overloadEscalationCap = 5 // after 5 consecutive overloaded failures, double cooldown
	defaultMaxKeys        = 512
)

// NewCooldownTracker creates a tracker with a max key limit.
func NewCooldownTracker(maxKeys int) *CooldownTracker {
	if maxKeys <= 0 {
		maxKeys = defaultMaxKeys
	}
	return &CooldownTracker{
		entries: make(map[string]*cooldownEntry),
		maxKeys: maxKeys,
		nowFn:   time.Now,
	}
}

// CooldownKey builds a cooldown lookup key from provider and model.
func CooldownKey(provider, model string) string {
	return provider + ":" + model
}

// RecordFailure records a provider error and enters cooldown with reason-appropriate duration.
func (t *CooldownTracker) RecordFailure(key string, reason FailoverReason) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.nowFn()
	// Amortize TTL cleanup: only scan every 5 minutes to avoid O(n) on every call
	if now.Sub(t.lastCleanup) > 5*time.Minute {
		t.cleanupLocked(now)
		t.lastCleanup = now
	}

	entry, exists := t.entries[key]
	if !exists {
		if len(t.entries) >= t.maxKeys {
			t.evictOldest()
		}
		entry = &cooldownEntry{createdAt: now}
		t.entries[key] = entry
	}

	// Track consecutive overload streak (resets on different reason)
	if reason == FailoverOverloaded {
		entry.overloadStreak++
	} else {
		entry.overloadStreak = 0
	}
	entry.reason = reason
	entry.failureCount++

	duration := cooldownDurations[reason]
	if duration == 0 {
		duration = 30 * time.Second
	}

	// Overload escalation: flat 2x cooldown after cap consecutive overloaded failures.
	// Intentionally flat (not exponential) to avoid overly long cooldowns.
	if reason == FailoverOverloaded && entry.overloadStreak > overloadEscalationCap {
		duration *= 2
	}

	entry.cooldownUntil = now.Add(duration)
}

// IsAvailable returns true if the key is not in active cooldown.
func (t *CooldownTracker) IsAvailable(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry, exists := t.entries[key]
	if !exists {
		return true
	}
	return t.nowFn().After(entry.cooldownUntil)
}

// ShouldProbe returns true if a probe request is allowed during cooldown.
// Atomically updates lastProbe so only one caller per interval gets true.
func (t *CooldownTracker) ShouldProbe(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry, exists := t.entries[key]
	if !exists {
		return false // not in cooldown
	}

	now := t.nowFn()
	if now.After(entry.cooldownUntil) {
		return false // cooldown expired
	}

	if now.Sub(entry.lastProbe) >= minProbeInterval {
		entry.lastProbe = now
		return true
	}
	return false
}

// RecordSuccess clears cooldown immediately for a key.
func (t *CooldownTracker) RecordSuccess(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.entries, key)
}

// cleanupLocked removes entries older than stateTTL. Must hold mu.
func (t *CooldownTracker) cleanupLocked(now time.Time) {
	for key, entry := range t.entries {
		if now.Sub(entry.createdAt) > stateTTL {
			delete(t.entries, key)
		}
	}
}

// evictOldest removes the oldest entry by createdAt. Must hold mu.
func (t *CooldownTracker) evictOldest() {
	var oldestKey string
	var oldestTime time.Time
	first := true
	for key, entry := range t.entries {
		if first || entry.createdAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.createdAt
			first = false
		}
	}
	if oldestKey != "" {
		delete(t.entries, oldestKey)
	}
}
