package tools

import (
	"testing"
	"time"
)

func TestNewToolRateLimiter_Zero(t *testing.T) {
	rl := NewToolRateLimiter(0)
	if rl != nil {
		t.Errorf("expected nil for maxPerHour=0, got %v", rl)
	}
}

func TestNewToolRateLimiter_Negative(t *testing.T) {
	rl := NewToolRateLimiter(-5)
	if rl != nil {
		t.Errorf("expected nil for maxPerHour=-5, got %v", rl)
	}
}

func TestToolRateLimiter_AllowUnderLimit(t *testing.T) {
	rl := NewToolRateLimiter(5)
	for i := range 5 {
		if err := rl.Allow("user1"); err != nil {
			t.Errorf("action %d should be allowed: %v", i, err)
		}
	}
}

func TestToolRateLimiter_BlockOverLimit(t *testing.T) {
	rl := NewToolRateLimiter(3)

	for i := range 3 {
		if err := rl.Allow("user1"); err != nil {
			t.Fatalf("action %d should be allowed: %v", i, err)
		}
	}

	err := rl.Allow("user1")
	if err == nil {
		t.Error("4th action should be blocked")
	}
}

func TestToolRateLimiter_SeparateKeys(t *testing.T) {
	rl := NewToolRateLimiter(2)

	// Fill user1
	rl.Allow("user1")
	rl.Allow("user1")

	// user1 is blocked
	if err := rl.Allow("user1"); err == nil {
		t.Error("user1 should be blocked")
	}

	// user2 is independent
	if err := rl.Allow("user2"); err != nil {
		t.Errorf("user2 should be allowed: %v", err)
	}
}

func TestToolRateLimiter_AllowWithLimit_Override(t *testing.T) {
	rl := NewToolRateLimiter(100) // global default 100

	// A per-agent override of 2 caps this key at 2, regardless of the global 100.
	if err := rl.AllowWithLimit("agentA", 2); err != nil {
		t.Fatalf("call 1 should be allowed: %v", err)
	}
	if err := rl.AllowWithLimit("agentA", 2); err != nil {
		t.Fatalf("call 2 should be allowed: %v", err)
	}
	if err := rl.AllowWithLimit("agentA", 2); err == nil {
		t.Error("call 3 should be blocked by the override of 2")
	}

	// maxOverride <= 0 falls back to the configured global (100), on its own key.
	for i := range 3 {
		if err := rl.AllowWithLimit("agentB", 0); err != nil {
			t.Fatalf("agentB call %d should use global 100: %v", i, err)
		}
	}
}

func TestToolRateLimiter_WindowExpiry(t *testing.T) {
	rl := &ToolRateLimiter{
		windows:  make(map[string][]time.Time),
		maxPerHr: 2,
		window:   100 * time.Millisecond, // short window for testing
	}

	// Fill the window
	rl.Allow("key1")
	rl.Allow("key1")

	if err := rl.Allow("key1"); err == nil {
		t.Error("should be blocked at limit")
	}

	// Wait for window to expire
	time.Sleep(150 * time.Millisecond)

	if err := rl.Allow("key1"); err != nil {
		t.Errorf("should be allowed after window expiry: %v", err)
	}
}

func TestToolRateLimiter_Cleanup(t *testing.T) {
	rl := &ToolRateLimiter{
		windows:  make(map[string][]time.Time),
		maxPerHr: 10,
		window:   50 * time.Millisecond,
	}

	rl.Allow("key1")
	rl.Allow("key2")

	time.Sleep(100 * time.Millisecond)
	rl.Cleanup()

	rl.mu.Lock()
	count := len(rl.windows)
	rl.mu.Unlock()

	if count != 0 {
		t.Errorf("cleanup should remove all expired entries, got %d", count)
	}
}

func TestToolRateLimiter_CleanupPartial(t *testing.T) {
	rl := &ToolRateLimiter{
		windows:  make(map[string][]time.Time),
		maxPerHr: 10,
		window:   200 * time.Millisecond,
	}

	rl.Allow("key1") // will expire
	time.Sleep(100 * time.Millisecond)
	rl.Allow("key1") // still fresh

	// Only the first entry should be pruned, not the whole key
	time.Sleep(150 * time.Millisecond)
	rl.Cleanup()

	rl.mu.Lock()
	entries := len(rl.windows["key1"])
	rl.mu.Unlock()

	if entries != 1 {
		t.Errorf("expected 1 remaining entry, got %d", entries)
	}
}
