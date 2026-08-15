package tools

import (
	"context"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

const asyncCompletionAttempts = 3

const (
	asyncTerminalPersistenceAttempts = 12
	asyncPersistenceAttemptTimeout   = 2 * time.Second
	asyncPersistenceMaxBackoff       = time.Second
)

// PublishAsyncCompletion retries only after child execution has released its
// admission lease. The durable task row remains the fallback if every attempt
// finds the inbound queue saturated.
func PublishAsyncCompletion(
	ctx context.Context,
	msgBus *bus.MessageBus,
	message bus.InboundMessage,
) bool {
	if msgBus == nil {
		return true
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for attempt := range asyncCompletionAttempts {
		if msgBus.TryPublishInbound(message) {
			return true
		}
		if attempt == asyncCompletionAttempts-1 {
			break
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
	return false
}

func retryAsyncPersistence(
	ctx context.Context,
	operation func(context.Context) error,
) error {
	return retryPersistence(ctx, asyncCompletionAttempts, operation)
}

func retryTerminalPersistence(
	ctx context.Context,
	operation func(context.Context) error,
) error {
	return retryPersistence(ctx, asyncTerminalPersistenceAttempts, operation)
}

func retryPersistence(
	ctx context.Context,
	attempts int,
	operation func(context.Context) error,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var err error
	for attempt := range attempts {
		attemptCtx, cancel := context.WithTimeout(ctx, asyncPersistenceAttemptTimeout)
		err = operation(attemptCtx)
		cancel()
		if err == nil {
			return nil
		}
		if attempt == attempts-1 {
			break
		}
		backoff := min(time.Duration(1<<attempt)*25*time.Millisecond, asyncPersistenceMaxBackoff)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return err
		case <-timer.C:
		}
	}
	return err
}
