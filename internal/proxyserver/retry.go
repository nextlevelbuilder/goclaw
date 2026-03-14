package proxyserver

import (
	"context"
	"log/slog"
	"math"
	"math/rand"
	"time"
)

// RetryableStatusCodes are HTTP status codes that should trigger a retry.
var RetryableStatusCodes = map[int]bool{
	408: true, // Request Timeout
	429: true, // Too Many Requests
	500: true, // Internal Server Error
	502: true, // Bad Gateway
	503: true, // Service Unavailable
	504: true, // Gateway Timeout
}

// IsRetryableStatusCode checks if a status code should trigger a retry.
func IsRetryableStatusCode(statusCode int) bool {
	return RetryableStatusCodes[statusCode]
}

// Retryer handles retry logic with exponential backoff.
type Retryer struct {
	maxAttempts     int
	baseDelay       time.Duration
	maxDelay        time.Duration
	exponentialBase float64
	logger          *slog.Logger
}

// NewRetryer creates a new retryer from config.
func NewRetryer(cfg *RetryConfig, logger *slog.Logger) *Retryer {
	return &Retryer{
		maxAttempts:     cfg.MaxAttempts,
		baseDelay:       cfg.BaseDelay,
		maxDelay:        cfg.MaxDelay,
		exponentialBase: cfg.ExponentialBase,
		logger:          logger.With("component", "retryer"),
	}
}

// GetMaxAttempts returns the maximum number of retry attempts.
func (r *Retryer) GetMaxAttempts() int {
	return r.maxAttempts
}

// CalculateDelay calculates the delay for a given attempt number (0-indexed).
func (r *Retryer) CalculateDelay(attempt int) time.Duration {
	delay := float64(r.baseDelay) * math.Pow(r.exponentialBase, float64(attempt))
	if delay > float64(r.maxDelay) {
		delay = float64(r.maxDelay)
	}
	jitter := delay * 0.1 * rand.Float64()
	delay += jitter
	return time.Duration(delay)
}

// WaitForRetry waits for the calculated delay before the next retry attempt.
func (r *Retryer) WaitForRetry(ctx context.Context, attempt int) error {
	delay := r.CalculateDelay(attempt)

	r.logger.Debug("waiting before retry",
		"attempt", attempt+2,
		"max_attempts", r.maxAttempts,
		"delay", delay)

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// ShouldRetry determines if a retry should be attempted.
func (r *Retryer) ShouldRetry(attempt int, statusCode int) bool {
	if attempt >= r.maxAttempts-1 {
		return false
	}
	return IsRetryableStatusCode(statusCode)
}
