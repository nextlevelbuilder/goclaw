package personal

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal/protocol"
)

const (
	maxChannelRestarts   = 10
	maxChannelBackoff    = 60 * time.Second
	code3000InitialDelay = 60 * time.Second
)

func (c *Channel) listenLoop(ctx context.Context) {
	defer c.SetRunning(false)
	for {
		if !c.runListenerLoop(ctx) {
			return
		}
	}
}

// runListenerLoop reads from the current listener until it closes.
// Returns true if the channel restarted and the outer loop should continue,
// false if the channel should stop permanently.
func (c *Channel) runListenerLoop(ctx context.Context) bool {
	ln := c.getListener()
	if ln == nil {
		return false
	}
	for {
		select {
		case <-ctx.Done():
			slog.Info("zalo_personal listener loop stopped (context)")
			return false
		case <-c.stopCh:
			slog.Info("zalo_personal listener loop stopped")
			return false

		case msg, ok := <-ln.Messages():
			if !ok {
				return false
			}
			c.handleMessage(msg)

		case ci := <-ln.Disconnected():
			slog.Warn("zalo_personal disconnected", "code", ci.Code, "reason", ci.Reason)

		case ci := <-ln.Closed():
			slog.Warn("zalo_personal connection closed", "code", ci.Code, "reason", ci.Reason)

			// Code 3000: wait 60s before retry (duplicate session may be transient)
			if ci.Code == protocol.CloseCodeDuplicate {
				slog.Warn("zalo_personal duplicate session (code 3000), waiting before retry", "channel", c.Name())
				select {
				case <-ctx.Done():
					return false
				case <-c.stopCh:
					return false
				case <-time.After(code3000InitialDelay):
				}
			}

			return c.restartWithBackoff(ctx)

		case err := <-ln.Errors():
			slog.Warn("zalo_personal listener error", "error", err)
		}
	}
}

// restartWithBackoff attempts to restart the channel with exponential backoff.
// This is the channel-level retry layer for auth/session failures. The listener
// has its own internal retry for transient WS disconnects (endpoint rotation).
// When the listener exhausts its retries, it emits to closedCh which triggers this.
// Returns true if restart succeeded and the listen loop should continue.
func (c *Channel) restartWithBackoff(ctx context.Context) bool {
	for attempt := range maxChannelRestarts {
		delay := min(time.Duration(1<<uint(attempt+1))*time.Second, maxChannelBackoff)
		slog.Info("zalo_personal restarting channel", "attempt", attempt+1, "delay", delay, "channel", c.Name())

		select {
		case <-ctx.Done():
			return false
		case <-c.stopCh:
			return false
		case <-time.After(delay):
		}

		if err := c.restart(ctx); err != nil {
			slog.Warn("zalo_personal restart failed", "attempt", attempt+1, "error", err)
			continue
		}
		return true
	}
	slog.Error("zalo_personal channel gave up after max restart attempts", "channel", c.Name())
	return false
}

// restart performs a full re-authentication and listener restart.
// Called from the listenLoop goroutine when the listener exhausts retries.
func (c *Channel) restart(ctx context.Context) error {
	if ln := c.getListener(); ln != nil {
		ln.Stop()
	}

	sess, err := c.authenticate(ctx)
	if err != nil {
		return fmt.Errorf("re-auth: %w", err)
	}

	ln, err := protocol.NewListener(sess)
	if err != nil {
		return fmt.Errorf("new listener: %w", err)
	}
	if err := ln.Start(ctx); err != nil {
		return fmt.Errorf("start listener: %w", err)
	}

	c.mu.Lock()
	c.sess = sess
	c.listener = ln
	c.mu.Unlock()
	return nil
}
