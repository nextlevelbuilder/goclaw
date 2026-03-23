package providers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"time"
)

// RetryConfig configures retry behavior for provider requests.
type RetryConfig struct {
	Attempts int           // max attempts (default 3, 1 = no retry)
	MinDelay time.Duration // initial delay (default 300ms)
	MaxDelay time.Duration // delay cap (default 30s)
	Jitter   float64       // jitter factor ±N (default 0.1 = ±10%)
}

// RetryHookFunc is called before each retry attempt.
// attempt is the failed attempt number (1-based), maxAttempts is the total.
type RetryHookFunc func(attempt, maxAttempts int, err error)

type retryHookKey struct{}

// WithRetryHook injects a retry notification callback into the context.
// RetryDo will call this hook before each retry attempt.
func WithRetryHook(ctx context.Context, fn RetryHookFunc) context.Context {
	return context.WithValue(ctx, retryHookKey{}, fn)
}

// retryHookFromContext returns the retry hook from context, or nil.
func retryHookFromContext(ctx context.Context) RetryHookFunc {
	fn, _ := ctx.Value(retryHookKey{}).(RetryHookFunc)
	return fn
}

// DefaultRetryConfig returns sensible defaults matching TS provider retry behavior.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		Attempts: 3,
		MinDelay: 300 * time.Millisecond,
		MaxDelay: 30 * time.Second,
		Jitter:   0.1,
	}
}

// HTTPError represents an HTTP error with status code and optional Retry-After.
type HTTPError struct {
	Status     int
	Body       string
	RetryAfter time.Duration // parsed from Retry-After header (0 if absent)
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body)
}

// IsRetryableError checks if an error is retryable.
// Retryable: 429 (rate limit), 500, 502, 503, 504, connection errors, timeouts.
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for HTTPError
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		switch httpErr.Status {
		case 429, 500, 502, 503, 504:
			return true
		}
		return false
	}

	// Check for network errors
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true // includes timeouts
	}

	// Check for connection reset / broken pipe / EOF in error string
	errStr := err.Error()
	if strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "EOF") ||
		strings.Contains(errStr, "timeout") {
		return true
	}

	return false
}

// IsContextOverflowError checks if an error indicates a context window/length limit has been exceeded.
// Recognizes common provider strings for context window exhaustion across multiple LLM backends.
func IsContextOverflowError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	// Match standard context window stop reason strings and provider-specific error variants.
	return strings.Contains(lower, "context_window_exceeded") ||
		strings.Contains(lower, "context_length_exceeded") ||
		strings.Contains(lower, "context_overflow") ||
		strings.Contains(lower, "too many tokens") ||
		strings.Contains(lower, "maximum context length")
}

// RetryDo executes fn with retry logic using exponential backoff and jitter.
func RetryDo[T any](ctx context.Context, cfg RetryConfig, fn func() (T, error)) (T, error) {
	if cfg.Attempts <= 0 {
		cfg.Attempts = 1
	}

	var lastErr error
	var zero T

	for attempt := 1; attempt <= cfg.Attempts; attempt++ {
		result, err := fn()
		if err == nil {
			return result, nil
		}

		lastErr = err

		// Don't retry if not retryable or last attempt
		if !IsRetryableError(err) || attempt == cfg.Attempts {
			return zero, err
		}

		// Compute delay
		delay := computeDelay(cfg, attempt, err)

		slog.Debug("provider retry",
			"attempt", attempt,
			"maxAttempts", cfg.Attempts,
			"delay", delay,
			"error", err.Error(),
		)

		// Notify retry hook (for placeholder updates, etc.)
		if hook := retryHookFromContext(ctx); hook != nil {
			hook(attempt, cfg.Attempts, err)
		}

		// Wait with context cancellation support
		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-time.After(delay):
		}
	}

	return zero, lastErr
}

// computeDelay calculates the retry delay with exponential backoff, jitter, and Retry-After support.
func computeDelay(cfg RetryConfig, attempt int, err error) time.Duration {
	// Check for Retry-After header
	var httpErr *HTTPError
	if errors.As(err, &httpErr) && httpErr.RetryAfter > 0 {
		return httpErr.RetryAfter
	}

	// Exponential backoff: minDelay * 2^(attempt-1)
	delay := float64(cfg.MinDelay) * math.Pow(2, float64(attempt-1))

	// Cap at maxDelay
	if time.Duration(delay) > cfg.MaxDelay {
		delay = float64(cfg.MaxDelay)
	}

	// Apply jitter: ±jitter%
	if cfg.Jitter > 0 {
		jitterRange := delay * cfg.Jitter
		delay += (rand.Float64()*2 - 1) * jitterRange
	}

	if delay < 0 {
		delay = float64(cfg.MinDelay)
	}

	return time.Duration(delay)
}

// ParseRetryAfter parses a Retry-After header value (seconds or HTTP-date).
func ParseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}

	// Try integer seconds first
	if seconds, err := strconv.Atoi(value); err == nil {
		return time.Duration(seconds) * time.Second
	}

	// Try HTTP-date format
	if t, err := time.Parse(time.RFC1123, value); err == nil {
		d := time.Until(t)
		if d > 0 {
			return d
		}
	}

	return 0
}
