package common

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeClock returns advancing deterministic timestamps so tests don't sleep.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newDedupWithClock(ttl time.Duration, maxGlobal int) (*Dedup, *fakeClock) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	d := NewDedup(ttl, maxGlobal)
	d.now = clk.now
	return d, clk
}

func TestDedup_FirstAddNotSeen(t *testing.T) {
	d, _ := newDedupWithClock(time.Minute, 100)
	id := uuid.New()
	if d.SeenOrAdd(id, "m1") {
		t.Error("first SeenOrAdd should report not-seen")
	}
}

func TestDedup_DuplicateWithinTTLSeen(t *testing.T) {
	d, _ := newDedupWithClock(time.Minute, 100)
	id := uuid.New()
	d.SeenOrAdd(id, "m1")
	if !d.SeenOrAdd(id, "m1") {
		t.Error("second SeenOrAdd within TTL should report seen")
	}
}

func TestDedup_ExpiryRecyclesEntry(t *testing.T) {
	d, clk := newDedupWithClock(10*time.Millisecond, 100)
	id := uuid.New()
	d.SeenOrAdd(id, "m1")
	clk.advance(20 * time.Millisecond)
	if d.SeenOrAdd(id, "m1") {
		t.Error("entry should be expired and treated as not-seen")
	}
}

func TestDedup_InstanceScopeIsolation(t *testing.T) {
	d, _ := newDedupWithClock(time.Minute, 100)
	a, b := uuid.New(), uuid.New()
	d.SeenOrAdd(a, "m1")
	if d.SeenOrAdd(b, "m1") {
		t.Error("same messageID under different instanceID should not collide")
	}
}

func TestDedup_GlobalCapEvictsOldest(t *testing.T) {
	// maxGlobal=12 keeps per-instance cap at 3 (maxGlobal/4) so this exercises
	// global eviction without colliding with the per-instance cap.
	d, clk := newDedupWithClock(time.Minute, 12)
	a, b, c, e := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{a, b, c, e} {
		for _, m := range []string{"m1", "m2", "m3"} {
			d.SeenOrAdd(id, m)
			clk.advance(time.Millisecond)
		}
	}
	if d.Len() != 12 {
		t.Fatalf("len = %d, want 12", d.Len())
	}
	// One more entry forces global eviction of the oldest (a, m1).
	d.SeenOrAdd(uuid.New(), "x")
	if d.Len() != 12 {
		t.Errorf("len after eviction = %d, want 12", d.Len())
	}
	if d.SeenOrAdd(a, "m1") {
		t.Error("oldest entry should have been evicted")
	}
}

func TestDedup_PerInstanceCapEvictsOldestForThatInstance(t *testing.T) {
	d, clk := newDedupWithClock(time.Minute, 16) // perInstance=4
	a, b := uuid.New(), uuid.New()
	for _, m := range []string{"m1", "m2", "m3", "m4"} {
		d.SeenOrAdd(a, m)
		clk.advance(time.Millisecond)
	}
	d.SeenOrAdd(b, "z1") // unrelated tenant
	clk.advance(time.Millisecond)

	// Adding 5th entry for `a` evicts `a`'s oldest (m1) only.
	d.SeenOrAdd(a, "m5")
	if d.SeenOrAdd(b, "z1") == false {
		t.Error("instance b's entry should still be present after a's eviction")
	}
	if d.SeenOrAdd(a, "m1") {
		t.Error("a/m1 should have been evicted as oldest for instance a")
	}
}

func TestDedup_EmptyMessageIDNotRecorded(t *testing.T) {
	d, _ := newDedupWithClock(time.Minute, 100)
	id := uuid.New()
	if d.SeenOrAdd(id, "") {
		t.Error("empty messageID should never report seen")
	}
	if d.Len() != 0 {
		t.Error("empty messageID should not be recorded")
	}
}

func TestDedup_ConcurrentAccessRaceClean(t *testing.T) {
	d := NewDedup(time.Minute, 1000)
	id := uuid.New()
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			d.SeenOrAdd(id, "m1")
		}(i)
	}
	wg.Wait()
}
