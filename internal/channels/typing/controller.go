// Package typing provides a channel-agnostic typing indicator controller
// with TTL safety net, keepalive support, and post-close guards.
//
// The controller prevents stuck typing indicators by:
//   - Auto-stopping after a configurable TTL (default 60s)
//   - Requiring both MarkRunComplete + MarkDispatchIdle for graceful cleanup
//   - Guarding against post-close keepalive invocations
package typing

import (
	"log/slog"
	"sync"
	"time"
)

// Options configures a typing indicator controller.
type Options struct {
	// MaxDuration is the TTL safety net. If the indicator hasn't been
	// stopped after this duration, it auto-stops. 0 disables the TTL.
	// Default: 60s.
	MaxDuration time.Duration

	// KeepaliveInterval is how often to re-send the typing action.
	// Telegram typing expires after 5s, Discord after 10s.
	// 0 disables keepalive (single fire-and-forget).
	KeepaliveInterval time.Duration

	// StartFn sends the channel-specific typing indicator.
	// Called on Start() and on each keepalive tick.
	StartFn func() error

	// StopFn sends the channel-specific stop-typing signal.
	// Optional — some channels (Telegram) auto-stop on message send.
	StopFn func() error
}

// Controller manages the lifecycle of a typing indicator.
// It is safe for concurrent use.
type Controller struct {
	mu sync.Mutex

	// State flags
	closed       bool // post-close guard: prevents stale startFn calls
	runComplete  bool // signal 1: agent finished processing
	dispatchIdle bool // signal 2: message delivery finished
	stopSent     bool // prevents duplicate stopFn calls

	// Configuration
	maxDuration       time.Duration
	keepaliveInterval time.Duration
	startFn           func() error
	stopFn            func() error

	// Timers
	ttlTimer      *time.Timer
	keepaliveDone chan struct{}
}

// New creates a typing controller with the given options.
func New(opts Options) *Controller {
	maxDur := opts.MaxDuration
	if maxDur == 0 {
		maxDur = 60 * time.Second
	}
	return &Controller{
		maxDuration:       maxDur,
		keepaliveInterval: opts.KeepaliveInterval,
		startFn:           opts.StartFn,
		stopFn:            opts.StopFn,
	}
}

// Start begins the typing indicator, TTL timer, and keepalive loop.
func (c *Controller) Start() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return
	}

	// Fire initial typing action
	c.fireStart()

	// Start TTL safety net
	if c.maxDuration > 0 {
		c.ttlTimer = time.AfterFunc(c.maxDuration, func() {
			c.mu.Lock()
			if !c.closed {
				slog.Debug("typing: TTL exceeded, auto-stopping", "ttl", c.maxDuration)
				c.forceStop()
			}
			c.mu.Unlock()
		})
	}

	// Start keepalive loop
	if c.keepaliveInterval > 0 {
		c.keepaliveDone = make(chan struct{})
		go c.keepaliveLoop()
	}
}

// Stop forcefully stops the typing indicator and all timers.
// Safe to call multiple times (idempotent).
func (c *Controller) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.forceStop()
}

// MarkRunComplete signals that the agent has finished processing.
// Cleanup happens only when both MarkRunComplete and MarkDispatchIdle have been called.
func (c *Controller) MarkRunComplete() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.runComplete = true
	c.tryCleanup()
}

// MarkDispatchIdle signals that message delivery has completed.
// Cleanup happens only when both MarkRunComplete and MarkDispatchIdle have been called.
func (c *Controller) MarkDispatchIdle() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dispatchIdle = true
	c.tryCleanup()
}

// tryCleanup runs cleanup only when both completion signals have been received.
// Must be called with c.mu held.
func (c *Controller) tryCleanup() {
	if c.runComplete && c.dispatchIdle && !c.closed {
		c.forceStop()
	}
}

// forceStop cancels all timers and sends stop signal.
// Must be called with c.mu held.
func (c *Controller) forceStop() {
	if c.closed {
		return
	}
	c.closed = true

	// Cancel TTL timer
	if c.ttlTimer != nil {
		c.ttlTimer.Stop()
		c.ttlTimer = nil
	}

	// Stop keepalive loop
	if c.keepaliveDone != nil {
		close(c.keepaliveDone)
	}

	// Send stop signal
	c.fireStop()
}

// fireStart invokes the channel-specific start function.
// Must be called with c.mu held.
func (c *Controller) fireStart() {
	if c.closed || c.startFn == nil {
		return
	}
	if err := c.startFn(); err != nil {
		slog.Debug("typing: startFn error", "error", err)
	}
}

// fireStop invokes the channel-specific stop function (once).
// Must be called with c.mu held.
func (c *Controller) fireStop() {
	if c.stopSent || c.stopFn == nil {
		return
	}
	c.stopSent = true
	if err := c.stopFn(); err != nil {
		slog.Debug("typing: stopFn error", "error", err)
	}
}

// keepaliveLoop periodically re-sends the typing indicator.
func (c *Controller) keepaliveLoop() {
	ticker := time.NewTicker(c.keepaliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.keepaliveDone:
			return
		case <-ticker.C:
			c.mu.Lock()
			if c.closed {
				c.mu.Unlock()
				return
			}
			c.fireStart()
			c.mu.Unlock()
		}
	}
}
