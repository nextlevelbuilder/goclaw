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
