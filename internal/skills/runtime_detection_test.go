package skills

import (
	"testing"
)

// TestIsAlpineRuntime_NoPanic verifies the function executes without panic
// and returns a consistent cached result on repeated calls.
// The actual boolean value is environment-dependent (true on Alpine CI,
// false on macOS/Debian dev hosts) — we verify determinism, not the value.
func TestIsAlpineRuntime_NoPanic(t *testing.T) {
	first := IsAlpineRuntime()
	second := IsAlpineRuntime()

	if first != second {
		t.Errorf("IsAlpineRuntime() returned different values on consecutive calls: %v then %v (must be cached)", first, second)
	}
}

// TestOverrideAlpineRuntime_ForcesTrue verifies the test-only override hook
// correctly forces IsAlpineRuntime to return true.
func TestOverrideAlpineRuntime_ForcesTrue(t *testing.T) {
	overrideAlpineRuntime(true)
	if !IsAlpineRuntime() {
		t.Error("overrideAlpineRuntime(true): IsAlpineRuntime() returned false, want true")
	}
}

// TestOverrideAlpineRuntime_ForcesFalse verifies the test-only override hook
// correctly forces IsAlpineRuntime to return false.
func TestOverrideAlpineRuntime_ForcesFalse(t *testing.T) {
	overrideAlpineRuntime(false)
	if IsAlpineRuntime() {
		t.Error("overrideAlpineRuntime(false): IsAlpineRuntime() returned true, want false")
	}
}

// TestOverrideAlpineRuntime_Idempotent verifies that calling the override
// twice gives the last value and the result stays stable.
func TestOverrideAlpineRuntime_Idempotent(t *testing.T) {
	overrideAlpineRuntime(true)
	overrideAlpineRuntime(false)
	if IsAlpineRuntime() {
		t.Error("second overrideAlpineRuntime(false) should win: IsAlpineRuntime() returned true")
	}
	// A second read must be consistent.
	if IsAlpineRuntime() {
		t.Error("IsAlpineRuntime() not stable after override — cache broken")
	}
}
