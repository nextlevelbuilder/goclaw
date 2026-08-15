package webhooks

import (
	"math/rand/v2"
	"time"
)

// backoffSchedule is the fixed delay table indexed by attempt number (0-based).
// Attempt 0 → 30s, 1 → 2m, 2 → 10m, 3 → 1h, 4 → 6h.
// After attempt 4 the row is moved to status=dead.
var backoffSchedule = []time.Duration{
	30 * time.Second,
	2 * time.Minute,
	10 * time.Minute,
	1 * time.Hour,
	6 * time.Hour,
}

// MaxAttempts is the total number of delivery attempts (initial + retries) before
// a call moves to status=dead. After MaxAttempts-1 consecutive failures the row
// is marked dead and no further delivery is attempted.
const MaxAttempts = 5

// DelayFor returns the back-off duration for the given attempt number with ±10% jitter.
// attempt is the number of attempts already made (pre-send count).
// If attempt >= len(backoffSchedule) the last bucket is used (6h).
func DelayFor(attempt int) time.Duration {
	idx := max(attempt, 0)
	if idx >= len(backoffSchedule) {
		idx = len(backoffSchedule) - 1
	}
	base := backoffSchedule[idx]

	// ±10% jitter: multiply by a factor in [0.90, 1.10].
	jitterFactor := 0.90 + rand.Float64()*0.20 //nolint:gosec — non-crypto jitter
	return time.Duration(float64(base) * jitterFactor)
}
