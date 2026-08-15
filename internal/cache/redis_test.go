//go:build redis

package cache

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func testRedisClient(t *testing.T) *redis.Client {
	t.Helper()
	dsn := os.Getenv("REDIS_TEST_DSN")
	if dsn == "" {
		dsn = "redis://localhost:6379/15" // use DB 15 for tests
	}
	client, err := NewRedisClient(dsn)
	if err != nil {
		t.Skipf("Redis not available: %v", err)
	}
	t.Cleanup(func() {
		client.FlushDB(context.Background())
		client.Close()
	})
	return client
}

func TestRedisCache_GetSet(t *testing.T) {
	client := testRedisClient(t)
	c := NewRedisCache[string](client, "test:getset")
	ctx := context.Background()

	// miss
	_, ok := c.Get(ctx, "k1")
	if ok {
		t.Fatal("expected miss")
	}

	// hit
	c.Set(ctx, "k1", "hello", time.Minute)
	v, ok := c.Get(ctx, "k1")
	if !ok || v != "hello" {
		t.Fatalf("expected hello, got %q ok=%v", v, ok)
	}
}

func TestRedisCache_TTLExpiry(t *testing.T) {
	client := testRedisClient(t)
	c := NewRedisCache[int](client, "test:ttl")
	ctx := context.Background()

	c.Set(ctx, "k1", 42, 100*time.Millisecond)
	v, ok := c.Get(ctx, "k1")
	if !ok || v != 42 {
		t.Fatalf("expected 42, got %d ok=%v", v, ok)
	}

	time.Sleep(150 * time.Millisecond)
	_, ok = c.Get(ctx, "k1")
	if ok {
		t.Fatal("expected miss after TTL")
	}
}

func TestRedisCache_Delete(t *testing.T) {
	client := testRedisClient(t)
	c := NewRedisCache[string](client, "test:del")
	ctx := context.Background()

	c.Set(ctx, "k1", "v1", time.Minute)
	c.Delete(ctx, "k1")

	_, ok := c.Get(ctx, "k1")
	if ok {
		t.Fatal("expected miss after delete")
	}
}

func TestRedisCache_DeleteByPrefix(t *testing.T) {
	client := testRedisClient(t)
	c := NewRedisCache[string](client, "test:prefix")
	ctx := context.Background()

	c.Set(ctx, "agent:1:a", "v1", time.Minute)
	c.Set(ctx, "agent:1:b", "v2", time.Minute)
	c.Set(ctx, "agent:2:a", "v3", time.Minute)

	c.DeleteByPrefix(ctx, "agent:1:")

	if _, ok := c.Get(ctx, "agent:1:a"); ok {
		t.Fatal("agent:1:a should be deleted")
	}
	if _, ok := c.Get(ctx, "agent:1:b"); ok {
		t.Fatal("agent:1:b should be deleted")
	}
	if _, ok := c.Get(ctx, "agent:2:a"); !ok {
		t.Fatal("agent:2:a should still exist")
	}
}

func TestRedisCache_Clear(t *testing.T) {
	client := testRedisClient(t)
	c := NewRedisCache[int](client, "test:clear")
	ctx := context.Background()

	c.Set(ctx, "a", 1, time.Minute)
	c.Set(ctx, "b", 2, time.Minute)
	c.Clear(ctx)

	if _, ok := c.Get(ctx, "a"); ok {
		t.Fatal("expected miss after clear")
	}
	if _, ok := c.Get(ctx, "b"); ok {
		t.Fatal("expected miss after clear")
	}
}

// TestRedisCache_StructRoundtrip verifies JSON serialization of complex types.
type testStruct struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func TestRedisCache_StructRoundtrip(t *testing.T) {
	client := testRedisClient(t)
	c := NewRedisCache[[]testStruct](client, "test:struct")
	ctx := context.Background()

	data := []testStruct{{Name: "Alice", Age: 30}, {Name: "Bob", Age: 25}}
	c.Set(ctx, "users", data, time.Minute)

	got, ok := c.Get(ctx, "users")
	if !ok {
		t.Fatal("expected hit")
	}
	if len(got) != 2 || got[0].Name != "Alice" || got[1].Age != 25 {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
}
