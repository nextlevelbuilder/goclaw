// Package permissions — Denial tracking (CP-06).
// Prevents "permission fatigue" — when auto-classifier denies the same
// pattern repeatedly, fall back to prompting the user.
package permissions

import "sync/atomic"

// DenialTracker detects repeated auto-denials and falls back to user prompt.
//
// Logic: if the classifier denies 3 consecutive times OR 20 total times,
// the classifier may be wrong → ask the user instead of auto-denying.
type DenialTracker struct {
	consecutive atomic.Int32
	total       atomic.Int32

	maxConsecutive int32
	maxTotal       int32
}

// NewDenialTracker creates a tracker with default thresholds.
func NewDenialTracker() *DenialTracker {
	return &DenialTracker{
		maxConsecutive: 3,
		maxTotal:       20,
	}
}

// NewDenialTrackerWithLimits creates a tracker with custom thresholds.
func NewDenialTrackerWithLimits(maxConsecutive, maxTotal int) *DenialTracker {
	return &DenialTracker{
		maxConsecutive: int32(maxConsecutive),
		maxTotal:       int32(maxTotal),
	}
}

// RecordDenial increments denial counters.
// Returns true if the system should fall back to user prompting
// (too many denials → classifier may be wrong).
func (dt *DenialTracker) RecordDenial() bool {
	c := dt.consecutive.Add(1)
	t := dt.total.Add(1)
	return c >= dt.maxConsecutive || t >= dt.maxTotal
}

// RecordSuccess resets the consecutive counter (total is kept).
func (dt *DenialTracker) RecordSuccess() {
	dt.consecutive.Store(0)
}

// Reset clears all counters (e.g., on new session).
func (dt *DenialTracker) Reset() {
	dt.consecutive.Store(0)
	dt.total.Store(0)
}

// Stats returns current denial statistics.
func (dt *DenialTracker) Stats() (consecutive, total int) {
	return int(dt.consecutive.Load()), int(dt.total.Load())
}
