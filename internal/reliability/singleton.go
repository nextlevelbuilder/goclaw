package reliability

import (
	"sync"
)

// Runtime bundles the process-wide reliability components so consumers (agent
// loop, provider adapters, health commands) can reach one shared instance
// through Default() instead of each constructing their own. The bundle is
// rebuilt once at gateway startup from configuration via Configure(); tests
// may call Configure() again to swap in a fresh bundle.
type Runtime struct {
	Breaker   *CircuitBreaker
	Health    *HealthRegistry
	RateLimit *RateLimitCoordinator
	Metrics   *Metrics
}

var (
	// once lazily constructs the default bundle on the first Default() call.
	// It is never reset: Configure() only replaces curRuntime under mu, and
	// the once closure double-checks before constructing, so the two cannot
	// race or double-construct.
	once sync.Once
	// mu guards curRuntime for both reads and writes.
	mu         sync.RWMutex
	curRuntime *Runtime
)

// defaultRuntime builds the process default bundle. The circuit breaker uses
// the production defaults (5 consecutive failures → open, 2 → degraded,
// 30s cooldown, max 1 half-open probe with a 30s stale-probe timeout); the
// health registry shares the same breaker; the rate-limit coordinator is
// unlimited (maxPending 0); metrics start with a fresh recorder.
func defaultRuntime() *Runtime {
	breaker := NewCircuitBreaker(DefaultCircuitOptions())
	return &Runtime{
		Breaker:   breaker,
		Health:    NewHealthRegistry(breaker),
		RateLimit: NewRateLimitCoordinator(0),
		Metrics:   NewMetrics(),
	}
}

// Default returns the current process-wide reliability bundle, lazily
// constructed with the default options on first use. Callers must not retain
// the returned bundle across a Configure() call — treat it as the current
// view.
func Default() *Runtime {
	once.Do(func() {
		mu.Lock()
		defer mu.Unlock()
		if curRuntime == nil {
			curRuntime = defaultRuntime()
		}
	})
	mu.RLock()
	defer mu.RUnlock()
	return curRuntime
}

// Configure rebuilds the process-wide reliability bundle with the given
// circuit options and rate-limit pending cap (maxPending <= 0 disables the
// cap). It atomically replaces the previous bundle; references obtained
// earlier continue to work on their old state. The gateway calls this once at
// startup from its configuration; tests may call it multiple times to swap in
// fresh state between cases.
func Configure(opts CircuitOptions, maxPending int) *Runtime {
	r := &Runtime{
		Breaker:   NewCircuitBreaker(opts),
		RateLimit: NewRateLimitCoordinator(maxPending),
		Metrics:   NewMetrics(),
	}
	r.Health = NewHealthRegistry(r.Breaker)

	mu.Lock()
	curRuntime = r
	mu.Unlock()
	return r
}