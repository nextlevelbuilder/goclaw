package cron

import (
	"math/rand/v2"
	"time"
)

// RetryConfig controls exponential backoff retry for failed cron jobs.
type RetryConfig struct {
	MaxRetries int           // max retry attempts (default 3, 0 = no retry)
	BaseDelay  time.Duration // initial backoff delay (default 2s)
	MaxDelay   time.Duration // maximum backoff delay (default 30s)
}

// DefaultRetryConfig returns sensible defaults.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries: 3,
		BaseDelay:  2 * time.Second,
		MaxDelay:   30 * time.Second,
	}
}

// ExecuteWithRetry runs fn, retrying on error with exponential backoff + jitter.
// Returns the first successful result or the last error after all retries.
func ExecuteWithRetry(fn func() (string, error), cfg RetryConfig) (result string, attempts int, err error) {
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		result, err = fn()
		if err == nil {
			return result, attempt + 1, nil
		}

		if attempt < cfg.MaxRetries {
			delay := backoffWithJitter(cfg.BaseDelay, cfg.MaxDelay, attempt)
			time.Sleep(delay)
		}
	}
	return "", cfg.MaxRetries + 1, err
}

// backoffWithJitter computes delay = min(base * 2^attempt, max) + jitter(±25%).
func backoffWithJitter(base, max time.Duration, attempt int) time.Duration {
	delay := min(
		// base * 2^attempt
		base<<uint(attempt), max)

	// Jitter: ±25% of delay
	quarter := delay / 4
	if quarter > 0 {
		jitter := time.Duration(rand.Int64N(int64(quarter*2))) - quarter
		delay += jitter
	}

	return delay
}

// maxOutputBytes is the truncation limit for cron job output (16KB).
// Prevents storing excessively large results in run logs.
const maxOutputBytes = 16 * 1024

// TruncateOutput truncates output to maxOutputBytes, appending "..." if truncated.
func TruncateOutput(s string) string {
	if len(s) <= maxOutputBytes {
		return s
	}
	return s[:maxOutputBytes] + "...[truncated]"
}
