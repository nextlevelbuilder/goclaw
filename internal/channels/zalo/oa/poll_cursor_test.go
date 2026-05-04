package oa

import (
	"testing"
)

func TestPollCursor_AdvanceAndGet(t *testing.T) {
	t.Parallel()
	pc := newPollCursor(10)

	if got := pc.Get("u1"); got != 0 {
		t.Errorf("Get(missing) = %d, want 0", got)
	}
	if !pc.Advance("u1", 100) {
		t.Errorf("Advance(u1, 100) returned false on fresh cursor")
	}
	if got := pc.Get("u1"); got != 100 {
		t.Errorf("Get(u1) = %d, want 100", got)
	}

	// Newer ts updates.
	if !pc.Advance("u1", 200) {
		t.Errorf("Advance(u1, 200) returned false (newer ts)")
	}
	if got := pc.Get("u1"); got != 200 {
		t.Errorf("Get(u1) = %d, want 200", got)
	}

	// Older ts is ignored, returns false.
	if pc.Advance("u1", 150) {
		t.Errorf("Advance(u1, 150) returned true on older ts; want false")
	}
	if got := pc.Get("u1"); got != 200 {
		t.Errorf("Get(u1) = %d after stale advance, want 200", got)
	}
}

func TestPollCursor_LRUEvictsOldestEntry(t *testing.T) {
	t.Parallel()
	pc := newPollCursor(3)

	pc.Advance("u1", 1)
	pc.Advance("u2", 2)
	pc.Advance("u3", 3)

	// All three present, no eviction yet.
	for k, want := range map[string]int64{"u1": 1, "u2": 2, "u3": 3} {
		if got := pc.Get(k); got != want {
			t.Errorf("Get(%s) = %d, want %d", k, got, want)
		}
	}

	// Touch u1 → moves to MRU.
	pc.Advance("u1", 10)
	// Insert u4 → triggers eviction of LEAST-recent = u2.
	pc.Advance("u4", 4)

	if got := pc.Get("u2"); got != 0 {
		t.Errorf("Get(u2 evicted) = %d, want 0", got)
	}
	if got := pc.Get("u1"); got != 10 {
		t.Errorf("Get(u1 still present) = %d, want 10", got)
	}
	if got := pc.Get("u4"); got != 4 {
		t.Errorf("Get(u4) = %d, want 4", got)
	}
}

func TestPollCursor_DirtyFlag(t *testing.T) {
	t.Parallel()
	pc := newPollCursor(10)

	if pc.IsDirty() {
		t.Error("fresh cursor is dirty")
	}
	pc.Advance("u1", 100)
	if !pc.IsDirty() {
		t.Error("after Advance, cursor not dirty")
	}
	pc.ClearDirty()
	if pc.IsDirty() {
		t.Error("after ClearDirty, still dirty")
	}
	// Re-advance same value → no change → not dirty
	pc.Advance("u1", 100)
	if pc.IsDirty() {
		t.Error("re-advance with same value marked dirty")
	}
	// Advance with new value → dirty
	pc.Advance("u1", 200)
	if !pc.IsDirty() {
		t.Error("advance with new value didn't dirty")
	}
}

func TestPollCursor_Snapshot(t *testing.T) {
	t.Parallel()
	pc := newPollCursor(10)
	pc.Advance("u1", 1)
	pc.Advance("u2", 2)
	pc.Advance("u3", 3)

	snap := pc.Snapshot()
	if len(snap) != 3 {
		t.Errorf("snap len = %d, want 3", len(snap))
	}
	if snap["u2"] != 2 {
		t.Errorf("snap[u2] = %d, want 2", snap["u2"])
	}
	// Snapshot is a copy — mutating it does not affect cursor.
	snap["u2"] = 999
	if pc.Get("u2") != 2 {
		t.Errorf("Snapshot returned a live ref; cursor mutated")
	}
}

func TestPollCursor_LastSeenTimestamp(t *testing.T) {
	t.Parallel()
	pc := newPollCursor(10)

	// Empty cursor → 0.
	if got := pc.LastSeenTimestamp(); got != 0 {
		t.Errorf("LastSeenTimestamp(empty) = %d, want 0", got)
	}

	pc.Advance("u1", 100)
	pc.Advance("u2", 300)
	pc.Advance("u3", 200)

	if got := pc.LastSeenTimestamp(); got != 300 {
		t.Errorf("LastSeenTimestamp = %d, want 300 (max)", got)
	}

	// Advancing a smaller user does not lower the max.
	pc.Advance("u1", 250)
	if got := pc.LastSeenTimestamp(); got != 300 {
		t.Errorf("LastSeenTimestamp = %d, want 300", got)
	}
	// New higher entry wins.
	pc.Advance("u4", 500)
	if got := pc.LastSeenTimestamp(); got != 500 {
		t.Errorf("LastSeenTimestamp = %d, want 500", got)
	}
}

func TestParseCursorFromConfig(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
		"poll_interval_seconds": 15,
		"poll_cursor": {"u1": 100, "u2": 200}
	}`)
	got := parseCursorFromConfig(raw)
	if got["u1"] != 100 || got["u2"] != 200 {
		t.Errorf("parseCursorFromConfig = %v", got)
	}

	// Missing key → empty map (not nil).
	got2 := parseCursorFromConfig([]byte(`{"poll_interval_seconds":15}`))
	if got2 == nil {
		t.Errorf("expected non-nil map for missing poll_cursor key")
	}
	if len(got2) != 0 {
		t.Errorf("expected empty map, got %v", got2)
	}

	// Garbage input → empty map (no panic).
	if parseCursorFromConfig([]byte(`{not json`)) == nil {
		t.Errorf("expected non-nil map for invalid JSON")
	}
}


