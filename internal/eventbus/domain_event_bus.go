package eventbus

import (
	"context"
	"time"
)

// DomainEventHandler processes a single domain event.
type DomainEventHandler func(ctx context.Context, event DomainEvent) error

// DomainEventBus manages typed event publishing + worker subscriptions.
type DomainEventBus interface {
	// Publish enqueues an event for async processing. Non-blocking.
	Publish(event DomainEvent)

	// Subscribe registers a handler for a specific event type.
	// Returns an unsubscribe function.
	Subscribe(eventType EventType, handler DomainEventHandler) func()

	// Start launches the worker pool. Must be called before Publish.
	Start(ctx context.Context)

	// Drain waits for all queued events to be processed. For graceful shutdown.
	Drain(timeout time.Duration) error
}

// Config for the domain event bus worker pool.
type Config struct {
	QueueSize     int           // buffered channel capacity (default 1000)
	WorkerCount   int           // goroutines processing events (default 2)
	RetryAttempts int           // retry on handler error (default 3)
	RetryDelay    time.Duration // backoff base (default 1s, exponential)
	DedupTTL      time.Duration // dedup window for SourceID (default 5m)
}

// DefaultConfig returns sensible defaults for the event bus.
func DefaultConfig() Config {
	return Config{
		QueueSize:     1000,
		WorkerCount:   2,
		RetryAttempts: 3,
		RetryDelay:    time.Second,
		DedupTTL:      5 * time.Minute,
	}
}
