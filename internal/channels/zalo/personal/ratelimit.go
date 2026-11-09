package personal

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrDailyCapReached is returned when the per-instance daily outbound cap is
// exhausted. Callers must surface it; silently dropping a send hides a policy
// problem from both the agent and the operator.
var ErrDailyCapReached = errors.New("zalo_personal: daily send cap reached")

const (
	defaultThreadBuckets = 1000
	minJitter            = 200 * time.Millisecond
	maxJitter            = 600 * time.Millisecond
)

type sendLimiterConfig struct {
	ratePerMinute int
	delay         time.Duration
	dailyCap      int
	maxThreads    int
	now           func() time.Time
	sleep         func(context.Context, time.Duration) error
	jitter        func() time.Duration
}

// sendLimiter enforces a global token bucket, a per-thread minimum interval,
// and a daily logical-send cap. It deliberately counts logical sends (an image,
// a file, or one text chunk), not HTTP requests, so media uploads do not
// double-count against the cap.
type sendLimiter struct {
	mu sync.Mutex

	ratePerMinute float64 // tokens per minute
	capacity      float64
	delay         time.Duration
	dailyCap      int
	maxThreads    int

	now    func() time.Time
	sleep  func(context.Context, time.Duration) error
	jitter func() time.Duration

	tokens     float64
	lastRefill time.Time

	threads map[string]*threadBucket
	order   []string

	day       string
	sentToday int
}

type threadBucket struct {
	last     time.Time
	lastUsed time.Time
}

func newSendLimiter(cfg sendLimiterConfig) *sendLimiter {
	if cfg.ratePerMinute <= 0 {
		cfg.ratePerMinute = 30
	}
	if cfg.delay <= 0 {
		cfg.delay = 1200 * time.Millisecond
	}
	if cfg.dailyCap == 0 {
		cfg.dailyCap = 500
	}
	if cfg.maxThreads <= 0 {
		cfg.maxThreads = defaultThreadBuckets
	}
	if cfg.now == nil {
		cfg.now = time.Now
	}
	if cfg.sleep == nil {
		cfg.sleep = sleepContext
	}
	if cfg.jitter == nil {
		cfg.jitter = randomJitter
	}

	now := cfg.now()
	return &sendLimiter{
		ratePerMinute: float64(cfg.ratePerMinute),
		capacity:      float64(cfg.ratePerMinute),
		delay:         cfg.delay,
		dailyCap:      cfg.dailyCap,
		maxThreads:    cfg.maxThreads,
		now:           cfg.now,
		sleep:         cfg.sleep,
		jitter:        cfg.jitter,
		tokens:        float64(cfg.ratePerMinute),
		lastRefill:    now,
		threads:       make(map[string]*threadBucket),
		day:           now.Format("2006-01-02"),
	}
}

// Wait blocks until the send may proceed, then reserves one logical send.
func (l *sendLimiter) Wait(ctx context.Context, threadID string) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		l.mu.Lock()
		now := l.now()
		l.rollDayLocked(now)
		if l.dailyCap > 0 && l.sentToday >= l.dailyCap {
			l.mu.Unlock()
			return fmt.Errorf("%w (%d)", ErrDailyCapReached, l.dailyCap)
		}

		l.refillLocked(now)
		wait := l.waitForLocked(now, threadID)
		if wait <= 0 {
			l.tokens--
			l.markThreadLocked(threadID, now)
			l.sentToday++
			l.mu.Unlock()
			return nil
		}
		l.mu.Unlock()

		if err := l.sleep(ctx, wait); err != nil {
			return err
		}
	}
}

// Jitter sleeps the configured random interval between text chunks.
func (l *sendLimiter) Jitter(ctx context.Context) error {
	return l.sleep(ctx, l.jitter())
}

func (l *sendLimiter) waitForLocked(now time.Time, threadID string) time.Duration {
	var wait time.Duration

	if l.tokens < 1 {
		perToken := time.Minute / time.Duration(l.ratePerMinute)
		wait = time.Duration((1 - l.tokens) * float64(perToken))
	}

	if b := l.threads[threadID]; b != nil && !b.last.IsZero() {
		if elapsed := now.Sub(b.last); elapsed < l.delay {
			if d := l.delay - elapsed; d > wait {
				wait = d
			}
		}
	}
	return wait
}

func (l *sendLimiter) refillLocked(now time.Time) {
	elapsed := now.Sub(l.lastRefill)
	if elapsed <= 0 {
		return
	}
	l.tokens += elapsed.Minutes() * l.ratePerMinute
	if l.tokens > l.capacity {
		l.tokens = l.capacity
	}
	l.lastRefill = now
}

func (l *sendLimiter) markThreadLocked(threadID string, now time.Time) {
	b := l.threads[threadID]
	if b == nil {
		if len(l.threads) >= l.maxThreads {
			l.evictThreadLocked()
		}
		b = &threadBucket{}
		l.threads[threadID] = b
		l.order = append(l.order, threadID)
	}
	b.last = now
	b.lastUsed = now
}

func (l *sendLimiter) evictThreadLocked() {
	for len(l.order) > 0 {
		oldest := l.order[0]
		l.order = l.order[1:]
		if _, ok := l.threads[oldest]; ok {
			delete(l.threads, oldest)
			return
		}
	}
}

func (l *sendLimiter) rollDayLocked(now time.Time) {
	day := now.Format("2006-01-02")
	if day != l.day {
		l.day = day
		l.sentToday = 0
	}
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func randomJitter() time.Duration {
	// time.Now().UnixNano is sufficient here; jitter only avoids mechanical
	// pacing and is not security-sensitive.
	span := int64(maxJitter - minJitter)
	return minJitter + time.Duration(time.Now().UnixNano()%span)
}
