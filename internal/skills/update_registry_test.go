package skills

import (
	"context"
	"testing"
	"time"
)

// fakeChecker is a minimal UpdateChecker for registry tests.
type fakeChecker struct {
	source    string
	available bool
	err       error
}

func (f *fakeChecker) Source() string { return f.source }
func (f *fakeChecker) Check(_ context.Context, _ map[string]string) UpdateCheckResult {
	return UpdateCheckResult{
		Source:    f.source,
		Available: f.available,
		Err:       f.err,
	}
}

func TestRegistry_Availability(t *testing.T) {
	reg := NewUpdateRegistry(nil, "", time.Hour)

	reg.RegisterChecker(&fakeChecker{source: "github", available: true})
	reg.RegisterChecker(&fakeChecker{source: "pip", available: false})

	errs := reg.CheckAll(context.Background())
	if len(errs) != 0 {
		t.Fatalf("unexpected errors from CheckAll: %v", errs)
	}

	avail := reg.Availability()

	if got, want := avail["github"], true; got != want {
		t.Errorf("Availability[github] = %v, want %v", got, want)
	}
	if got, want := avail["pip"], false; got != want {
		t.Errorf("Availability[pip] = %v, want %v", got, want)
	}

	// Verify returned map is a clone — mutating it must not affect the registry.
	avail["github"] = false
	avail["pip"] = true
	avail2 := reg.Availability()
	if avail2["github"] != true {
		t.Error("Availability() returned same map (not a clone): mutation propagated")
	}
	if avail2["pip"] != false {
		t.Error("Availability() returned same map (not a clone): mutation propagated")
	}
}

func TestRegistry_Availability_NeverChecked(t *testing.T) {
	// A registry with no CheckAll call should return an empty map.
	// Callers are expected to treat missing keys as true (first-boot default).
	reg := NewUpdateRegistry(nil, "", time.Hour)
	avail := reg.Availability()
	if len(avail) != 0 {
		t.Errorf("expected empty map before CheckAll, got %v", avail)
	}
}

func TestRegistry_Availability_UpdatedOnRecheck(t *testing.T) {
	// A checker that flips available state between calls.
	reg := NewUpdateRegistry(nil, "", time.Hour)
	checker := &fakeChecker{source: "npm", available: false}
	reg.RegisterChecker(checker)

	reg.CheckAll(context.Background()) //nolint:errcheck
	if got := reg.Availability()["npm"]; got != false {
		t.Errorf("first check: Availability[npm] = %v, want false", got)
	}

	// Second check with available=true.
	checker.available = true
	reg.CheckAll(context.Background()) //nolint:errcheck
	if got := reg.Availability()["npm"]; got != true {
		t.Errorf("second check: Availability[npm] = %v, want true", got)
	}
}
