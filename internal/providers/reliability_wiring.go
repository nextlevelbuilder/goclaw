package providers

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/reliability"
)

// This file is the single place provider code touches the reliability layer
// (internal/reliability). It exposes nil-safe helpers so a reliability bug can
// never break a provider request: every call is guarded with a recovery-free
// nil check on the singleton and its components.
//
// Direction of imports: providers -> reliability only (reliability does not
// import providers), so wiring here cannot create an import cycle.

// observeSuccess records a successful LLM call in the health registry, the
// circuit breaker and the metrics counters. It never fails.
func observeSuccess(provider, model string) {
	reg := reliability.Default()
	if reg == nil {
		return
	}
	safeRecord(func() {
		if reg.Metrics != nil {
			reg.Metrics.RecordLLMSuccess()
		}
		if reg.Health != nil {
			reg.Health.ObserveSuccess(provider, model)
		}
		if reg.Breaker != nil {
			reg.Breaker.RecordSuccess(provider + ":" + model)
		}
	})
}

// observeFailure records a failed LLM call, classifying the error with the
// reliability taxonomy so the health registry, breaker and metrics counters
// reflect the failure kind. It never fails.
func observeFailure(provider, model string, err error) {
	reg := reliability.Default()
	if reg == nil {
		return
	}
	classified, _ := reliability.ClassifyError(err)
	code := reliability.ErrProviderServerError
	if classified != nil {
		code = classified.Code
	}
	safeRecord(func() {
		if reg.Health != nil {
			reg.Health.ObserveFailure(provider, model, code)
		}
		// Health.ObserveFailure already records the failure on the circuit
		// breaker keyed by provider:model — do not record it again here.
		if reg.Metrics != nil {
			switch code {
			case reliability.ErrProviderRateLimited:
				reg.Metrics.RecordLLMRateLimited()
			case reliability.ErrProviderTimeout:
				reg.Metrics.RecordLLMTimeout()
			case reliability.ErrProviderServerError, reliability.ErrProviderOverloaded:
				reg.Metrics.RecordLLMServerError()
			}
		}
	})
}

// circuitAllow reports whether a request for provider:model may proceed.
// It returns nil when allowed; when the circuit is Open (or probe slots are
// exhausted) it returns a *reliability.ReliabilityError carrying the breaker's
// NextRetryAt as RetryAfter. Non-blocking.
func circuitAllow(provider, model string) error {
	reg := reliability.Default()
	if reg == nil || reg.Breaker == nil {
		return nil
	}
	key := provider + ":" + model
	if reg.Breaker.Allow(key) {
		return nil
	}
	retryAfter := reg.Breaker.NextRetryAt(key)
	if retryAfter.IsZero() {
		return nil // cooldown expired between Allow and NextRetryAt — let the request through
	}
	code := reliability.ErrProviderOverloaded
	if reg.Breaker.State(key) != reliability.CircuitOpen {
		// Concurrency-cap (HalfOpen probe exhausted), not full overload: a
		// single-flight-probe rejection is best framed as a rate-limit hold.
		code = reliability.ErrProviderRateLimited
	}
	return reliability.New(code, "circuit breaker rejecting "+key).
		WithRetryAfter(time.Until(retryAfter))
}

// waitRateLimit blocks until the shared rate-limit cooldown for
// provider:model expires or ctx is done. It returns the cooldown error
// (context.Canceled / context.DeadlineExceeded) when the wait is interrupted,
// and nil when no cooldown is active or it expired. Nil-safe.
func waitRateLimit(ctx context.Context, provider, model string) error {
	reg := reliability.Default()
	if reg == nil || reg.RateLimit == nil {
		return nil
	}
	return reg.RateLimit.Wait(ctx, provider, model)
}

// safeRecord runs fn guarded against panic so a reliability-layer defect can
// never take down a provider request. The panic is swallowed; the outage shows
// up as missing metrics rather than a failed request.
func safeRecord(fn func()) {
	defer func() {
		_ = recover()
	}()
	fn()
}

// record429Cooldown registers a rate-limit event on the shared coordinator so
// concurrent runs for the same provider:model wait instead of retry-storming.
// Nil-safe; retryAfter <= 0 falls back to the coordinator's default.
func record429Cooldown(provider, model string, retryAfter time.Duration) {
	reg := reliability.Default()
	if reg == nil || reg.RateLimit == nil {
		return
	}
	safeRecord(func() { reg.RateLimit.Record429(provider, model, retryAfter) })
}

// isRateLimitedErr reports whether err is a 429 / rate-limit error.
func isRateLimitedErr(err error) bool {
	if err == nil {
		return false
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.Status == 429
	}
	classified, _ := reliability.ClassifyError(err)
	return classified != nil && classified.Code == reliability.ErrProviderRateLimited
}

// rateLimitRetryAfter extracts the Retry-After hint from a rate-limit error,
// or 0 when absent.
func rateLimitRetryAfter(err error) time.Duration {
	var httpErr *HTTPError
	if errors.As(err, &httpErr) && httpErr.RetryAfter > 0 {
		return httpErr.RetryAfter
	}
	return 0
}

// ---------------------------------------------------------------------------
// Stream watchdog (streaming idle / first-byte timeouts)
// ---------------------------------------------------------------------------

// streamTimeoutConfig is the per-provider idle and first-byte timeout view read
// from the reliability runtime at request time. Both durations are 0 when
// disabled.
type streamTimeoutConfig struct {
	idle      time.Duration
	firstByte time.Duration
}

// streamWatchdogKind identifies which watchdog deadline fired. The zero value
// is unknown, so a fired watchdog can always distinguish itself from a plain
// parent cancellation.
type streamWatchdogKind int

const (
	streamWatchdogNone streamWatchdogKind = iota
	streamWatchdogIdle
	streamWatchdogFirstByte
)

// streamWatchdogState is the shared state for one request's watchdog. It is
// deliberately small and allocation-free: a watchdogCtx plus a mutexed
// deadline, both created up front, so no per-event heap allocation is needed.
type streamWatchdogState struct {
	ctx *watchdogCtx

	mu       sync.Mutex
	deadline time.Time          // zero = no deadline armed
	kind     streamWatchdogKind // kind of deadline above
}

// watchdogCtx is a context that fires when the watchdog deadline passes. It
// observes its parent: when the parent is cancelled first, the watchdog
// becomes inert — it can never fire against a cancelled parent, so a parent
// cancellation can never be misreported as a stream stall.
type watchdogCtx struct {
	done   chan struct{}
	parent context.Context
	kind   streamWatchdogKind
}

func (w *watchdogCtx) Deadline() (time.Time, bool) { return time.Time{}, false }
func (w *watchdogCtx) Done() <-chan struct{}       { return w.done }
func (w *watchdogCtx) Err() error                  { return context.Canceled }
func (w *watchdogCtx) Value(key any) any           { return w.parent.Value(key) }

// streamWatchdogStalled reports whether the ctx returned by
// streamWatchdogContext fired because of a watchdog deadline. It returns
// (idle, true) for an idle timeout, (firstByte, true) for a first-byte
// timeout, and (none, false) otherwise — including when the watchdog was
// cancelled (kind is written before the done channel is closed, so a closed
// done with no kind means the watch was cancelled, never a stall). Callers can
// treat ctx.Err() as their own signal for parent cancellations.
func streamWatchdogStalled(ctx context.Context) (streamWatchdogKind, bool) {
	w, ok := ctx.(*watchdogCtx)
	if !ok || w == nil {
		return streamWatchdogNone, false
	}
	select {
	case <-w.done:
		if w.kind == streamWatchdogNone {
			return streamWatchdogNone, false // cancelled, not stalled
		}
		return w.kind, true
	default:
		return streamWatchdogNone, false
	}
}

// streamWatchdogContext derives a context from parent that is cancelled by the
// watchdog when no data event arrives within idle (reset per event via reset())
// or when the first event does not arrive within firstByte. A component <= 0
// disables it; when both are <= 0 the helper is a no-op and returns parent with
// nil reset/cancel. The returned cancel stops the watchdog; it is idempotent
// and safe to call twice.
//
// The reset function re-arms the idle deadline from the time it is called:
// call it after every successfully parsed SSE event. It is a no-op after the
// watchdog has fired or been cancelled. The first-byte watchdog (when enabled)
// is armed exactly once on creation and never re-armed.
//
// The watchdog waits on a single re-armed timer. Each reset() delivers a fresh
// deadline; if the reset is delivered while the timer is running, the timer is
// re-created for the later deadline, so a superseded deadline can never cancel
// the stream. Nothing (including a past-due stale deadline) fires with a
// kind other than the deadline it was armed with. The watchdog goroutine
// always exits: on fire, on cancel, on parent completion — a cancelled parent
// can never misreport a stall.
func streamWatchdogContext(parent context.Context, idle, firstByte time.Duration) (context.Context, func(), func()) {
	if parent == nil {
		parent = context.Background()
	}
	if idle <= 0 && firstByte <= 0 {
		return parent, nil, nil
	}

	state := &streamWatchdogState{
		ctx: &watchdogCtx{
			done:   make(chan struct{}),
			parent: parent,
		},
	}
	var once sync.Once
	fire := func(kind streamWatchdogKind) {
		once.Do(func() {
			state.ctx.kind = kind
			close(state.ctx.done)
		})
	}
	cancel := func() {
		once.Do(func() {
			close(state.ctx.done)
		})
	}

	// setDeadline arms (or re-arms) the current deadline under the state mutex.
	setDeadline := func(d time.Time, idleKind bool) {
		state.mu.Lock()
		state.deadline = d
		if idleKind {
			state.kind = streamWatchdogIdle
		} else {
			state.kind = streamWatchdogFirstByte
		}
		state.mu.Unlock()
	}

	// resetCh wakes the watchdog goroutine whenever a new deadline is armed so
	// it can re-create its timer. Buffered so reset() never blocks; stale
	// wakeups are harmless (the goroutine re-reads the deadline).
	resetCh := make(chan struct{}, 1)
	wake := func() {
		select {
		case resetCh <- struct{}{}:
		default:
		}
	}

	switch {
	case firstByte > 0:
		// First-byte deadline is armed exactly once, before any reset can
		// supersede it with an idle deadline.
		setDeadline(time.Now().Add(firstByte), false)
	case idle > 0:
		// No first-byte deadline configured: arm the idle deadline from stream
		// start so a connection that never delivers a single event (headers
		// received, silence forever) still fires the watchdog.
		setDeadline(time.Now().Add(idle), true)
	}

	go func() {
		var timer *time.Timer
		var timerC <-chan time.Time
		stopTimer := func() {
			if timer != nil {
				timer.Stop()
				timer = nil
				timerC = nil
			}
		}
		defer stopTimer()

		for {
			state.mu.Lock()
			d := state.deadline
			kind := state.kind
			state.mu.Unlock()
			if d.IsZero() {
				// Nothing armed (idle-only watchdog before... nothing) — wait
				// for a wakeup or shutdown.
				select {
				case <-state.ctx.done:
					return
				case <-parent.Done():
					return
				case <-resetCh:
					continue
				}
			}

			remaining := time.Until(d)
			if remaining <= 0 {
				fire(kind)
				return
			}
			stopTimer()
			timer = time.NewTimer(remaining)
			timerC = timer.C

			select {
			case <-state.ctx.done:
				return
			case <-parent.Done():
				return
			case <-timerC:
				// Re-check under the mutex: the deadline may have been re-armed
				// while the timer was running, in which case the timer is stale.
				state.mu.Lock()
				cur := state.deadline
				curKind := state.kind
				state.mu.Unlock()
				if !cur.Equal(d) || cur.IsZero() {
					continue // superseded — re-arm for the new deadline
				}
				fire(curKind)
				return
			case <-resetCh:
				// New deadline arrived — loop and re-arm.
			}
		}
	}()

	reset := func() {
		if idle <= 0 {
			return
		}
		select {
		case <-state.ctx.done:
			return // watchdog already fired or cancelled — no re-arming
		default:
		}
		setDeadline(time.Now().Add(idle), true)
		wake()
	}

	return state.ctx, reset, cancel
}

// streamTimeoutCtxKey carries a per-request stream timeout override in the
// context. It exists so tests (and callers that need request-scoped timeouts)
// can arm the watchdog without touching the process-wide runtime knobs.
type streamTimeoutCtxKey struct{}

// WithStreamTimeouts attaches a per-request stream watchdog override to ctx.
// A component <= 0 keeps the runtime default for that component; a value > 0
// replaces it. The override is per-request only and never mutates the runtime.
func WithStreamTimeouts(ctx context.Context, idle, firstByte time.Duration) context.Context {
	if ctx == nil {
		return ctx
	}
	return context.WithValue(ctx, streamTimeoutCtxKey{}, streamTimeoutConfig{
		idle:      idle,
		firstByte: firstByte,
	})
}

// streamTimeoutConfigFor reads the effective stream timeouts for a provider
// request: the runtime knobs (stream.idle_timeout_ms / first_byte_timeout_ms)
// with an optional per-model override (ModelSpec.StreamTimeoutMs) applied to
// the idle timeout. A per-request override (WithStreamTimeouts) wins over
// both. Everything is nil-safe and forward-compatible: when the runtime has
// no Stream field yet, or the model spec has no override, the relevant
// duration is 0 (disabled).
func streamTimeoutConfigFor(ctx context.Context, provider, model string, registry ModelRegistry) streamTimeoutConfig {
	cfg := streamTimeoutConfig{}
	reg := reliability.Default()
	if reg != nil {
		cfg.idle, cfg.firstByte = streamDurationsFromRuntime(reg)
	}
	if ctx != nil {
		if ov, ok := ctx.Value(streamTimeoutCtxKey{}).(streamTimeoutConfig); ok {
			if ov.idle > 0 {
				cfg.idle = ov.idle
			}
			if ov.firstByte > 0 {
				cfg.firstByte = ov.firstByte
			}
		}
	}
	if registry != nil {
		if spec := registry.Resolve(provider, model); spec != nil {
			if ms := modelStreamTimeoutMs(spec); ms > 0 {
				cfg.idle = time.Duration(ms) * time.Millisecond
			}
		}
	}
	return cfg
}

// streamDurationsFromRuntime reads StreamOptions{IdleTimeout, FirstByteTimeout}
// off the reliability Runtime via reflection. The field is owned by the
// reliability/config package (this phase's other lane); reflecting keeps this
// package compilable and nil-safe whether or not the field exists yet, and
// reads it exactly once per request.
func streamDurationsFromRuntime(reg *reliability.Runtime) (idle, firstByte time.Duration) {
	if reg == nil {
		return 0, 0
	}
	v := reflect.ValueOf(reg)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return 0, 0
	}
	s := v.Elem().FieldByName("Stream")
	if !s.IsValid() || !s.CanInterface() {
		return 0, 0
	}
	idle, _ = s.FieldByName("IdleTimeout").Interface().(time.Duration)
	firstByte, _ = s.FieldByName("FirstByteTimeout").Interface().(time.Duration)
	if idle < 0 {
		idle = 0
	}
	if firstByte < 0 {
		firstByte = 0
	}
	return idle, firstByte
}

// modelStreamTimeoutMs returns the per-model idle-timeout override from a
// ModelSpec, or 0 when the field is absent (0 = inherit the runtime value).
func modelStreamTimeoutMs(spec *ModelSpec) int {
	if spec == nil {
		return 0
	}
	v := reflect.ValueOf(spec)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return 0
	}
	f := v.Elem().FieldByName("StreamTimeoutMs")
	if !f.IsValid() || !f.CanInterface() {
		return 0
	}
	ms, _ := f.Interface().(int)
	if ms < 0 {
		return 0
	}
	return ms
}

// streamWatchdogError builds the canonical timeout error for a stalled stream.
// It is created once per stalled request so the message is stable across
// callers (loop retry, failover, pipeline).
func streamWatchdogError(provider, model string, kind streamWatchdogKind) *reliability.ReliabilityError {
	msg := "stream idle timeout: " + provider + "/" + model
	if kind == streamWatchdogFirstByte {
		msg = "stream first-byte timeout: " + provider + "/" + model
	}
	return reliability.New(reliability.ErrProviderTimeout, msg)
}

// observeStreamStall records a stalled stream on the health registry and the
// metrics counters. It is nil-safe and never fails, mirroring observeFailure.
// Callers must record a stall at most once per request (guard with the
// watchdog fire path).
func observeStreamStall(provider, model string) {
	reg := reliability.Default()
	if reg == nil {
		return
	}
	safeRecord(func() {
		if reg.Health != nil {
			reg.Health.ObserveStreamStall(provider, model)
		}
		if reg.Metrics != nil {
			reg.Metrics.RecordLLMStreamStall()
		}
	})
}

