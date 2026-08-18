package reliability

import (
	"sync"
	"time"
)

// SLOStatus is a point-in-time view of the rolling SLO evaluator.
type SLOStatus struct {
	Enabled       bool
	Window        time.Duration
	TotalRequests uint64
	Successes     uint64
	SuccessRate   float64
	Target        float64
	BurnRate      float64
	WithinBudget  bool
}

// sloSample is one windowed observation of a Metrics flush delta. `ok` records
// whether every request in that delta succeeded (no failures in the interval).
type sloSample struct {
	ok bool
	at time.Time
}

// SLOTracker evaluates a config-driven reliability SLO (error budget) over a
// rolling FIFO window of snapshot deltas. Observe folds each Metrics flush into
// the window and prunes entries older than the window, so the success rate
// reflects only recent traffic.
//
// It is nil-safe and division-safe: with no samples (or zero total requests)
// the status carries zero success-rate/burn-rate and reports WithinBudget — an
// idle gateway never burns its error budget. BurnRate is target/successRate
// and is only computed when successRate > 0.
type SLOTracker struct {
	mu      sync.Mutex
	target  float64
	window  time.Duration
	samples []sloSample
	nowFn   func() time.Time
}

// NewSLOTracker returns an SLO evaluator with the given success-rate target
// (e.g. 0.99) and rolling window. Callers pass Effective* values from
// ReliabilityConfig.SLO.
func NewSLOTracker(target float64, window time.Duration) *SLOTracker {
	return &SLOTracker{
		target: target,
		window: window,
		nowFn:  time.Now,
	}
}

// Observe folds the snapshot delta into the rolling window and prunes entries
// older than the window. A delta with zero requests is ignored — no idle
// traffic means no error-budget burn. Concurrent observes are serialized.
func (s *SLOTracker) Observe(delta Snapshot) {
	if s == nil || s.window <= 0 || delta.LLMRequests == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.addLocked(sloSample{ok: delta.LLMSuccesses >= delta.LLMRequests, at: s.nowFn()})
}

// addLocked appends a sample then prunes samples older than the window. It
// requires the caller to hold s.mu.
func (s *SLOTracker) addLocked(sample sloSample) {
	s.samples = append(s.samples, sample)
	cutoff := sample.at.Add(-s.window)
	stale := 0
	for stale < len(s.samples) && s.samples[stale].at.Before(cutoff) {
		stale++
	}
	s.samples = s.samples[stale:]
}

// Status returns the current SLO evaluation. With no samples in the window the
// success rate is zero but WithinBudget stays true — no traffic means no
// error-budget burn, so an idle gateway never pages. A nil tracker or an
// unconfigured (zero-window) tracker returns the zero SLOStatus (Enabled=false)
// so callers can distinguish "tracker not configured" from "tracker idle".
func (s *SLOTracker) Status() SLOStatus {
	if s == nil || s.window <= 0 {
		return SLOStatus{WithinBudget: true}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusLocked()
}

// statusLocked computes the status from the in-window samples. It requires the
// caller to hold s.mu.
func (s *SLOTracker) statusLocked() SLOStatus {
	st := SLOStatus{
		Enabled:       true,
		Window:        s.window,
		TotalRequests: uint64(len(s.samples)),
		Target:        s.target,
		WithinBudget:  true,
	}
	for _, sm := range s.samples {
		if sm.ok {
			st.Successes++
		}
	}
	if st.TotalRequests == 0 {
		return st
	}
	st.SuccessRate = float64(st.Successes) / float64(st.TotalRequests)
	if st.SuccessRate > 0 {
		// Burn rate is the ratio of target success-rate to actual success-rate.
		// Zero observed success rate leaves BurnRate at 0 — the zero-request
		// success-rate field and WithinBudget=false carry the outage signal, and
		// the 0 keeps the status JSON-marshalable (Inf is not).
		st.BurnRate = s.target / st.SuccessRate
	}
	st.WithinBudget = st.SuccessRate >= s.target
	return st
}

// Enabled reports whether the tracker is configured with a valid window and
// target.
func (s *SLOTracker) Enabled() bool {
	return s != nil && s.window > 0 && s.target > 0
}