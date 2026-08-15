package reliability

import (
	"sync"
	"time"
)

// CircuitState is the state of a circuit breaker for one provider:model pair.
type CircuitState int

const (
	// CircuitHealthy — normal operation, all requests allowed.
	CircuitHealthy CircuitState = iota
	// CircuitDegraded — some failures observed, still allowing requests.
	CircuitDegraded
	// CircuitOpen — too many failures; requests are rejected until cooldown expires.
	CircuitOpen
	// CircuitHalfOpen — cooldown expired; limited probe requests allowed.
	CircuitHalfOpen
)

func (s CircuitState) String() string {
	switch s {
	case CircuitHealthy:
		return "healthy"
	case CircuitDegraded:
		return "degraded"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half_open"
	default:
		return "unknown"
	}
}

// CircuitOptions configures a CircuitBreaker.
type CircuitOptions struct {
	// FailureThreshold — consecutive failures (or failures within a window)
	// before transitioning Healthy→Degraded→Open.
	FailureThreshold int
	// DegradedThreshold — failures after which Healthy becomes Degraded.
	DegradedThreshold int
	// Cooldown — how long the breaker stays Open before becoming HalfOpen.
	Cooldown time.Duration
	// HalfOpenMax — number of probe requests allowed while HalfOpen before
	// deciding to close or reopen.
	HalfOpenMax int
	// ProbeTimeout — how long a dispatched half-open probe may stay unresolved
	// before its slot is considered stale and a fresh probe is allowed. Guards
	// against wedging the breaker open if the probe holder never records an
	// outcome (crashed worker, late context cancel, decision not to send).
	ProbeTimeout time.Duration
	// MinSilence — minimum observation window semantics are intentionally
	// simple: we use consecutive-failure counting, not a sliding window.
	// This keeps the implementation deterministic and testable.
	nowFn func() time.Time
}

// DefaultCircuitOptions returns production-appropriate defaults.
func DefaultCircuitOptions() CircuitOptions {
	return CircuitOptions{
		FailureThreshold:  5,
		DegradedThreshold: 2,
		Cooldown:          30 * time.Second,
		HalfOpenMax:       1,
		ProbeTimeout:      30 * time.Second,
	}
}

// circuitEntry holds the mutable state for one key.
type circuitEntry struct {
	state             CircuitState
	consecutiveFails  int
	halfOpenProbes    int
	halfOpenSince     time.Time // when the first probe slot was consumed
	probeOutstanding  bool      // a probe was allowed but not yet resolved
	openUntil         time.Time
	lastFailureAt     time.Time
	lastSuccessAt     time.Time
}

// CircuitBreaker implements per-key (provider+model) circuit breaking.
// Thread-safe.
type CircuitBreaker struct {
	mu      sync.Mutex
	opts    CircuitOptions
	entries map[string]*circuitEntry
}

// NewCircuitBreaker creates a breaker with the given options.
func NewCircuitBreaker(opts CircuitOptions) *CircuitBreaker {
	if opts.FailureThreshold <= 0 {
		opts.FailureThreshold = 5
	}
	if opts.DegradedThreshold <= 0 || opts.DegradedThreshold >= opts.FailureThreshold {
		opts.DegradedThreshold = opts.FailureThreshold - 1
	}
	if opts.Cooldown <= 0 {
		opts.Cooldown = 30 * time.Second
	}
	if opts.HalfOpenMax <= 0 {
		opts.HalfOpenMax = 1
	}
	if opts.ProbeTimeout <= 0 {
		opts.ProbeTimeout = 30 * time.Second
	}
	if opts.nowFn == nil {
		opts.nowFn = time.Now
	}
	return &CircuitBreaker{
		opts:    opts,
		entries: make(map[string]*circuitEntry),
	}
}

// Allow reports whether a request for key may proceed right now.
// While Open, requests are rejected until the cooldown expires, at which
// point the breaker flips to HalfOpen and permits limited probes.
func (c *CircuitBreaker) Allow(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	e := c.entries[key]
	if e == nil {
		return true
	}
	now := c.opts.nowFn()
	switch e.state {
	case CircuitOpen:
		if now.Before(e.openUntil) {
			return false
		}
		// Cooldown expired → HalfOpen. Consume the first probe slot so a
		// subsequent concurrent caller cannot sneak in and probe twice.
		e.state = CircuitHalfOpen
		e.halfOpenProbes = 1
		e.probeOutstanding = true
		e.halfOpenSince = now
		return true
	case CircuitHalfOpen:
		// A stale probe (allowed but never resolved) must not wedge the breaker
		// open. If the outstanding probe exceeded its timeout, treat it as
		// released and allow a fresh probe.
		if e.probeOutstanding && now.Sub(e.halfOpenSince) >= c.opts.ProbeTimeout {
			e.probeOutstanding = true
			e.halfOpenSince = now
			return true
		}
		if e.halfOpenProbes < c.opts.HalfOpenMax {
			e.halfOpenProbes++
			e.probeOutstanding = true
			e.halfOpenSince = now
			return true
		}
		return false
	default:
		return true
	}
}

// RecordSuccess clears failures and moves the breaker toward Healthy.
func (c *CircuitBreaker) RecordSuccess(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e := c.ensure(key)
	now := c.opts.nowFn()
	e.consecutiveFails = 0
	e.lastSuccessAt = now
	switch e.state {
	case CircuitHalfOpen:
		// A successful probe closes the circuit.
		e.state = CircuitHealthy
	case CircuitOpen:
		e.state = CircuitHealthy
	case CircuitDegraded:
		e.state = CircuitHealthy
	}
	e.halfOpenProbes = 0
	e.probeOutstanding = false
	e.openUntil = time.Time{}
}

// RecordFailure records a failure, escalating the state toward Open.
func (c *CircuitBreaker) RecordFailure(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e := c.ensure(key)
	now := c.opts.nowFn()
	e.consecutiveFails++
	e.lastFailureAt = now

	switch e.state {
	case CircuitHalfOpen:
		// A failed probe reopens the circuit.
		e.state = CircuitOpen
		e.openUntil = now.Add(c.opts.Cooldown)
	case CircuitOpen:
		// Keep open, nudge cooldown for sustained failure.
		e.openUntil = now.Add(c.opts.Cooldown)
	case CircuitDegraded, CircuitHealthy:
		if e.consecutiveFails >= c.opts.FailureThreshold {
			e.state = CircuitOpen
			e.openUntil = now.Add(c.opts.Cooldown)
		} else if e.consecutiveFails >= c.opts.DegradedThreshold {
			e.state = CircuitDegraded
		}
	}
	e.probeOutstanding = false
}

// State returns the current circuit state for a key (creating it if absent).
func (c *CircuitBreaker) State(key string) CircuitState {
	c.mu.Lock()
	defer c.mu.Unlock()

	e := c.entries[key]
	if e == nil {
		return CircuitHealthy
	}
	return e.state
}

// NextRetryAt returns when the circuit will leave Open for a key, or zero time
// if the circuit is not open.
func (c *CircuitBreaker) NextRetryAt(key string) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	e := c.entries[key]
	if e == nil || e.state != CircuitOpen {
		return time.Time{}
	}
	return e.openUntil
}

func (c *CircuitBreaker) ensure(key string) *circuitEntry {
	e, ok := c.entries[key]
	if !ok {
		e = &circuitEntry{state: CircuitHealthy}
		c.entries[key] = e
	}
	return e
}