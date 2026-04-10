package eventbus

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// busImpl implements DomainEventBus with a worker pool, dedup, and retry.
type busImpl struct {
	cfg      Config
	queue    chan DomainEvent
	handlers map[EventType][]DomainEventHandler
	mu       sync.RWMutex
	dedup    *dedupSet
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
	started  atomic.Bool
	draining atomic.Bool
}

// NewDomainEventBus creates a bus. Call Start() before Publish().
func NewDomainEventBus(cfg Config) DomainEventBus {
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 1000
	}
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = 2
	}
	if cfg.RetryAttempts <= 0 {
		cfg.RetryAttempts = 3
	}
	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = time.Second
	}
	if cfg.DedupTTL <= 0 {
		cfg.DedupTTL = 5 * time.Minute
	}
	return &busImpl{
		cfg:      cfg,
		queue:    make(chan DomainEvent, cfg.QueueSize),
		handlers: make(map[EventType][]DomainEventHandler),
		dedup:    newDedupSet(cfg.DedupTTL),
	}
}

func (b *busImpl) Start(ctx context.Context) {
	if b.started.Swap(true) {
		return // already started
	}
	b.ctx, b.cancel = context.WithCancel(ctx)
	for range b.cfg.WorkerCount {
		b.wg.Add(1)
		go b.worker()
	}
}

func (b *busImpl) Publish(event DomainEvent) {
	if b.draining.Load() {
		return
	}
	select {
	case b.queue <- event:
	default:
		slog.Warn("eventbus: queue full, dropping event",
			"type", event.Type, "source_id", event.SourceID)
	}
}

func (b *busImpl) Subscribe(eventType EventType, handler DomainEventHandler) func() {
	b.mu.Lock()
	b.handlers[eventType] = append(b.handlers[eventType], handler)
	idx := len(b.handlers[eventType]) - 1
	b.mu.Unlock()

	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		hs := b.handlers[eventType]
		if idx < len(hs) {
			b.handlers[eventType] = append(hs[:idx], hs[idx+1:]...)
		}
	}
}

func (b *busImpl) Drain(timeout time.Duration) error {
	b.draining.Store(true)
	close(b.queue)

	done := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		b.dedup.Close()
		return nil
	case <-time.After(timeout):
		b.cancel()
		b.dedup.Close()
		return fmt.Errorf("eventbus: drain timeout after %v", timeout)
	}
}

// worker processes events from the queue.
func (b *busImpl) worker() {
	defer b.wg.Done()
	for event := range b.queue {
		if b.ctx.Err() != nil {
			return
		}
		b.dispatch(event)
	}
}

// dispatch fans out event to all registered handlers with dedup + retry.
func (b *busImpl) dispatch(event DomainEvent) {
	// Only dedup events with a SourceID. Events without SourceID always process.
	if event.SourceID != "" && !b.dedup.Add(string(event.Type)+":"+event.SourceID) {
		return // duplicate
	}

	b.mu.RLock()
	handlers := make([]DomainEventHandler, len(b.handlers[event.Type]))
	copy(handlers, b.handlers[event.Type])
	b.mu.RUnlock()

	for _, handler := range handlers {
		b.callWithRetry(handler, event)
	}
}

// callWithRetry calls handler with exponential backoff retry on error.
func (b *busImpl) callWithRetry(handler DomainEventHandler, event DomainEvent) {
	delay := b.cfg.RetryDelay
	for attempt := range b.cfg.RetryAttempts {
		err := b.safeCall(handler, event)
		if err == nil {
			return
		}
		slog.Warn("eventbus: handler error",
			"type", event.Type, "attempt", attempt+1, "err", err)
		if attempt < b.cfg.RetryAttempts-1 {
			time.Sleep(delay)
			delay *= 2
		}
	}
}

// safeCall invokes handler with panic recovery.
func (b *busImpl) safeCall(handler DomainEventHandler, event DomainEvent) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("eventbus: handler panic: %v", r)
			slog.Error("eventbus: handler panic", "type", event.Type, "panic", r)
		}
	}()
	return handler(b.ctx, event)
}
