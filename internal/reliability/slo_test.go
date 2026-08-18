package reliability

import (
	"math"
	"sync"
	"testing"
	"time"
)

// newTestTracker builds a tracker whose clock is controllable so window
// pruning can be tested without real sleeps.
func newTestTracker(t *testing.T, target float64, window time.Duration, now *time.Time) *SLOTracker {
	t.Helper()
	tr := NewSLOTracker(target, window)
	tr.nowFn = func() time.Time { return *now }
	return tr
}

// takeSnapshot converts raw request/success counts into a Snapshot delta.
func takeSnapshot(requests, successes uint64) Snapshot {
	return Snapshot{LLMRequests: requests, LLMSuccesses: successes}
}

func TestSLOTrackerEmptyZero(t *testing.T) {
	var nilTr *SLOTracker
	st := nilTr.Status()
	if st.Enabled || st.TotalRequests != 0 || st.SuccessRate != 0 || st.BurnRate != 0 || !st.WithinBudget {
		t.Fatalf("nil tracker status = %+v, want zero with WithinBudget=true", st)
	}

	// Zero-window tracker behaves like nil (no window, no evaluation).
	zero := NewSLOTracker(0.99, 0)
	if st := zero.Status(); st.Enabled || st.Window != 0 || !st.WithinBudget {
		t.Fatalf("zero-window tracker status = %+v, want zero with WithinBudget=true", st)
	}
	if zero.Enabled() {
		t.Fatal("zero-window tracker must not report Enabled")
	}
	zero.Observe(takeSnapshot(10, 10)) // must not panic
	if st := zero.Status(); st.TotalRequests != 0 {
		t.Fatalf("zero-window tracker collected samples: %+v", st)
	}
}

func TestSLOTrackerIdleNoBurn(t *testing.T) {
	now := time.Unix(1000, 0)
	tr := newTestTracker(t, 0.99, time.Hour, &now)

	// Idle snapshots (zero requests) must never contribute samples and must
	// not trigger division-by-zero.
	for i := 0; i < 5; i++ {
		tr.Observe(takeSnapshot(0, 0))
	}
	if st := tr.Status(); st.TotalRequests != 0 {
		t.Fatalf("idle observes added samples: %+v", st)
	}

	// Failure deltas do count, and every one is a failed interval.
	for i := 0; i < 3; i++ {
		tr.Observe(takeSnapshot(3, 0))
	}
	st := tr.Status()
	if st.TotalRequests != 3 {
		t.Fatalf("request-bearing samples = %d, want 3", st.TotalRequests)
	}
	if st.SuccessRate != 0 {
		t.Fatalf("all-failure success rate = %v, want 0", st.SuccessRate)
	}
	if st.WithinBudget {
		t.Fatal("all failures must be outside budget")
	}
	// Zero observed success rate leaves BurnRate at 0 (no division); the zero
	// success rate + WithinBudget=false carry the outage signal.
	if st.BurnRate != 0 {
		t.Fatalf("burn rate with zero success rate = %v, want 0", st.BurnRate)
	}
}

func TestSLOTrackerSuccessRate(t *testing.T) {
	now := time.Unix(1000, 0)
	tr := newTestTracker(t, 0.99, time.Hour, &now)

	// A flush interval counts as a success only when every request succeeded.
	tr.Observe(takeSnapshot(100, 90)) // interval with failures → bad
	tr.Observe(takeSnapshot(100, 100))
	tr.Observe(takeSnapshot(1, 1))

	st := tr.Status()
	if st.TotalRequests != 3 {
		t.Fatalf("total requests = %d, want 3", st.TotalRequests)
	}
	if st.Successes != 2 {
		t.Fatalf("successes = %d, want 2", st.Successes)
	}
	if math.Abs(st.SuccessRate-2.0/3.0) > 1e-9 {
		t.Fatalf("success rate = %v, want 2/3", st.SuccessRate)
	}
	if st.WithinBudget {
		t.Fatal("2/3 < 0.99 must be outside budget")
	}
	wantBurn := 0.99 / (2.0 / 3.0)
	if math.Abs(st.BurnRate-wantBurn) > 1e-9 {
		t.Fatalf("burn rate = %v, want %v", st.BurnRate, wantBurn)
	}
}

func TestSLOTrackerOnTarget(t *testing.T) {
	now := time.Unix(1000, 0)
	tr := newTestTracker(t, 0.99, time.Hour, &now)

	// 99 ok intervals + 1 failed interval → rate exactly 0.99.
	for i := 0; i < 99; i++ {
		tr.Observe(takeSnapshot(1, 1))
	}
	tr.Observe(takeSnapshot(1, 0))

	st := tr.Status()
	if math.Abs(st.SuccessRate-0.99) > 1e-9 {
		t.Fatalf("success rate = %v, want 0.99", st.SuccessRate)
	}
	if !st.WithinBudget {
		t.Fatal("success rate == target must be within budget")
	}
	if math.Abs(st.BurnRate-1.0) > 1e-9 {
		t.Fatalf("burn rate at target = %v, want 1.0", st.BurnRate)
	}
}

func TestSLOTrackerPruneByWindow(t *testing.T) {
	now := new(time.Time)
	*now = time.Unix(1000, 0)
	tr := newTestTracker(t, 0.99, 30*time.Minute, now)

	tr.Observe(takeSnapshot(10, 10)) // t=1000
	*now = time.Unix(1000+20*60, 0)
	tr.Observe(takeSnapshot(10, 9)) // t=2200, still within window
	st := tr.Status()
	if st.TotalRequests != 2 {
		t.Fatalf("after two in-window observes total = %d, want 2", st.TotalRequests)
	}

	*now = time.Unix(1000+31*60, 0)
	tr.Observe(takeSnapshot(10, 10)) // t=2860 — first sample (t=1000) is now stale
	st = tr.Status()
	if st.TotalRequests != 2 {
		t.Fatalf("after pruning total = %d, want 2", st.TotalRequests)
	}
	// Survivors: t=2200 (9/10 → failed interval) and t=2860 (10/10 → ok).
	if st.Successes != 1 {
		t.Fatalf("after pruning successes = %d, want 1", st.Successes)
	}
}

func TestSLOTrackerAllDead(t *testing.T) {
	now := time.Unix(1000, 0)
	tr := newTestTracker(t, 0.99, time.Hour, &now)
	tr.Observe(takeSnapshot(10, 0))

	st := tr.Status()
	if st.SuccessRate != 0 {
		t.Fatalf("success rate = %v, want 0", st.SuccessRate)
	}
	// Burn rate is undefined (0) when observed success rate is zero — no
	// division-by-zero, and the field stays JSON-marshalable.
	if st.BurnRate != 0 {
		t.Fatalf("burn rate = %v, want 0", st.BurnRate)
	}
	if st.WithinBudget {
		t.Fatal("all requests failing must be outside budget")
	}
}

func TestSLOTrackerConcurrentObserve(t *testing.T) {
	tr := NewSLOTracker(0.99, time.Hour)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				tr.Observe(takeSnapshot(1, 1))
			}
		}()
	}
	wg.Wait()
	st := tr.Status()
	if st.TotalRequests != 800 {
		t.Fatalf("concurrent observes total = %d, want 800", st.TotalRequests)
	}
	if math.Abs(st.SuccessRate-1.0) > 1e-9 {
		t.Fatalf("success rate = %v, want 1.0", st.SuccessRate)
	}
}