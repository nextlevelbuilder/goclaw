package reliability

import (
	"testing"
	"time"
)

func TestDefaultReturnsNonNilBundle(t *testing.T) {
	// Configure first so the test does not depend on prior package state.
	Configure(DefaultCircuitOptions(), 0)

	r := Default()
	if r == nil {
		t.Fatal("Default() returned nil runtime")
	}
	if r.Breaker == nil {
		t.Error("Breaker is nil")
	}
	if r.Health == nil {
		t.Error("Health is nil")
	}
	if r.RateLimit == nil {
		t.Error("RateLimit is nil")
	}
	if r.Metrics == nil {
		t.Error("Metrics is nil")
	}
}

func TestDefaultIsStableAcrossCalls(t *testing.T) {
	Configure(DefaultCircuitOptions(), 0)
	a := Default()
	b := Default()
	if a != b {
		t.Fatal("Default() must return the same bundle until Configure() is called")
	}
	if a.Breaker != b.Breaker || a.Health != b.Health || a.RateLimit != b.RateLimit || a.Metrics != b.Metrics {
		t.Fatal("bundle components must be stable across Default() calls")
	}
}

func TestConfigureRebuildsBundle(t *testing.T) {
	Configure(DefaultCircuitOptions(), 0)
	old := Default()

	newRT := Configure(DefaultCircuitOptions(), 0)
	if newRT != Default() {
		t.Fatal("Configure() must install the new bundle as the default")
	}
	if newRT.Breaker == old.Breaker {
		t.Error("Configure() must build a new breaker, got the old pointer")
	}
	if newRT.Health == old.Health {
		t.Error("Configure() must build a new health registry, got the old pointer")
	}
	if newRT.RateLimit == old.RateLimit {
		t.Error("Configure() must build a new rate-limit coordinator, got the old pointer")
	}
	if newRT.Metrics == old.Metrics {
		t.Error("Configure() must build a new metrics recorder, got the old pointer")
	}
}

func TestConfigureAppliesOptions(t *testing.T) {
	opts := CircuitOptions{
		FailureThreshold:  3,
		DegradedThreshold: 1,
		Cooldown:          5 * time.Second,
		HalfOpenMax:       2,
		ProbeTimeout:      2 * time.Second,
	}
	rt := Configure(opts, 7)
	if rt == nil {
		t.Fatal("Configure() returned nil runtime")
	}

	// Health registry shares the configured breaker: observing failures on
	// the registry must move the breaker through degraded to open using the
	// custom thresholds.
	rt.Health.ObserveFailure("provider", "model", ErrProviderServerError)
	if st := rt.Breaker.State("provider:model"); st != CircuitDegraded {
		t.Fatalf("expected degraded after 1 failure with DegradedThreshold=1, got %v", st)
	}
	rt.Health.ObserveFailure("provider", "model", ErrProviderServerError)
	if st := rt.Breaker.State("provider:model"); st != CircuitDegraded {
		t.Fatalf("expected degraded after 2 failures (FailureThreshold=3), got %v", st)
	}
	rt.Health.ObserveFailure("provider", "model", ErrProviderServerError)
	if st := rt.Breaker.State("provider:model"); st != CircuitOpen {
		t.Fatalf("expected open after 3 failures with FailureThreshold=3, got %v", st)
	}

	// Cooldown must be honored.
	if until := rt.Breaker.NextRetryAt("provider:model"); until.IsZero() {
		t.Fatal("expected open-until deadline after breaker opened")
	}
}

func TestDefaultRuntimeBuildsDefaultBundle(t *testing.T) {
	// defaultRuntime is the bundle Default() lazily constructs when nothing
	// has been Configure()'d. Testing it directly avoids depending on
	// package-global state (other tests in this package call Configure()).
	r := defaultRuntime()
	if r == nil || r.Breaker == nil || r.Health == nil || r.RateLimit == nil || r.Metrics == nil {
		t.Fatal("default bundle incomplete")
	}

	// The default breaker opts must be the production defaults.
	want := DefaultCircuitOptions()
	got := r.Breaker.opts
	if got.FailureThreshold != want.FailureThreshold ||
		got.DegradedThreshold != want.DegradedThreshold ||
		got.Cooldown != want.Cooldown ||
		got.HalfOpenMax != want.HalfOpenMax ||
		got.ProbeTimeout != want.ProbeTimeout {
		t.Fatalf("default breaker options mismatch: got %+v want %+v", got, want)
	}

	// The health registry must share the default breaker.
	if r.Health.breaker != r.Breaker {
		t.Fatal("health registry must share the default breaker")
	}
}
