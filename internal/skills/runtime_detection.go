package skills

import (
	"os"
	"sync"
)

// isAlpineOnce ensures the stat call happens at most once per process lifetime.
var (
	isAlpineOnce sync.Once
	isAlpineVal  bool
)

// IsAlpineRuntime reports whether the current process is running on Alpine
// Linux. Detection: presence of /etc/alpine-release (Alpine-specific file;
// not present on Debian, Ubuntu, RHEL, macOS, or Windows).
//
// The result is cached for the lifetime of the process; safe for concurrent use.
// Used by packages update wiring to gate apk checker/executor registration.
// Call overrideAlpineRuntime in tests to bypass the stat call.
func IsAlpineRuntime() bool {
	isAlpineOnce.Do(func() {
		_, err := os.Stat("/etc/alpine-release")
		isAlpineVal = err == nil
	})
	return isAlpineVal
}

// overrideAlpineRuntime resets the once guard and sets a fixed result.
// ONLY for use in tests — not exported. Tests that need to control the
// Alpine detection result must call this before exercising any code that
// calls IsAlpineRuntime().
func overrideAlpineRuntime(val bool) {
	isAlpineOnce = sync.Once{}
	isAlpineVal = val
	isAlpineOnce.Do(func() {
		// Already set via isAlpineVal; Do body records the value.
		// Reassign inside Do to guarantee the once-cached value is val.
		isAlpineVal = val
	})
}
