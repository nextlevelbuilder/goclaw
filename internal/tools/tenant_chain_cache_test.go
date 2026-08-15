package tools

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestTenantChainCache_TTLExpiry verifies that cached entries expire after TTL.
// Uses injected clock to avoid time.Sleep.
func TestTenantChainCache_TTLExpiry(t *testing.T) {
	c := newTenantChainCache()

	// Replace the cache's time function with a fake clock.
	fakeNow := time.Unix(1000, 0)
	c.now = func() time.Time { return fakeNow }

	tid := uuid.New()
	chain := []SearchProvider{&fakeSearchProvider{"brave"}}

	// Set chain at t=1000
	c.Set(tid, chain)

	// Should be present at t=1000
	got, ok := c.Get(tid)
	if !ok || len(got) != 1 {
		t.Error("expected cache hit at t=0")
	}

	// Move time forward 30 seconds (still within 60s TTL)
	fakeNow = time.Unix(1030, 0)
	got, ok = c.Get(tid)
	if !ok || len(got) != 1 {
		t.Error("expected cache hit at t+30s")
	}

	// Move time forward to 61 seconds (exceeds TTL)
	fakeNow = time.Unix(1061, 0)
	_, ok = c.Get(tid)
	if ok {
		t.Error("expected cache miss after TTL expiry")
	}
}

// TestTenantChainCache_Invalidate verifies single-tenant invalidation.
func TestTenantChainCache_Invalidate(t *testing.T) {
	c := newTenantChainCache()
	c.now = func() time.Time { return time.Now() }

	tid1, tid2 := uuid.New(), uuid.New()
	chain1 := []SearchProvider{&fakeSearchProvider{"brave"}}
	chain2 := []SearchProvider{&fakeSearchProvider{"exa"}}

	c.Set(tid1, chain1)
	c.Set(tid2, chain2)

	// Verify both cached
	if _, ok := c.Get(tid1); !ok {
		t.Error("tid1 should be cached")
	}
	if _, ok := c.Get(tid2); !ok {
		t.Error("tid2 should be cached")
	}

	// Invalidate only tid1
	c.Invalidate(tid1)

	// tid1 should be gone, tid2 should remain
	if _, ok := c.Get(tid1); ok {
		t.Error("tid1 should be evicted")
	}
	if _, ok := c.Get(tid2); !ok {
		t.Error("tid2 should still be cached")
	}
}

// TestTenantChainCache_InvalidateAll verifies full-cache invalidation.
func TestTenantChainCache_InvalidateAll(t *testing.T) {
	c := newTenantChainCache()
	c.now = func() time.Time { return time.Now() }

	tid1, tid2, tid3 := uuid.New(), uuid.New(), uuid.New()
	for _, tid := range []uuid.UUID{tid1, tid2, tid3} {
		c.Set(tid, []SearchProvider{&fakeSearchProvider{"test"}})
	}

	// Verify all cached
	for _, tid := range []uuid.UUID{tid1, tid2, tid3} {
		if _, ok := c.Get(tid); !ok {
			t.Errorf("expected %v cached", tid)
		}
	}

	// Invalidate all
	c.InvalidateAll()

	// All should be gone
	for _, tid := range []uuid.UUID{tid1, tid2, tid3} {
		if _, ok := c.Get(tid); ok {
			t.Errorf("expected %v evicted", tid)
		}
	}
}

// TestTenantChainCache_ConcurrentReaders verifies race-safe reads.
func TestTenantChainCache_ConcurrentReaders(t *testing.T) {
	c := newTenantChainCache()
	c.now = func() time.Time { return time.Now() }

	tid := uuid.New()
	chain := []SearchProvider{&fakeSearchProvider{"brave"}, &fakeSearchProvider{"exa"}}
	c.Set(tid, chain)

	// Spawn 10 concurrent readers
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			for range 100 {
				got, ok := c.Get(tid)
				if !ok || len(got) != 2 {
					t.Errorf("concurrent read failed")
				}
			}
		})
	}

	wg.Wait()
}

// TestTenantChainCache_ConcurrentMutations verifies race-safe writes.
func TestTenantChainCache_ConcurrentMutations(t *testing.T) {
	c := newTenantChainCache()
	c.now = func() time.Time { return time.Now() }

	// Spawn concurrent writers for different tenants
	var wg sync.WaitGroup
	for i := range 5 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tid := uuid.New()
			for j := range 10 {
				c.Set(tid, []SearchProvider{&fakeSearchProvider{"brave"}})
				c.Get(tid)
				if j%3 == 0 {
					c.Invalidate(tid)
				}
			}
		}(i)
	}

	wg.Wait()
}

// TestTenantChainCache_SameSliceOnBackToBackCalls verifies idempotent reads.
func TestTenantChainCache_SameSliceOnBackToBackCalls(t *testing.T) {
	c := newTenantChainCache()
	c.now = func() time.Time { return time.Now() }

	tid := uuid.New()
	originalChain := []SearchProvider{&fakeSearchProvider{"brave"}}
	c.Set(tid, originalChain)

	// Back-to-back calls should return identical slice
	chain1, ok1 := c.Get(tid)
	chain2, ok2 := c.Get(tid)

	if !ok1 || !ok2 {
		t.Fatal("cache hits failed")
	}

	if len(chain1) != len(chain2) || len(chain1) != 1 {
		t.Errorf("chain lengths differ: %d vs %d", len(chain1), len(chain2))
	}

	if chain1[0].Name() != chain2[0].Name() {
		t.Errorf("provider names differ: %s vs %s", chain1[0].Name(), chain2[0].Name())
	}
}

// TestTenantChainCache_InvalidateAfterRead verifies cache refresh on invalidation.
func TestTenantChainCache_InvalidateAfterRead(t *testing.T) {
	c := newTenantChainCache()
	c.now = func() time.Time { return time.Now() }

	tid := uuid.New()

	// Set chain v1
	c.Set(tid, []SearchProvider{&fakeSearchProvider{"brave"}})
	got1, ok1 := c.Get(tid)
	if !ok1 {
		t.Fatal("initial read failed")
	}
	if got1[0].Name() != "brave" {
		t.Errorf("expected brave, got %s", got1[0].Name())
	}

	// Invalidate and set new chain v2
	c.Invalidate(tid)
	c.Set(tid, []SearchProvider{&fakeSearchProvider{"exa"}})
	got2, ok2 := c.Get(tid)
	if !ok2 {
		t.Fatal("read after invalidate failed")
	}
	if got2[0].Name() != "exa" {
		t.Errorf("expected exa, got %s", got2[0].Name())
	}
}
