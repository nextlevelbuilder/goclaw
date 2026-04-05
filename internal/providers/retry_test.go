package providers

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// --- IsRetryableError ---

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"http_429_rate_limit", &HTTPError{Status: 429}, true},
		{"http_500_server_error", &HTTPError{Status: 500}, true},
		{"http_502_bad_gateway", &HTTPError{Status: 502}, true},
		{"http_503_unavailable", &HTTPError{Status: 503}, true},
		{"http_504_timeout", &HTTPError{Status: 504}, true},
		{"http_400_bad_request", &HTTPError{Status: 400}, false},
		{"http_401_unauthorized", &HTTPError{Status: 401}, false},
		{"http_403_forbidden", &HTTPError{Status: 403}, false},
		{"http_404_not_found", &HTTPError{Status: 404}, false},
		{"connection_reset", errors.New("connection reset by peer"), true},
		{"broken_pipe", errors.New("write: broken pipe"), true},
		{"eof", errors.New("unexpected EOF"), true},
		{"timeout_string", errors.New("i/o timeout"), true},
		{"generic_error", errors.New("something went wrong"), false},
		{"wrapped_retryable", fmt.Errorf("provider: %w", &HTTPError{Status: 429}), true},
		{"wrapped_non_retryable", fmt.Errorf("provider: %w", &HTTPError{Status: 400}), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsRetryableError(tt.err)
			if got != tt.want {
				t.Fatalf("IsRetryableError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// --- computeDelay ---

func TestComputeDelay_ExponentialBackoff(t *testing.T) {
	cfg := RetryConfig{
		MinDelay: 100 * time.Millisecond,
		MaxDelay: 10 * time.Second,
		Jitter:   0, // no jitter for deterministic test
	}
	err := &HTTPError{Status: 500}

	// attempt 1: 100ms * 2^0 = 100ms
	d1 := computeDelay(cfg, 1, err)
	if d1 != 100*time.Millisecond {
		t.Fatalf("attempt 1: got %v, want 100ms", d1)
	}

	// attempt 2: 100ms * 2^1 = 200ms
	d2 := computeDelay(cfg, 2, err)
	if d2 != 200*time.Millisecond {
		t.Fatalf("attempt 2: got %v, want 200ms", d2)
	}

	// attempt 3: 100ms * 2^2 = 400ms
	d3 := computeDelay(cfg, 3, err)
	if d3 != 400*time.Millisecond {
		t.Fatalf("attempt 3: got %v, want 400ms", d3)
	}

	// attempt 4: 100ms * 2^3 = 800ms
	d4 := computeDelay(cfg, 4, err)
	if d4 != 800*time.Millisecond {
		t.Fatalf("attempt 4: got %v, want 800ms", d4)
	}
}

func TestComputeDelay_CappedAtMaxDelay(t *testing.T) {
	cfg := RetryConfig{
		MinDelay: 1 * time.Second,
		MaxDelay: 5 * time.Second,
		Jitter:   0,
	}
	err := &HTTPError{Status: 500}

	// attempt 10: 1s * 2^9 = 512s → capped at 5s
	d := computeDelay(cfg, 10, err)
	if d != 5*time.Second {
		t.Fatalf("attempt 10: got %v, want 5s (capped)", d)
	}
}

func TestComputeDelay_JitterRange(t *testing.T) {
	cfg := RetryConfig{
		MinDelay: 1 * time.Second,
		MaxDelay: 30 * time.Second,
		Jitter:   0.25, // ±25%
	}
	err := &HTTPError{Status: 500}

	// attempt 1: base = 1s, jitter ±25% → [750ms, 1250ms]
	min := 750 * time.Millisecond
	max := 1250 * time.Millisecond

	for range 100 {
		d := computeDelay(cfg, 1, err)
		if d < min || d > max {
			t.Fatalf("jitter out of range: got %v, want [%v, %v]", d, min, max)
		}
	}
}

func TestComputeDelay_NeverNegative(t *testing.T) {
	cfg := RetryConfig{
		MinDelay: 10 * time.Millisecond,
		MaxDelay: 100 * time.Millisecond,
		Jitter:   0.9, // extreme jitter
	}
	err := &HTTPError{Status: 500}

	for range 200 {
		d := computeDelay(cfg, 1, err)
		if d < 0 {
			t.Fatalf("negative delay: %v", d)
		}
	}
}

func TestComputeDelay_RetryAfterOverride(t *testing.T) {
	cfg := RetryConfig{
		MinDelay: 100 * time.Millisecond,
		MaxDelay: 30 * time.Second,
		Jitter:   0.1,
	}
	// HTTPError with RetryAfter should override computed delay
	err := &HTTPError{Status: 429, RetryAfter: 42 * time.Second}

	d := computeDelay(cfg, 1, err)
	if d != 42*time.Second {
		t.Fatalf("expected Retry-After override: got %v, want 42s", d)
	}
}

// --- ParseRetryAfter ---

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"empty", "", 0},
		{"integer_seconds", "30", 30 * time.Second},
		{"zero", "0", 0},
		{"negative_int", "-5", -5 * time.Second}, // strconv.Atoi succeeds → returns negative duration (caller should clamp)
		{"non_numeric", "abc", 0},                // neither int nor date
		{"float", "1.5", 0},                      // not a valid int
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseRetryAfter(tt.value)
			if got != tt.want {
				t.Fatalf("ParseRetryAfter(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestParseRetryAfter_RFC1123(t *testing.T) {
	// Use a future date so time.Until() returns positive
	future := time.Now().Add(60 * time.Second).UTC().Format(time.RFC1123)
	got := ParseRetryAfter(future)
	// Should be roughly 60s (allow ±5s tolerance for test execution time)
	if got < 55*time.Second || got > 65*time.Second {
		t.Fatalf("RFC1123 parse: got %v, want ~60s", got)
	}
}

func TestParseRetryAfter_PastDate(t *testing.T) {
	past := time.Now().Add(-60 * time.Second).UTC().Format(time.RFC1123)
	got := ParseRetryAfter(past)
	if got != 0 {
		t.Fatalf("past date should return 0, got %v", got)
	}
}

// --- RetryDo ---

func TestRetryDo_SuccessOnFirstAttempt(t *testing.T) {
	cfg := RetryConfig{Attempts: 3, MinDelay: time.Millisecond}
	var calls int
	result, err := RetryDo(context.Background(), cfg, func() (string, error) {
		calls++
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Fatalf("got %q, want %q", result, "ok")
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestRetryDo_SuccessAfterRetries(t *testing.T) {
	cfg := RetryConfig{Attempts: 3, MinDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond}
	var calls int
	result, err := RetryDo(context.Background(), cfg, func() (string, error) {
		calls++
		if calls < 3 {
			return "", &HTTPError{Status: 500, Body: "server error"}
		}
		return "recovered", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "recovered" {
		t.Fatalf("got %q, want %q", result, "recovered")
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}
}

func TestRetryDo_NonRetryableError_NoRetry(t *testing.T) {
	cfg := RetryConfig{Attempts: 5, MinDelay: time.Millisecond}
	var calls int
	_, err := RetryDo(context.Background(), cfg, func() (string, error) {
		calls++
		return "", &HTTPError{Status: 400, Body: "bad request"}
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("non-retryable error should not retry: got %d calls, want 1", calls)
	}
}

func TestRetryDo_MaxAttemptsExhausted(t *testing.T) {
	cfg := RetryConfig{Attempts: 3, MinDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond}
	var calls int
	_, err := RetryDo(context.Background(), cfg, func() (string, error) {
		calls++
		return "", &HTTPError{Status: 503, Body: "unavailable"}
	})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if calls != 3 {
		t.Fatalf("expected 3 attempts, got %d", calls)
	}
}

func TestRetryDo_ContextCancellation(t *testing.T) {
	cfg := RetryConfig{Attempts: 10, MinDelay: 5 * time.Second, MaxDelay: 5 * time.Second}
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := RetryDo(ctx, cfg, func() (string, error) {
		return "", &HTTPError{Status: 500}
	})
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
	// Should have been cancelled quickly, not waited for 5s backoff
	if elapsed > 2*time.Second {
		t.Fatalf("context cancellation took too long: %v", elapsed)
	}
}

func TestRetryDo_HookCalledOnRetry(t *testing.T) {
	cfg := RetryConfig{Attempts: 3, MinDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond}

	var hookCalls atomic.Int32
	ctx := WithRetryHook(context.Background(), func(attempt, maxAttempts int, err error) {
		hookCalls.Add(1)
	})

	RetryDo(ctx, cfg, func() (string, error) {
		return "", &HTTPError{Status: 500}
	})

	// Hook called before each retry (not the first attempt, not the last failure)
	// With 3 attempts: attempt 1 fails → hook → attempt 2 fails → hook → attempt 3 fails → done
	if got := hookCalls.Load(); got != 2 {
		t.Fatalf("expected 2 hook calls, got %d", got)
	}
}

func TestRetryDo_ZeroAttempts_DefaultsToOne(t *testing.T) {
	cfg := RetryConfig{Attempts: 0}
	var calls int
	_, err := RetryDo(context.Background(), cfg, func() (string, error) {
		calls++
		return "", &HTTPError{Status: 500}
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("zero attempts should default to 1: got %d calls", calls)
	}
}

// --- HTTPError ---

func TestHTTPError_ErrorString(t *testing.T) {
	err := &HTTPError{Status: 429, Body: "rate limited"}
	got := err.Error()
	want := "HTTP 429: rate limited"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
