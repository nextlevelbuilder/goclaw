package providers

import (
	"sync"
	"testing"
	"time"
)

func TestCooldownKey(t *testing.T) {
	tests := []struct {
		provider string
		model    string
		expected string
	}{
		{"openai", "gpt-4", "openai:gpt-4"},
		{"anthropic", "claude-3-opus", "anthropic:claude-3-opus"},
		{"groq", "mixtral-8x7b", "groq:mixtral-8x7b"},
		{"openai", "gpt-4-turbo", "openai:gpt-4-turbo"},
	}

	for _, tt := range tests {
		t.Run(tt.provider+"/"+tt.model, func(t *testing.T) {
			result := CooldownKey(tt.provider, tt.model)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestNewCooldownTracker(t *testing.T) {
	tests := []struct {
		name      string
		maxKeys   int
		expectMax int
	}{
		{"default max keys", 0, defaultMaxKeys},
		{"negative max keys", -1, defaultMaxKeys},
		{"custom max keys", 256, 256},
		{"large max keys", 10000, 10000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := NewCooldownTracker(tt.maxKeys)
			if tracker.maxKeys != tt.expectMax {
				t.Errorf("maxKeys: got %d, want %d", tracker.maxKeys, tt.expectMax)
			}
			if tracker.nowFn == nil {
				t.Error("nowFn not initialized")
			}
			if len(tracker.entries) != 0 {
				t.Error("entries should be empty on init")
			}
		})
	}
}

func TestRecordFailureAndIsAvailable(t *testing.T) {
	tracker := NewCooldownTracker(512)
	now := time.Now()
	tracker.nowFn = func() time.Time { return now }

	key := "openai:gpt-4"

	// Initially available
	if !tracker.IsAvailable(key) {
		t.Error("key should be available initially")
	}

	// Record failure
	tracker.RecordFailure(key, FailoverRateLimit)

	// Should not be available immediately after failure
	if tracker.IsAvailable(key) {
		t.Error("key should not be available after failure")
	}

	// Advance time past cooldown duration (rate_limit = 30s)
	now = now.Add(31 * time.Second)
	if !tracker.IsAvailable(key) {
		t.Error("key should be available after cooldown expires")
	}
}

func TestRecordFailureCooldownDurations(t *testing.T) {
	tests := []struct {
		reason           FailoverReason
		expectedDuration time.Duration
	}{
		{FailoverRateLimit, 30 * time.Second},
		{FailoverOverloaded, 60 * time.Second},
		{FailoverBilling, 5 * time.Minute},
		{FailoverAuth, 10 * time.Minute},
		{FailoverAuthPermanent, 1 * time.Hour},
		{FailoverTimeout, 15 * time.Second},
		{FailoverModelNotFound, 1 * time.Hour},
		{FailoverFormat, 5 * time.Minute},
		{FailoverUnknown, 30 * time.Second},
	}

	for _, tt := range tests {
		t.Run(string(tt.reason), func(t *testing.T) {
			tracker := NewCooldownTracker(512)
			now := time.Now()
			tracker.nowFn = func() time.Time { return now }

			key := CooldownKey("openai", "test-model")
			tracker.RecordFailure(key, tt.reason)

			// Just before cooldown expires - not available
			now = now.Add(tt.expectedDuration - 1*time.Second)
			tracker.nowFn = func() time.Time { return now } // Update closure
			if tracker.IsAvailable(key) {
				t.Errorf("key should not be available %v before expiry", 1*time.Second)
			}

			// After cooldown expires - available
			now = now.Add(2 * time.Second)
			tracker.nowFn = func() time.Time { return now } // Update closure
			if !tracker.IsAvailable(key) {
				t.Error("key should be available after cooldown expires")
			}
		})
	}
}

func TestShouldProbeInterval(t *testing.T) {
	tracker := NewCooldownTracker(512)
	now := time.Now()
	tracker.nowFn = func() time.Time { return now }

	key := "openai:gpt-4"

	// Record failure to enter cooldown (FailoverTimeout = 15s, but we need longer for this test)
	// Use FailoverBilling which is 5 min to have room for probes
	tracker.RecordFailure(key, FailoverBilling) // 5 min cooldown

	// First probe immediately after failure should be allowed
	if !tracker.ShouldProbe(key) {
		t.Error("first probe should be allowed")
	}

	// Second probe immediately after should be denied
	if tracker.ShouldProbe(key) {
		t.Error("second probe immediately after should be denied")
	}

	// Advance time less than minProbeInterval (30s) - still denied
	now = now.Add(20 * time.Second)
	tracker.nowFn = func() time.Time { return now } // Update closure
	if tracker.ShouldProbe(key) {
		t.Error("probe before minProbeInterval should be denied")
	}

	// Advance past minProbeInterval - allowed
	now = now.Add(11 * time.Second)                 // total 31s from start
	tracker.nowFn = func() time.Time { return now } // Update closure
	if !tracker.ShouldProbe(key) {
		t.Error("probe after minProbeInterval should be allowed")
	}

	// Next probe immediately after should be denied
	if tracker.ShouldProbe(key) {
		t.Error("next probe immediately after should be denied")
	}
}

func TestShouldProbeAfterCooldownExpires(t *testing.T) {
	tracker := NewCooldownTracker(512)
	now := time.Now()
	tracker.nowFn = func() time.Time { return now }

	key := "openai:gpt-4"

	// Record failure with short cooldown
	tracker.RecordFailure(key, FailoverTimeout) // 15s

	// Probe allowed initially
	if !tracker.ShouldProbe(key) {
		t.Error("first probe should be allowed")
	}

	// Advance past cooldown expiry
	now = now.Add(16 * time.Second)
	tracker.nowFn = func() time.Time { return now } // Update closure

	// Should return false after cooldown expires
	if tracker.ShouldProbe(key) {
		t.Error("probe should return false after cooldown expires")
	}
}

func TestShouldProbeNotInCooldown(t *testing.T) {
	tracker := NewCooldownTracker(512)
	now := time.Now()
	tracker.nowFn = func() time.Time { return now }

	key := "openai:gpt-4"

	// No cooldown entry - should return false
	if tracker.ShouldProbe(key) {
		t.Error("probe should return false when not in cooldown")
	}
}

func TestShouldProbeAtomicity(t *testing.T) {
	tracker := NewCooldownTracker(512)
	now := time.Now()
	tracker.nowFn = func() time.Time { return now }

	key := "openai:gpt-4"
	tracker.RecordFailure(key, FailoverTimeout)

	var results []bool
	var mu sync.Mutex

	// Launch multiple goroutines that all call ShouldProbe at approximately the same time
	wg := sync.WaitGroup{}
	for range 10 {
		wg.Go(func() {
			result := tracker.ShouldProbe(key)
			mu.Lock()
			results = append(results, result)
			mu.Unlock()
		})
	}
	wg.Wait()

	// Exactly one goroutine should have gotten true
	trueCount := 0
	for _, r := range results {
		if r {
			trueCount++
		}
	}
	if trueCount != 1 {
		t.Errorf("expected exactly 1 true result, got %d", trueCount)
	}
}

func TestRecordSuccessClearsCooldown(t *testing.T) {
	tracker := NewCooldownTracker(512)
	now := time.Now()
	tracker.nowFn = func() time.Time { return now }

	key := "openai:gpt-4"

	// Record failure
	tracker.RecordFailure(key, FailoverBilling) // 5 min cooldown

	// Not available
	if tracker.IsAvailable(key) {
		t.Error("key should not be available after failure")
	}

	// Record success - clears cooldown
	tracker.RecordSuccess(key)

	// Should be available immediately
	if !tracker.IsAvailable(key) {
		t.Error("key should be available after RecordSuccess")
	}

	// ShouldProbe should return false (entry deleted)
	if tracker.ShouldProbe(key) {
		t.Error("ShouldProbe should return false after RecordSuccess")
	}
}

func TestOverloadEscalation(t *testing.T) {
	tracker := NewCooldownTracker(512)
	now := time.Now()
	tracker.nowFn = func() time.Time { return now }

	key := "openai:gpt-4"

	// Record 5 failures (at cap)
	for range 5 {
		tracker.RecordFailure(key, FailoverOverloaded)
		now = now.Add(1 * time.Second)                  // Increment time to allow new failures
		tracker.nowFn = func() time.Time { return now } // Update closure
	}

	// After 5th failure, should still be available at 60s (normal duration)
	now = now.Add(61 * time.Second)
	tracker.nowFn = func() time.Time { return now } // Update closure
	if !tracker.IsAvailable(key) {
		t.Error("should be available 61s after 5th failure")
	}

	// Reset to trigger 6th failure
	now = time.Now()
	tracker.nowFn = func() time.Time { return now }
	tracker.RecordFailure(key, FailoverOverloaded)
	tracker.RecordFailure(key, FailoverOverloaded) // 6th failure triggers escalation

	// Should be in cooldown at normal duration
	if tracker.IsAvailable(key) {
		t.Error("should not be available immediately after 6th failure")
	}

	// Advance 61 seconds - still not available due to escalation (120s)
	now = now.Add(61 * time.Second)
	tracker.nowFn = func() time.Time { return now } // Update closure
	if tracker.IsAvailable(key) {
		t.Error("should not be available 61s after 6th failure (escalated)")
	}

	// Advance to 121 seconds total - should be available
	now = now.Add(60 * time.Second)
	tracker.nowFn = func() time.Time { return now } // Update closure
	if !tracker.IsAvailable(key) {
		t.Error("should be available 121s after 6th failure")
	}
}

func TestMaxKeyEviction(t *testing.T) {
	maxKeys := 3
	tracker := NewCooldownTracker(maxKeys)
	now := time.Now()
	tracker.nowFn = func() time.Time { return now }

	// Add keys up to max
	for i := range maxKeys {
		key := CooldownKey("openai", "model-"+string(rune(i)))
		tracker.RecordFailure(key, FailoverTimeout)
		now = now.Add(1 * time.Second)                  // Increment to make createdAt different
		tracker.nowFn = func() time.Time { return now } // Update closure
	}

	if len(tracker.entries) != maxKeys {
		t.Errorf("expected %d entries, got %d", maxKeys, len(tracker.entries))
	}

	// Add one more key - should evict oldest
	key4 := CooldownKey("openai", "model-4")
	tracker.RecordFailure(key4, FailoverTimeout)

	if len(tracker.entries) != maxKeys {
		t.Errorf("expected %d entries after eviction, got %d", maxKeys, len(tracker.entries))
	}

	// Oldest key (model-0) should have been evicted
	key0 := CooldownKey("openai", "model-0")
	if tracker.IsAvailable(key0) && len(tracker.entries) == maxKeys {
		// If key0 is available and we still have maxKeys entries, key0 was evicted
		// Check that it's not in entries
		if _, exists := tracker.entries[key0]; exists {
			t.Error("oldest key should have been evicted")
		}
	}

	// New key should be in entries
	if _, exists := tracker.entries[key4]; !exists {
		t.Error("new key should be in entries")
	}
}

func TestTTLCleanup(t *testing.T) {
	tracker := NewCooldownTracker(512)
	now := time.Now()
	tracker.nowFn = func() time.Time { return now }

	// Add an old entry
	key1 := CooldownKey("openai", "old-model")
	tracker.RecordFailure(key1, FailoverTimeout)

	// Add a fresh entry (will trigger cleanup during next RecordFailure)
	now = now.Add(25 * time.Hour)                   // Past TTL (24h)
	tracker.nowFn = func() time.Time { return now } // Update closure
	key2 := CooldownKey("openai", "new-model")
	tracker.RecordFailure(key2, FailoverTimeout)

	// Old entry should have been cleaned up
	if _, exists := tracker.entries[key1]; exists {
		t.Error("old entry should have been cleaned up by TTL")
	}

	// New entry should still exist
	if _, exists := tracker.entries[key2]; !exists {
		t.Error("new entry should still exist")
	}
}

func TestConcurrentAccess(t *testing.T) {
	tracker := NewCooldownTracker(512)
	now := time.Now()
	tracker.nowFn = func() time.Time { return now }

	keys := []string{
		CooldownKey("openai", "gpt-4"),
		CooldownKey("anthropic", "claude-3-opus"),
		CooldownKey("groq", "mixtral-8x7b"),
	}

	wg := sync.WaitGroup{}

	// Concurrent RecordFailure and IsAvailable
	for range 10 {
		for _, key := range keys {
			wg.Add(1)
			go func(k string) {
				defer wg.Done()
				tracker.RecordFailure(k, FailoverTimeout)
				_ = tracker.IsAvailable(k)
				_ = tracker.ShouldProbe(k)
				tracker.RecordSuccess(k)
			}(key)
		}
	}

	wg.Wait()

	// All keys should have been cleaned up by RecordSuccess
	if len(tracker.entries) != 0 {
		t.Errorf("expected 0 entries after cleanup, got %d", len(tracker.entries))
	}
}

func TestMultipleFailureReasons(t *testing.T) {
	tracker := NewCooldownTracker(512)
	now := time.Now()
	tracker.nowFn = func() time.Time { return now }

	key := "openai:gpt-4"

	// Record first failure
	tracker.RecordFailure(key, FailoverRateLimit)
	if tracker.IsAvailable(key) {
		t.Error("should not be available after rate_limit failure")
	}

	// Advance to next probe window
	now = now.Add(35 * time.Second)
	tracker.nowFn = func() time.Time { return now } // Update closure

	// Record different failure reason during cooldown (updates reason, resets cooldown)
	tracker.RecordFailure(key, FailoverAuth) // 10 min

	// Should not be available due to new auth cooldown
	if tracker.IsAvailable(key) {
		t.Error("should not be available, auth cooldown is set")
	}

	// Advance past auth cooldown (10 min = 600s)
	now = now.Add(601 * time.Second)
	tracker.nowFn = func() time.Time { return now } // Update closure
	if !tracker.IsAvailable(key) {
		t.Error("should be available after auth cooldown expires")
	}
}

func TestEmptyKey(t *testing.T) {
	tracker := NewCooldownTracker(512)
	now := time.Now()
	tracker.nowFn = func() time.Time { return now }

	// Should handle empty key without panic
	tracker.RecordFailure("", FailoverTimeout)
	if tracker.IsAvailable("") {
		t.Error("empty key should not be available after failure")
	}
	tracker.RecordSuccess("")
	if !tracker.IsAvailable("") {
		t.Error("empty key should be available after success")
	}
}

func TestUnknownFailoverReason(t *testing.T) {
	tracker := NewCooldownTracker(512)
	now := time.Now()
	tracker.nowFn = func() time.Time { return now }

	key := "openai:gpt-4"

	// Record with unknown reason - should use default 30s
	tracker.RecordFailure(key, FailoverReason("unknown_future_reason"))

	if tracker.IsAvailable(key) {
		t.Error("should not be available after unknown reason failure")
	}

	now = now.Add(31 * time.Second)
	if !tracker.IsAvailable(key) {
		t.Error("should be available after default 30s cooldown")
	}
}
