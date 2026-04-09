package gateway

import (
	"testing"
	"time"
)

func TestRateLimiter_Disabled(t *testing.T) {
	rl := NewRateLimiter(0, 5)
	if rl.Enabled() {
		t.Error("rate limiter with rpm=0 should be disabled")
	}
	for range 100 {
		if !rl.Allow("any-key") {
			t.Fatal("disabled limiter should always allow")
		}
	}
}

func TestRateLimiter_Enabled(t *testing.T) {
	rl := NewRateLimiter(60, 5)
	if !rl.Enabled() {
		t.Error("rate limiter with rpm=60 should be enabled")
	}
}

func TestRateLimiter_BurstAllowed(t *testing.T) {
	rl := NewRateLimiter(60, 3)
	for i := range 3 {
		if !rl.Allow("user-1") {
			t.Fatalf("request %d should be allowed within burst", i+1)
		}
	}
}

func TestRateLimiter_ExceedBurstBlocked(t *testing.T) {
	rl := NewRateLimiter(60, 2)
	rl.Allow("user-1")
	rl.Allow("user-1")
	if rl.Allow("user-1") {
		t.Error("request exceeding burst should be blocked")
	}
}

func TestRateLimiter_PerKeyIsolation(t *testing.T) {
	rl := NewRateLimiter(60, 1)
	rl.Allow("user-1")
	if rl.Allow("user-1") {
		t.Error("user-1 should be rate limited")
	}
	if !rl.Allow("user-2") {
		t.Error("user-2 should not be affected by user-1's rate limit")
	}
}

func TestRateLimiter_DefaultBurst_VerifiedViaBehavior(t *testing.T) {
	rl := NewRateLimiter(60, 0) // burst=0 should default to 5
	// Verify default burst allows 5 requests
	for i := range 5 {
		if !rl.Allow("user") {
			t.Fatalf("request %d should be allowed (default burst)", i+1)
		}
	}
	if rl.Allow("user") {
		t.Error("6th request should be blocked (default burst = 5)")
	}
}

func TestRateLimiter_NegativeBurst_VerifiedViaBehavior(t *testing.T) {
	rl := NewRateLimiter(60, -1) // negative burst should default to 5
	for i := range 5 {
		if !rl.Allow("user") {
			t.Fatalf("request %d should be allowed (negative burst defaults to 5)", i+1)
		}
	}
	if rl.Allow("user") {
		t.Error("6th request should be blocked")
	}
}

func TestRateLimiter_Cleanup_StaleRemoved(t *testing.T) {
	rl := NewRateLimiter(60, 5)
	rl.Allow("stale-key")
	rl.Allow("fresh-key")

	// Simulate stale entry by manipulating lastSeen via the sync.Map.
	// This is intentionally testing the cleanup contract: entries inactive
	// for >10 minutes are removed.
	if v, ok := rl.limiters.Load("stale-key"); ok {
		v.(*limiterEntry).lastSeen = time.Now().Add(-20 * time.Minute)
	}

	rl.cleanup()

	// stale should be gone, fresh should remain
	if _, ok := rl.limiters.Load("stale-key"); ok {
		t.Error("stale entry should have been cleaned up")
	}
	if _, ok := rl.limiters.Load("fresh-key"); !ok {
		t.Error("fresh entry should NOT be cleaned up")
	}
}
