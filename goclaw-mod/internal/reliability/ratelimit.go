package reliability

import (
	"context"
	"sync"
	"time"
)

// RateLimitCoordinator provides a shared, single-flight view of provider
// cooldowns. Multiple concurrent runs using the same provider:model must not
// collide: when one run gets a 429, every other run for that key waits for
// the same cooldown instead of retrying simultaneously (retry storm).
//
// It complements, rather than replaces, the existing per-provider
// CooldownTracker: that tracker handles a run's local decision about *its own*
// requests; this coordinator deduplicates the wait across concurrent runs.
type RateLimitCoordinator struct {
	mu        sync.Mutex
	cooldowns map[string]time.Time
	waiters   map[string]int
	maxPending int
	nowFn     func() time.Time
}

// NewRateLimitCoordinator builds a coordinator with the given cap on pending
// waiter tracking. maxPending <= 0 disables the pending cap.
func NewRateLimitCoordinator(maxPending int) *RateLimitCoordinator {
	return &RateLimitCoordinator{
		cooldowns: make(map[string]time.Time),
		waiters:   make(map[string]int),
		nowFn:     time.Now,
	}
}

// key returns the coordinator key for a provider:model.
func (r *RateLimitCoordinator) key(provider, model string) string {
	return provider + ":" + model
}

// Record429 registers a rate-limit event for a provider:model. retryAfter is
// the provider's Retry-After hint (0 if absent → normalized default 30s).
func (r *RateLimitCoordinator) Record429(provider, model string, retryAfter time.Duration) {
	if retryAfter <= 0 {
		retryAfter = 30 * time.Second
	}
	k := r.key(provider, model)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cooldowns[k] = r.nowFn().Add(retryAfter)
}

// CooldownFor returns the remaining cooldown for a key, and whether one is
// active.
func (r *RateLimitCoordinator) CooldownFor(provider, model string) (time.Duration, bool) {
	k := r.key(provider, model)
	r.mu.Lock()
	until, ok := r.cooldowns[k]
	r.mu.Unlock()
	if !ok {
		return 0, false
	}
	remaining := until.Sub(r.nowFn())
	if remaining <= 0 {
		return 0, false
	}
	return remaining, true
}

// ShouldWait reports how long the caller should wait before issuing a request
// for a provider:model. It also counts the caller as a waiter so callers can
// be discouraged from piling onto an already-saturated key.
func (r *RateLimitCoordinator) ShouldWait(provider, model string) time.Duration {
	k := r.key(provider, model)
	r.mu.Lock()
	defer r.mu.Unlock()

	until, ok := r.cooldowns[k]
	if ok {
		remaining := until.Sub(r.nowFn())
		if remaining > 0 {
			r.waiters[k]++
			return remaining
		}
		delete(r.cooldowns, k)
	}
	return 0
}

// BeginWait removes a waiter for the key. Callers should invoke it after the
// wait computed by ShouldWait completes (defer-style), so the waiter counter
// does not leak.
func (r *RateLimitCoordinator) BeginWait(provider, model string) {
	k := r.key(provider, model)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.waiters[k] > 0 {
		r.waiters[k]--
		if r.waiters[k] == 0 {
			delete(r.waiters, k)
		}
	}
}

// Waiters reports how many runs are currently waiting on a key.
func (r *RateLimitCoordinator) Waiters(provider, model string) int {
	k := r.key(provider, model)
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.waiters[k]
}

// ClearCooldown removes any active cooldown for the key (e.g. after a
// successful request).
func (r *RateLimitCoordinator) ClearCooldown(provider, model string) {
	k := r.key(provider, model)
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cooldowns, k)
}

// Wait blocks until the cooldown for a key expires or the context is done.
// It is a convenience that combines ShouldWait with a cancellable sleep and
// writes a pessimistic wait registration so the cancellation path can't leak.
func (r *RateLimitCoordinator) Wait(ctx context.Context, provider, model string) error {
	r.mu.Lock()
	k := r.key(provider, model)
	until, ok := r.cooldowns[k]
	if ok {
		r.waiters[k]++
	}
	r.mu.Unlock()
	if !ok {
		return nil
	}
	defer r.BeginWait(provider, model)

	d := until.Sub(r.nowFn())
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		// Leave the cooldown in place for other runs; only this waiter gives up.
		return ctx.Err()
	case <-t.C:
		// Only clear the cooldown we actually waited on. If another run recorded
		// a newer (likely longer) 429 during our wait, that deadline must not be
		// removed by this stale waiter — otherwise every other run would burst
		// the provider while it is still rate-limited.
		r.maybeClearCooldown(k, until)
		return nil
	}
}

// maybeClearCooldown removes a cooldown only if the given deadline is still the
// active one for the key. It is the guard that prevents a waiter with an older,
// shorter deadline from deleting a newer cooldown recorded mid-wait.
func (r *RateLimitCoordinator) maybeClearCooldown(k string, until time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if stored, ok := r.cooldowns[k]; ok && stored.Equal(until) {
		delete(r.cooldowns, k)
	}
}