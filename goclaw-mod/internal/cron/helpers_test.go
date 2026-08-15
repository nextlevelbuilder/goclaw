package cron

import (
	"testing"
	"time"
)

// setFastTick overrides runLoopTickInterval to 20ms for the duration of the
// test, so scheduler tests don't wait 1.5s per assertion to observe a tick.
//
// Production behavior is 100% unchanged — only the test sees the fast value.
// Original value is restored via t.Cleanup.
//
// Call this BEFORE cs.Start() — runLoop captures the var when it constructs
// the ticker, so changing it after Start() has no effect on the running loop.
func setFastTick(t *testing.T) {
	t.Helper()
	saved := runLoopTickInterval
	runLoopTickInterval = 20 * time.Millisecond
	t.Cleanup(func() { runLoopTickInterval = saved })
}
