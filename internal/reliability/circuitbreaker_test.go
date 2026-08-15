package reliability

import (
	"testing"
	"time"
)

func fakeClock(t *testing.T) (func() time.Time, func(d time.Duration)) {
	t.Helper()
	cur := time.Unix(1_000_000, 0)
	return func() time.Time { return cur }, func(d time.Duration) { cur = cur.Add(d) }
}

func newTestBreaker(now func() time.Time) *CircuitBreaker {
	o := DefaultCircuitOptions()
	o.nowFn = now
	return NewCircuitBreaker(o)
}

func TestAllowHealthy(t *testing.T) {
	now, _ := fakeClock(t)
	cb := newTestBreaker(now)
	if !cb.Allow("a:b") {
		t.Errorf("healthy breaker should allow")
	}
	if cb.State("a:b") != CircuitHealthy {
		t.Errorf("expected healthy state")
	}
}

func TestOpensAfterThreshold(t *testing.T) {
	now, advance := fakeClock(t)
	cb := newTestBreaker(now)

	// Failures below threshold → degraded, still allowed.
	for i := 0; i < 2; i++ {
		cb.RecordFailure("a:b")
	}
	if cb.State("a:b") != CircuitDegraded {
		t.Errorf("expected degraded after 2 failures, got %s", cb.State("a:b"))
	}
	if !cb.Allow("a:b") {
		t.Errorf("degraded should still allow")
	}

	// Cross threshold → open.
	cb.RecordFailure("a:b") // 3
	cb.RecordFailure("a:b") // 4
	cb.RecordFailure("a:b") // 5 → open
	if cb.State("a:b") != CircuitOpen {
		t.Errorf("expected open after threshold, got %s", cb.State("a:b"))
	}
	if cb.Allow("a:b") {
		t.Errorf("open breaker must reject")
	}
	if cb.NextRetryAt("a:b").IsZero() {
		t.Errorf("open breaker should report a retry time")
	}

	// Cooldown expires → half-open, probe allowed once.
	advance(cb.opts.Cooldown + time.Second)
	if !cb.Allow("a:b") {
		t.Errorf("half-open should allow a probe")
	}
	if cb.State("a:b") != CircuitHalfOpen {
		t.Errorf("expected half-open after cooldown, got %s", cb.State("a:b"))
	}
	// A second concurrent probe is blocked while the first is undetermined.
	if cb.Allow("a:b") {
		t.Errorf("half-open should only allow one probe")
	}
	// Record the probe outcome: success closes the circuit.
	cb.RecordSuccess("a:b")
	if cb.State("a:b") != CircuitHealthy {
		t.Errorf("successful probe should close circuit, got %s", cb.State("a:b"))
	}
	if !cb.Allow("a:b") {
		t.Errorf("healthy circuit should allow after close")
	}
}

func TestProbeSuccessCloses(t *testing.T) {
	now, advance := fakeClock(t)
	cb := newTestBreaker(now)
	for i := 0; i < 5; i++ {
		cb.RecordFailure("a:b")
	}
	if cb.State("a:b") != CircuitOpen {
		t.Fatalf("expected open, got %s", cb.State("a:b"))
	}
	advance(cb.opts.Cooldown + time.Second)
	if !cb.Allow("a:b") {
		t.Fatalf("expected probe allowed")
	}
	cb.RecordSuccess("a:b")
	if cb.State("a:b") != CircuitHealthy {
		t.Errorf("successful probe should close circuit, got %s", cb.State("a:b"))
	}
	if !cb.Allow("a:b") {
		t.Errorf("healthy after close should allow")
	}
}

func TestProbeFailureReopens(t *testing.T) {
	now, advance := fakeClock(t)
	cb := newTestBreaker(now)
	for i := 0; i < 5; i++ {
		cb.RecordFailure("a:b")
	}
	advance(cb.opts.Cooldown + time.Second)
	if !cb.Allow("a:b") {
		t.Fatalf("expected probe allowed")
	}
	cb.RecordFailure("a:b") // probe fails → reopen
	if cb.State("a:b") != CircuitOpen {
		t.Errorf("failed probe should reopen, got %s", cb.State("a:b"))
	}
}

// TestStaleProbeIsReleased prevents the half-open wedge: if a caller is allowed
// to probe but never records an outcome, the key must recover once the probe
// timeout elapses instead of staying blocked forever.
func TestStaleProbeIsReleased(t *testing.T) {
	now, advance := fakeClock(t)
	cb := newTestBreaker(now)
	for i := 0; i < 5; i++ {
		cb.RecordFailure("a:b")
	}
	advance(cb.opts.Cooldown + time.Second)
	if !cb.Allow("a:b") {
		t.Fatalf("expected an initial probe to be allowed")
	}
	// A second caller is blocked while the probe is unresolved.
	if cb.Allow("a:b") {
		t.Fatalf("expected single probe while unresolved")
	}

	// The probe holder never records an outcome. After ProbeTimeout, a fresh
	// probe must be permitted instead of the key staying blocked forever.
	advance(cb.opts.ProbeTimeout + time.Second)
	if !cb.Allow("a:b") {
		t.Fatalf("stale probe should be released after its timeout")
	}
}

// TestProbeStillBlockedBeforeTimeout: within the probe window, no second probe
// is allowed even if time passes.
func TestProbeStillBlockedBeforeTimeout(t *testing.T) {
	now, advance := fakeClock(t)
	cb := newTestBreaker(now)
	for i := 0; i < 5; i++ {
		cb.RecordFailure("a:b")
	}
	advance(cb.opts.Cooldown + time.Second)
	if !cb.Allow("a:b") {
		t.Fatalf("expected initial probe allowed")
	}
	advance(cb.opts.ProbeTimeout - time.Second)
	if cb.Allow("a:b") {
		t.Fatalf("second probe must still be blocked before the probe timeout")
	}
}

func TestSuccessResetsDegraded(t *testing.T) {
	now, _ := fakeClock(t)
	cb := newTestBreaker(now)
	cb.RecordFailure("a:b")
	cb.RecordFailure("a:b")
	if cb.State("a:b") != CircuitDegraded {
		t.Fatalf("expected degraded")
	}
	cb.RecordSuccess("a:b")
	if cb.State("a:b") != CircuitHealthy {
		t.Errorf("success should reset to healthy, got %s", cb.State("a:b"))
	}
	if cb.NextRetryAt("a:b") != (time.Time{}) {
		t.Errorf("non-open breaker should have no retry time")
	}
}

func TestKeysAreIsolated(t *testing.T) {
	now, _ := fakeClock(t)
	cb := newTestBreaker(now)
	for i := 0; i < 5; i++ {
		cb.RecordFailure("a:bad")
	}
	if cb.Allow("a:bad") {
		t.Errorf("bad key should be open")
	}
	if !cb.Allow("b:good") {
		t.Errorf("good key must be unaffected")
	}
}