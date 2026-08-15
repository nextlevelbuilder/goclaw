package eventbus

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func newTestBus() DomainEventBus {
	return NewDomainEventBus(Config{
		QueueSize:     100,
		WorkerCount:   2,
		RetryAttempts: 3,
		RetryDelay:    10 * time.Millisecond,
		DedupTTL:      time.Minute,
	})
}

func TestPublishSubscribe(t *testing.T) {
	bus := newTestBus()
	ctx := t.Context()
	bus.Start(ctx)

	var received atomic.Int32
	bus.Subscribe(EventRunCompleted, func(_ context.Context, e DomainEvent) error {
		received.Add(1)
		return nil
	})

	bus.Publish(DomainEvent{Type: EventRunCompleted, SourceID: "r1"})
	bus.Publish(DomainEvent{Type: EventRunCompleted, SourceID: "r2"})

	time.Sleep(50 * time.Millisecond)
	if got := received.Load(); got != 2 {
		t.Errorf("expected 2 events, got %d", got)
	}
	_ = bus.Drain(time.Second)
}

func TestMultipleHandlers(t *testing.T) {
	bus := newTestBus()
	ctx := t.Context()
	bus.Start(ctx)

	var h1, h2 atomic.Int32
	bus.Subscribe(EventToolExecuted, func(_ context.Context, _ DomainEvent) error {
		h1.Add(1)
		return nil
	})
	bus.Subscribe(EventToolExecuted, func(_ context.Context, _ DomainEvent) error {
		h2.Add(1)
		return nil
	})

	bus.Publish(DomainEvent{Type: EventToolExecuted, SourceID: "t1"})
	time.Sleep(50 * time.Millisecond)

	if h1.Load() != 1 || h2.Load() != 1 {
		t.Errorf("expected both handlers called once, got h1=%d h2=%d", h1.Load(), h2.Load())
	}
	_ = bus.Drain(time.Second)
}

func TestUnsubscribe(t *testing.T) {
	bus := newTestBus()
	ctx := t.Context()
	bus.Start(ctx)

	var count atomic.Int32
	unsub := bus.Subscribe(EventRunCompleted, func(_ context.Context, _ DomainEvent) error {
		count.Add(1)
		return nil
	})

	bus.Publish(DomainEvent{Type: EventRunCompleted, SourceID: "u1"})
	time.Sleep(50 * time.Millisecond)
	unsub()

	bus.Publish(DomainEvent{Type: EventRunCompleted, SourceID: "u2"})
	time.Sleep(50 * time.Millisecond)

	if got := count.Load(); got != 1 {
		t.Errorf("expected 1 after unsub, got %d", got)
	}
	_ = bus.Drain(time.Second)
}

func TestDedupBySourceID(t *testing.T) {
	bus := newTestBus()
	ctx := t.Context()
	bus.Start(ctx)

	var count atomic.Int32
	bus.Subscribe(EventSessionCompleted, func(_ context.Context, _ DomainEvent) error {
		count.Add(1)
		return nil
	})

	// Same SourceID, same type — should dedup
	bus.Publish(DomainEvent{Type: EventSessionCompleted, SourceID: "s1"})
	bus.Publish(DomainEvent{Type: EventSessionCompleted, SourceID: "s1"})
	bus.Publish(DomainEvent{Type: EventSessionCompleted, SourceID: "s1"})
	time.Sleep(50 * time.Millisecond)

	if got := count.Load(); got != 1 {
		t.Errorf("expected 1 (deduped), got %d", got)
	}
	_ = bus.Drain(time.Second)
}

func TestRetryOnError(t *testing.T) {
	bus := newTestBus()
	ctx := t.Context()
	bus.Start(ctx)

	var attempts atomic.Int32
	bus.Subscribe(EventRunCompleted, func(_ context.Context, _ DomainEvent) error {
		n := attempts.Add(1)
		if n < 3 {
			return fmt.Errorf("transient error %d", n)
		}
		return nil // succeed on 3rd attempt
	})

	bus.Publish(DomainEvent{Type: EventRunCompleted, SourceID: "retry1"})
	time.Sleep(200 * time.Millisecond) // allow retries

	if got := attempts.Load(); got != 3 {
		t.Errorf("expected 3 attempts, got %d", got)
	}
	_ = bus.Drain(time.Second)
}

func TestDrainFlushes(t *testing.T) {
	bus := newTestBus()
	ctx := t.Context()
	bus.Start(ctx)

	var count atomic.Int32
	bus.Subscribe(EventEpisodicCreated, func(_ context.Context, _ DomainEvent) error {
		time.Sleep(10 * time.Millisecond) // simulate work
		count.Add(1)
		return nil
	})

	for i := range 5 {
		bus.Publish(DomainEvent{Type: EventEpisodicCreated, SourceID: fmt.Sprintf("d%d", i)})
	}

	err := bus.Drain(2 * time.Second)
	if err != nil {
		t.Errorf("drain error: %v", err)
	}
	if got := count.Load(); got != 5 {
		t.Errorf("expected 5 processed before drain, got %d", got)
	}
}

func TestPublishAfterDrain(t *testing.T) {
	bus := newTestBus()
	ctx := t.Context()
	bus.Start(ctx)

	var count atomic.Int32
	bus.Subscribe(EventRunCompleted, func(_ context.Context, _ DomainEvent) error {
		count.Add(1)
		return nil
	})

	_ = bus.Drain(time.Second)

	// Should not panic or deliver
	bus.Publish(DomainEvent{Type: EventRunCompleted, SourceID: "post-drain"})
	time.Sleep(50 * time.Millisecond)

	if got := count.Load(); got != 0 {
		t.Errorf("expected 0 after drain, got %d", got)
	}
}

func TestEmptySourceIDNeverDeduped(t *testing.T) {
	bus := newTestBus()
	ctx := t.Context()
	bus.Start(ctx)

	var count atomic.Int32
	bus.Subscribe(EventToolExecuted, func(_ context.Context, _ DomainEvent) error {
		count.Add(1)
		return nil
	})

	// Empty SourceID — all should be processed
	bus.Publish(DomainEvent{Type: EventToolExecuted, SourceID: ""})
	bus.Publish(DomainEvent{Type: EventToolExecuted, SourceID: ""})
	bus.Publish(DomainEvent{Type: EventToolExecuted, SourceID: ""})
	time.Sleep(50 * time.Millisecond)

	if got := count.Load(); got != 3 {
		t.Errorf("expected 3 (no dedup on empty), got %d", got)
	}
	_ = bus.Drain(time.Second)
}
