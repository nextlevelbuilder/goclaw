package providers

import (
	"context"
	"errors"
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