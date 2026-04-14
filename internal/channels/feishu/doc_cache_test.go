package feishu

import (
	"testing"
	"time"
)

func TestDocCache_SetAndGet(t *testing.T) {
	cache := newDocCache(4, time.Minute)
	cache.Set("A", "alpha")
	cache.Set("B", "bravo")

	if got, ok := cache.Get("A"); !ok || got != "alpha" {
		t.Errorf("Get A: got (%q, %v), want (\"alpha\", true)", got, ok)
	}
	if got, ok := cache.Get("B"); !ok || got != "bravo" {
		t.Errorf("Get B: got (%q, %v), want (\"bravo\", true)", got, ok)
	}
	if _, ok := cache.Get("missing"); ok {
		t.Errorf("Get missing: expected not-found")
	}
}

func TestDocCache_EvictionOnSizeExceeded(t *testing.T) {
	cache := newDocCache(2, time.Minute)
	cache.Set("A", "alpha")
	cache.Set("B", "bravo")
	cache.Set("C", "charlie") // should evict LRU (A)

	if _, ok := cache.Get("A"); ok {
		t.Errorf("A should be evicted")
	}
	if _, ok := cache.Get("B"); !ok {
		t.Errorf("B should still be present")
	}
	if _, ok := cache.Get("C"); !ok {
		t.Errorf("C should be present")
	}
}

func TestDocCache_LRURecency(t *testing.T) {
	cache := newDocCache(2, time.Minute)
	cache.Set("A", "alpha")
	cache.Set("B", "bravo")
	// Touch A — makes B the LRU.
	if _, ok := cache.Get("A"); !ok {
		t.Fatal("A should be present before touch")
	}
	cache.Set("C", "charlie") // should evict B, not A

	if _, ok := cache.Get("B"); ok {
		t.Errorf("B should be evicted as LRU")
	}
	if _, ok := cache.Get("A"); !ok {
		t.Errorf("A should still be present (was recently touched)")
	}
}

func TestDocCache_TTLExpiry(t *testing.T) {
	// 50ms TTL + 100ms sleep is loose enough to survive CI scheduler jitter
	// on slow macOS/linux runners under -race.
	cache := newDocCache(4, 50*time.Millisecond)
	cache.Set("A", "alpha")
	time.Sleep(100 * time.Millisecond)
	if _, ok := cache.Get("A"); ok {
		t.Errorf("A should be expired")
	}
}

func TestDocCache_UpdateExistingKey(t *testing.T) {
	cache := newDocCache(4, time.Minute)
	cache.Set("A", "alpha")
	cache.Set("A", "alpha-v2") // same key, new value

	if got, ok := cache.Get("A"); !ok || got != "alpha-v2" {
		t.Errorf("update: got (%q, %v), want (\"alpha-v2\", true)", got, ok)
	}
}
