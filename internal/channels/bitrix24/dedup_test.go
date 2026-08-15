package bitrix24

import (
	"strconv"
	"sync"
	"testing"
	"time"
)

// peekDedup inspects the cache without mutating it. Only safe from tests in
// this package because it touches the unexported mutex directly.
func peekDedup(c *dedupCache, key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.seen[key]
	return ok
}

func TestDedupCache_SeenMarksFirstMissesDuplicates(t *testing.T) {
	c := newDedupCache(10, time.Minute)
	if c.Seen("abc") {
		t.Fatal("first sighting should be a miss")
	}
	if !c.Seen("abc") {
		t.Fatal("second sighting should be a hit")
	}
	if !c.Seen("abc") {
		t.Fatal("subsequent sightings should remain hits")
	}
}

func TestDedupCache_EmptyKeyIsUnseen(t *testing.T) {
	c := newDedupCache(10, time.Minute)
	if c.Seen("") {
		t.Fatal("empty key must never register as seen")
	}
	if c.Len() != 0 {
		t.Fatalf("empty key should not be stored; len=%d", c.Len())
	}
}

func TestDedupCache_EvictsOldestWhenFull(t *testing.T) {
	c := newDedupCache(3, time.Minute)
	// Fill up.
	_ = c.Seen("a")
	_ = c.Seen("b")
	_ = c.Seen("c")
	if c.Len() != 3 {
		t.Fatalf("len after fill = %d; want 3", c.Len())
	}

	// Adding a 4th evicts the oldest ("a").
	_ = c.Seen("d")
	if c.Len() != 3 {
		t.Fatalf("len after overflow = %d; want 3", c.Len())
	}
	if peekDedup(c, "a") {
		t.Error("'a' should have been evicted (LRU)")
	}
	// 'b', 'c', 'd' should still be present (no mutation via peek).
	for _, k := range []string{"b", "c", "d"} {
		if !peekDedup(c, k) {
			t.Errorf("%q should still be cached", k)
		}
	}
}

func TestDedupCache_LRUPromotionOnHit(t *testing.T) {
	c := newDedupCache(3, time.Minute)
	_ = c.Seen("a")
	_ = c.Seen("b")
	_ = c.Seen("c")

	// Touch 'a' — it should move to MRU, making 'b' the oldest.
	_ = c.Seen("a")

	_ = c.Seen("d") // evicts oldest — should be 'b' now
	if peekDedup(c, "b") {
		t.Error("'b' should have been evicted after 'a' promotion")
	}
	for _, k := range []string{"a", "c", "d"} {
		if !peekDedup(c, k) {
			t.Errorf("%q should still be cached", k)
		}
	}
}

func TestDedupCache_TTLEvictsOnAccess(t *testing.T) {
	c := newDedupCache(10, 100*time.Millisecond)
	nowBase := time.Unix(1_700_000_000, 0)
	// Virtualise time so the test doesn't sleep.
	c.now = func() time.Time { return nowBase }
	_ = c.Seen("x")

	// Fast-forward past TTL.
	c.now = func() time.Time { return nowBase.Add(200 * time.Millisecond) }

	if c.Seen("x") {
		t.Fatal("entry should have aged out")
	}
	// After that refresh, immediate re-check is a hit again.
	if !c.Seen("x") {
		t.Fatal("refreshed entry should be a hit")
	}
}

func TestDedupCache_SweepExpiredRemovesStale(t *testing.T) {
	c := newDedupCache(100, 50*time.Millisecond)
	nowBase := time.Unix(1_700_000_000, 0)
	c.now = func() time.Time { return nowBase }

	for i := 0; i < 10; i++ {
		_ = c.Seen("k" + strconv.Itoa(i))
	}
	if c.Len() != 10 {
		t.Fatalf("pre-sweep len = %d", c.Len())
	}

	// Jump well past TTL and sweep.
	c.now = func() time.Time { return nowBase.Add(1 * time.Second) }
	c.sweepExpired()

	if c.Len() != 0 {
		t.Fatalf("post-sweep len = %d; want 0", c.Len())
	}
}

func TestDedupCache_TTLDisabledKeepsEntries(t *testing.T) {
	c := newDedupCache(10, 0) // ttl=0 → no time-based eviction
	nowBase := time.Unix(1_700_000_000, 0)
	c.now = func() time.Time { return nowBase }

	_ = c.Seen("k")
	// Even a very large jump shouldn't matter when TTL is disabled.
	c.now = func() time.Time { return nowBase.Add(24 * time.Hour) }

	if !c.Seen("k") {
		t.Fatal("with TTL=0 entries must not age out")
	}
}

func TestDedupCache_ConcurrentSeenIsSafe(t *testing.T) {
	c := newDedupCache(1000, time.Minute)
	var wg sync.WaitGroup
	const workers = 16
	const perWorker = 200

	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				_ = c.Seen("key" + strconv.Itoa(i))
			}
		}(w)
	}
	wg.Wait()

	// With 200 distinct keys all concurrent workers should converge on
	// exactly 200 cached entries (no duplicates, no lost writes).
	if c.Len() != 200 {
		t.Fatalf("concurrent len = %d; want 200", c.Len())
	}
}

func TestDedupCache_StopIsIdempotent(t *testing.T) {
	c := newDedupCache(10, 100*time.Millisecond)
	c.StartSweeper(10 * time.Millisecond)

	c.Stop()
	c.Stop() // second call must not panic on closed channel

	// Wait long enough that any residual goroutine would have ticked.
	time.Sleep(30 * time.Millisecond)
}
