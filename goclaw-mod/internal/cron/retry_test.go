package cron

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestExecuteWithRetry_SuccessFirstAttempt(t *testing.T) {
	result, attempts, err := ExecuteWithRetry(func() (string, error) {
		return "ok", nil
	}, RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Errorf("expected 'ok', got %q", result)
	}
	if attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", attempts)
	}
}

func TestExecuteWithRetry_SuccessAfterRetries(t *testing.T) {
	callCount := 0
	result, attempts, err := ExecuteWithRetry(func() (string, error) {
		callCount++
		if callCount < 3 {
			return "", fmt.Errorf("fail-%d", callCount)
		}
		return "recovered", nil
	}, RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "recovered" {
		t.Errorf("expected 'recovered', got %q", result)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestExecuteWithRetry_AllFail(t *testing.T) {
	callCount := 0
	_, attempts, err := ExecuteWithRetry(func() (string, error) {
		callCount++
		return "", fmt.Errorf("always-fail")
	}, RetryConfig{MaxRetries: 2, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond})

	if err == nil {
		t.Fatal("expected error after all retries")
	}
	if err.Error() != "always-fail" {
		t.Errorf("expected 'always-fail', got %q", err.Error())
	}
	if callCount != 3 { // 1 initial + 2 retries
		t.Errorf("expected 3 calls, got %d", callCount)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestExecuteWithRetry_ZeroRetries(t *testing.T) {
	callCount := 0
	_, _, err := ExecuteWithRetry(func() (string, error) {
		callCount++
		return "", fmt.Errorf("fail")
	}, RetryConfig{MaxRetries: 0, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond})

	if err == nil {
		t.Fatal("expected error")
	}
	if callCount != 1 {
		t.Errorf("expected 1 call with 0 retries, got %d", callCount)
	}
}

func TestBackoffWithJitter(t *testing.T) {
	base := 100 * time.Millisecond
	max := 1 * time.Second

	// Attempt 0: ~100ms ± 25%
	d0 := backoffWithJitter(base, max, 0)
	if d0 < 75*time.Millisecond || d0 > 125*time.Millisecond {
		t.Errorf("attempt 0: expected ~100ms, got %v", d0)
	}

	// Attempt 1: ~200ms ± 25%
	d1 := backoffWithJitter(base, max, 1)
	if d1 < 150*time.Millisecond || d1 > 250*time.Millisecond {
		t.Errorf("attempt 1: expected ~200ms, got %v", d1)
	}

	// Attempt 2: ~400ms ± 25%
	d2 := backoffWithJitter(base, max, 2)
	if d2 < 300*time.Millisecond || d2 > 500*time.Millisecond {
		t.Errorf("attempt 2: expected ~400ms, got %v", d2)
	}
}

func TestBackoffWithJitter_CapsAtMax(t *testing.T) {
	base := 100 * time.Millisecond
	max := 200 * time.Millisecond

	// Attempt 10: should cap at ~200ms ± 25%
	d := backoffWithJitter(base, max, 10)
	if d < 150*time.Millisecond || d > 250*time.Millisecond {
		t.Errorf("expected capped at ~200ms, got %v", d)
	}
}

func TestTruncateOutput_Short(t *testing.T) {
	s := "hello world"
	if TruncateOutput(s) != s {
		t.Errorf("short string should not be truncated")
	}
}

func TestTruncateOutput_ExactLimit(t *testing.T) {
	s := strings.Repeat("a", maxOutputBytes)
	if TruncateOutput(s) != s {
		t.Error("string at exact limit should not be truncated")
	}
}

func TestTruncateOutput_OverLimit(t *testing.T) {
	s := strings.Repeat("x", maxOutputBytes+100)
	result := TruncateOutput(s)
	if len(result) > maxOutputBytes+20 { // allow for suffix
		t.Errorf("expected truncated output, got len %d", len(result))
	}
	if !strings.HasSuffix(result, "...[truncated]") {
		t.Error("expected ...[truncated] suffix")
	}
}

func TestDefaultRetryConfig(t *testing.T) {
	cfg := DefaultRetryConfig()
	if cfg.MaxRetries != 3 {
		t.Errorf("expected 3 retries, got %d", cfg.MaxRetries)
	}
	if cfg.BaseDelay != 2*time.Second {
		t.Errorf("expected 2s base, got %v", cfg.BaseDelay)
	}
	if cfg.MaxDelay != 30*time.Second {
		t.Errorf("expected 30s max, got %v", cfg.MaxDelay)
	}
}
