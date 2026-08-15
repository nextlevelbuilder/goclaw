package reliability

import (
	"context"
	"testing"
	"time"
)

// TestWaitReturnsImmediatelyWithoutCooldown: no cooldown → Wait is a no-op.
func TestWaitReturnsImmediatelyWithoutCooldown(t *testing.T) {
	now, _ := fakeClock(t)
	r := NewRateLimitCoordinator(0)
	r.nowFn = now
	if err := r.Wait(context.Background(), "pv", "m"); err != nil {
		t.Fatalf("Wait without cooldown should return nil, got %v", err)
	}
}

// TestWaitReturnsImmediatelyWhenExpired: if the recorded deadline is already in
// the past when Wait is called, it must not block.
func TestWaitReturnsImmediatelyWhenExpired(t *testing.T) {
	now, _ := fakeClock(t)
	r := NewRateLimitCoordinator(0)
	r.nowFn = now
	// Manually plant an already-expired cooldown.
	r.mu.Lock()
	r.cooldowns[r.key("pv", "m")] = now().Add(-time.Second)
	r.mu.Unlock()

	if err := r.Wait(context.Background(), "pv", "m"); err != nil {
		t.Fatalf("Wait on expired cooldown should return nil, got %v", err)
	}
}

// TestMaybeClearCooldown only clears the exact deadline it waited on — the
// critical fix. A waiter with an OLD, shorter deadline must never remove a
// NEWER, longer cooldown recorded mid-wait, or it would release every other
// run onto a provider that is still rate-limited.
func TestMaybeClearCooldownMatch(t *testing.T) {
	now, _ := fakeClock(t)
	r := NewRateLimitCoordinator(0)
	r.nowFn = now
	r.Record429("pv", "m", 10*time.Second)
	k := r.key("pv", "m")

	r.mu.Lock()
	stored := r.cooldowns[k]
	r.mu.Unlock()

	r.maybeClearCooldown(k, stored)
	if _, ok := r.CooldownFor("pv", "m"); ok {
		t.Errorf("matching deadline should clear the cooldown")
	}
}

func TestMaybeClearCooldownStaleDoesNotClearNewer(t *testing.T) {
	now, _ := fakeClock(t)
	r := NewRateLimitCoordinator(0)
	r.nowFn = now
	k := r.key("pv", "m")

	r.Record429("pv", "m", 10*time.Second) // old waiter's deadline
	r.mu.Lock()
	old := r.cooldowns[k]
	r.mu.Unlock()

	r.Record429("pv", "m", 30*time.Second) // newer, longer cooldown mid-wait

	r.maybeClearCooldown(k, old) // stale waiter fires with the OLD deadline
	if d, ok := r.CooldownFor("pv", "m"); !ok || d <= 0 {
		t.Fatalf("stale waiter cleared the newer cooldown (ok=%v d=%v)", ok, d)
	}
}

// TestWaitCancellationLeavesCooldown: a cancelled waiter gives up (before its
// real timer ever fires) and must leave the cooldown in place for others.
func TestWaitCancellationLeavesCooldown(t *testing.T) {
	now, _ := fakeClock(t)
	r := NewRateLimitCoordinator(0)
	r.nowFn = now
	r.Record429("pv", "m", 20*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Wait(ctx, "pv", "m") }()
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("cancelled Wait should return a non-nil error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled Wait did not return")
	}
	if _, ok := r.CooldownFor("pv", "m"); !ok {
		t.Errorf("cooldown should remain after a cancelled waiter gives up")
	}
}
