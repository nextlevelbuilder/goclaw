package gateway

import (
	"testing"
)

// TestRateLimiter_DisabledWhenZeroRPM verifies that rpm=0 means always-allow.
func TestRateLimiter_DisabledWhenZeroRPM(t *testing.T) {
	rl := NewRateLimiter(0, 5)
	if rl.Enabled() {
		t.Fatal("expected disabled when rpm=0")
	}
	for i := 0; i < 100; i++ {
		if !rl.Allow("key") {
			t.Fatal("expected allow when disabled")
		}
	}
}

// TestRateLimiter_DisabledWhenNegativeRPM verifies that rpm<0 means always-allow.
func TestRateLimiter_DisabledWhenNegativeRPM(t *testing.T) {
	rl := NewRateLimiter(-1, 5)
	if rl.Enabled() {
		t.Fatal("expected disabled when rpm<0")
	}
	if !rl.Allow("key") {
		t.Fatal("expected allow when disabled")
	}
}

// TestRateLimiter_EnabledWhenPositiveRPM verifies Enabled() returns true for rpm>0.
func TestRateLimiter_EnabledWhenPositiveRPM(t *testing.T) {
	rl := NewRateLimiter(60, 5)
	if !rl.Enabled() {
		t.Fatal("expected enabled when rpm>0")
	}
}

// TestRateLimiter_BurstAllowed verifies that up to burst requests pass immediately.
func TestRateLimiter_BurstAllowed(t *testing.T) {
	burst := 3
	// 1 RPM so refill is negligible during burst; burst=3 so first 3 should pass.
	rl := NewRateLimiter(1, burst)
	key := "test-burst"
	allowed := 0
	for i := 0; i < burst; i++ {
		if rl.Allow(key) {
			allowed++
		}
	}
	if allowed != burst {
		t.Errorf("expected %d burst-allowed requests, got %d", burst, allowed)
	}
}

// TestRateLimiter_BlocksAfterBurst verifies that requests beyond burst are blocked.
func TestRateLimiter_BlocksAfterBurst(t *testing.T) {
	// 1 RPM, burst=2 → 2 tokens available, 3rd request in same instant is blocked.
	rl := NewRateLimiter(1, 2)
	key := "test-block"
	// Drain the burst
	rl.Allow(key)
	rl.Allow(key)
	// Next request should be rate-limited
	if rl.Allow(key) {
		t.Error("expected request to be rate-limited after burst exhausted")
	}
}

// TestRateLimiter_PerKeyIsolation verifies that different keys have independent limiters.
func TestRateLimiter_PerKeyIsolation(t *testing.T) {
	// Very small RPM with burst=1 so each key gets exactly 1 request before blocking.
	rl := NewRateLimiter(1, 1)
	key1, key2 := "user1", "user2"

	// Both keys should pass their first request.
	if !rl.Allow(key1) {
		t.Error("key1 first request should be allowed")
	}
	if !rl.Allow(key2) {
		t.Error("key2 first request should be allowed (independent from key1)")
	}

	// Both should now be blocked.
	if rl.Allow(key1) {
		t.Error("key1 second request should be rate-limited")
	}
	if rl.Allow(key2) {
		t.Error("key2 second request should be rate-limited")
	}
}

// TestRateLimiter_DefaultBurstWhenZero verifies that burst<=0 defaults to 5.
func TestRateLimiter_DefaultBurstWhenZero(t *testing.T) {
	// burst=0 → default to 5; rpm=600 so refill is fast but we test burst size
	rl := NewRateLimiter(600, 0)
	key := "burst-default"
	// Should be able to consume at least 5 tokens from the default burst.
	allowed := 0
	for i := 0; i < 5; i++ {
		if rl.Allow(key) {
			allowed++
		}
	}
	if allowed < 5 {
		t.Errorf("expected at least 5 burst requests with default burst, got %d", allowed)
	}
}
