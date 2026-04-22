package cache

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryCache_GetSet(t *testing.T) {
	c := NewInMemoryCache[string]()
	ctx := context.Background()

	c.Set(ctx, "hello", "world", 0)
	val, ok := c.Get(ctx, "hello")
	if !ok {
		t.Fatal("expected key to exist")
	}
	if val != "world" {
		t.Fatalf("expected 'world', got %q", val)
	}
}

func TestInMemoryCache_TTLExpiry(t *testing.T) {
	c := NewInMemoryCache[int]()
	ctx := context.Background()

	c.Set(ctx, "key", 42, 20*time.Millisecond)

	// Should be present immediately
	val, ok := c.Get(ctx, "key")
	if !ok || val != 42 {
		t.Fatal("expected key to be present before TTL expiry")
	}

	time.Sleep(40 * time.Millisecond)

	_, ok = c.Get(ctx, "key")
	if ok {
		t.Fatal("expected key to be expired after TTL")
	}
}

func TestInMemoryCache_Delete(t *testing.T) {
	c := NewInMemoryCache[string]()
	ctx := context.Background()

	c.Set(ctx, "k", "v", 0)
	c.Delete(ctx, "k")

	_, ok := c.Get(ctx, "k")
	if ok {
		t.Fatal("expected key to be deleted")
	}
}

func TestInMemoryCache_DeleteByPrefix(t *testing.T) {
	c := NewInMemoryCache[string]()
	ctx := context.Background()

	c.Set(ctx, "agent:1:files", "a", 0)
	c.Set(ctx, "agent:1:meta", "b", 0)
	c.Set(ctx, "agent:2:files", "c", 0)

	c.DeleteByPrefix(ctx, "agent:1:")

	if _, ok := c.Get(ctx, "agent:1:files"); ok {
		t.Error("agent:1:files should be deleted")
	}
	if _, ok := c.Get(ctx, "agent:1:meta"); ok {
		t.Error("agent:1:meta should be deleted")
	}
	if _, ok := c.Get(ctx, "agent:2:files"); !ok {
		t.Error("agent:2:files should still exist")
	}
}

func TestInMemoryCache_Clear(t *testing.T) {
	c := NewInMemoryCache[int]()
	ctx := context.Background()

	c.Set(ctx, "a", 1, 0)
	c.Set(ctx, "b", 2, 0)
	c.Set(ctx, "c", 3, 0)

	c.Clear(ctx)

	for _, k := range []string{"a", "b", "c"} {
		if _, ok := c.Get(ctx, k); ok {
			t.Errorf("key %q should be cleared", k)
		}
	}
}

// TestInMemoryCache_PeriodicSweep verifies expired entries are removed by
// the background sweep goroutine (not just lazy on Get).
func TestInMemoryCache_PeriodicSweep(t *testing.T) {
	c := NewInMemoryCache[string](
		WithSweepInterval[string](20 * time.Millisecond),
	)
	defer c.Close()
	ctx := context.Background()

	c.Set(ctx, "short", "v1", 10*time.Millisecond)
	c.Set(ctx, "long", "v2", 0) // no expiry

	// Wait for sweep to run at least once after short entry expires
	time.Sleep(60 * time.Millisecond)

	// Short entry should be removed by sweep even without Get
	if count := c.sizeLocked(); count != 1 {
		t.Errorf("expected 1 entry after sweep, got %d", count)
	}
	if _, ok := c.Get(ctx, "long"); !ok {
		t.Error("long-lived entry should still exist")
	}
}

// TestInMemoryCache_MaxSizeEviction verifies oldest entries are evicted when
// max size cap is reached.
func TestInMemoryCache_MaxSizeEviction(t *testing.T) {
	c := NewInMemoryCache[int](
		WithMaxSize[int](5),
		WithSweepInterval[int](10*time.Millisecond),
	)
	defer c.Close()
	ctx := context.Background()

	// Insert 10 entries with distinct creation times to ensure oldest-first ordering
	for i := range 10 {
		c.Set(ctx, string(rune('a'+i)), i, 0)
		time.Sleep(2 * time.Millisecond)
	}

	// Trigger sweep by waiting for interval
	time.Sleep(30 * time.Millisecond)

	if count := c.sizeLocked(); count > 5 {
		t.Errorf("expected size ≤ 5 after max-size eviction, got %d", count)
	}
}

// TestInMemoryCache_Close verifies Close stops the sweep goroutine (no leak).
func TestInMemoryCache_Close(t *testing.T) {
	c := NewInMemoryCache[string](
		WithSweepInterval[string](10 * time.Millisecond),
	)
	ctx := context.Background()
	c.Set(ctx, "k", "v", 0)

	c.Close()
	// Close should be idempotent
	c.Close()

	// After Close, cache is still readable for lazy access but sweep is stopped
	if _, ok := c.Get(ctx, "k"); !ok {
		t.Error("Get should still work after Close")
	}
}

// TestInMemoryCache_ConcurrentSweepAndSet verifies no race between sweep and Set.
func TestInMemoryCache_ConcurrentSweepAndSet(t *testing.T) {
	c := NewInMemoryCache[int](
		WithSweepInterval[int](1*time.Millisecond),
		WithMaxSize[int](100),
	)
	defer c.Close()
	ctx := context.Background()

	done := make(chan bool)
	go func() {
		for i := range 500 {
			c.Set(ctx, string(rune('a'+(i%26))), i, 5*time.Millisecond)
		}
		done <- true
	}()
	go func() {
		for i := range 500 {
			_, _ = c.Get(ctx, string(rune('a'+(i%26))))
		}
		done <- true
	}()
	<-done
	<-done
}

// TestInMemoryCache_BackwardCompatZeroArg verifies existing call sites with
// zero-arg constructor still work (variadic options).
func TestInMemoryCache_BackwardCompatZeroArg(t *testing.T) {
	c := NewInMemoryCache[string]()
	defer c.Close()
	ctx := context.Background()
	c.Set(ctx, "k", "v", 0)
	if v, ok := c.Get(ctx, "k"); !ok || v != "v" {
		t.Errorf("backward compat broken: got %v, %v", v, ok)
	}
}
