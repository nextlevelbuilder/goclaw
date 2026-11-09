package personal

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSendLimiterPerThreadDelayAndDailyCap(t *testing.T) {
	now := time.Unix(0, 0)
	var slept []time.Duration
	limiter := newSendLimiter(sendLimiterConfig{
		ratePerMinute: 100,
		delay:         1200 * time.Millisecond,
		dailyCap:      5,
		now:           func() time.Time { return now },
		sleep: func(_ context.Context, d time.Duration) error {
			slept = append(slept, d)
			now = now.Add(d)
			return nil
		},
		jitter: func() time.Duration { return 200 * time.Millisecond },
	})

	ctx := context.Background()
	for i := range 5 {
		if err := limiter.Wait(ctx, "thread-1"); err != nil {
			t.Fatalf("Wait(%d): %v", i, err)
		}
	}
	if len(slept) != 4 {
		t.Fatalf("slept %d times, want 4", len(slept))
	}
	for i, d := range slept {
		if d < 1200*time.Millisecond {
			t.Fatalf("sleep[%d] = %s, want >= 1200ms", i, d)
		}
	}
	if err := limiter.Wait(ctx, "thread-1"); !errors.Is(err, ErrDailyCapReached) {
		t.Fatalf("Wait over cap = %v, want ErrDailyCapReached", err)
	}
}

func TestSendLimiterGlobalTokenBucket(t *testing.T) {
	now := time.Unix(0, 0)
	var slept []time.Duration
	limiter := newSendLimiter(sendLimiterConfig{
		ratePerMinute: 2,
		delay:         time.Nanosecond,
		dailyCap:      100,
		now:           func() time.Time { return now },
		sleep: func(_ context.Context, d time.Duration) error {
			slept = append(slept, d)
			now = now.Add(d)
			return nil
		},
		jitter: func() time.Duration { return 200 * time.Millisecond },
	})

	ctx := context.Background()
	for i := range 3 {
		if err := limiter.Wait(ctx, "thread-1"); err != nil {
			t.Fatalf("Wait(%d): %v", i, err)
		}
	}
	if len(slept) == 0 {
		t.Fatal("global bucket never throttled")
	}
	if last := slept[len(slept)-1]; last < 30*time.Second-time.Millisecond {
		t.Fatalf("global wait = %s, want >= 30s for 2/min", last)
	}
}

func TestSendLimiterResetsDailyCap(t *testing.T) {
	now := time.Unix(0, 0)
	limiter := newSendLimiter(sendLimiterConfig{
		ratePerMinute: 100,
		delay:         time.Nanosecond,
		dailyCap:      2,
		now:           func() time.Time { return now },
		sleep: func(_ context.Context, d time.Duration) error {
			now = now.Add(d)
			return nil
		},
		jitter: func() time.Duration { return 200 * time.Millisecond },
	})

	ctx := context.Background()
	for i := range 2 {
		if err := limiter.Wait(ctx, "thread-1"); err != nil {
			t.Fatalf("Wait(%d): %v", i, err)
		}
	}
	if err := limiter.Wait(ctx, "thread-1"); !errors.Is(err, ErrDailyCapReached) {
		t.Fatalf("Wait over cap = %v, want ErrDailyCapReached", err)
	}
	now = now.Add(24 * time.Hour)
	if err := limiter.Wait(ctx, "thread-1"); err != nil {
		t.Fatalf("Wait after day reset: %v", err)
	}
}
