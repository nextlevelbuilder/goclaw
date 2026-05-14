package max

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// handlerPoolSize bounds concurrent message handler goroutines per channel
// instance. Matches Telegram's value (20). Keep ample headroom for typical
// agent loops; raise if pollLoop reports back-pressure.
const handlerPoolSize = 20

// pollStopTimeout bounds how long Stop() waits for the polling goroutine
// to exit cleanly before logging a warning.
const pollStopTimeout = 10 * time.Second

// handlerStopTimeout bounds how long Stop() waits for in-flight message
// handlers to finish.
const handlerStopTimeout = 15 * time.Second

// probeTimeout bounds the GET /me call at Start() — used to validate token
// and capture bot identity. Must complete before polling can begin.
const probeTimeout = 30 * time.Second

// pollSupervisorMaxRestarts bounds restart attempts within pollSupervisorWindow.
// If exceeded, the channel is marked failed and the supervisor exits.
const pollSupervisorMaxRestarts = 5

// pollSupervisorWindow is the sliding window for rate-limiting restart attempts.
const pollSupervisorWindow = 5 * time.Minute

// pollSupervisorRestartDelay is the wait before relaunching pollLoop after
// an unexpected exit, preventing tight loops when something is fundamentally
// broken (e.g., bad credentials, persistent network failure).
const pollSupervisorRestartDelay = 5 * time.Second

// Channel implements channels.Channel for Max Messenger.
// Embeds BaseChannel for shared policy/allowlist/pairing/health logic.
type Channel struct {
	*channels.BaseChannel

	creds        instanceCreds
	cfg          instanceConfig
	client       *Client
	pendingStore store.PendingMessageStore
	audioMgr     *audio.Manager

	// Lifecycle.
	startOnce  sync.Once
	stopOnce   sync.Once
	startErr   error
	pollCancel context.CancelFunc
	pollDone   chan struct{}
	handlerWg  sync.WaitGroup
	handlerSem chan struct{}

	// Polling cursor — sequence marker returned by Max GET /updates.
	markerMu sync.Mutex
	marker   *int64

	// Outbound state.
	// placeholders maps chatID (string, as in bus.OutboundMessage.ChatID)
	// to the most recent message_id sent there. Used by streaming (Day 4.5)
	// to know which message to edit instead of sending a new one.
	placeholders sync.Map

	// sentCount tracks total successful chunk sends; useful for tests/metrics.
	sentCount int64

	// runCtxMu guards pollRunCtx — written by Start, read by reaction
	// refresher goroutines via pollContext(). NOT used by webhook
	// dispatch (webhook uses a fresh context.Background per delivery to
	// avoid a cancellation race with Stop; see webhook.go).
	runCtxMu   sync.RWMutex
	pollRunCtx context.Context

	// reactionRefreshers tracks active typing-action goroutines per chatID.
	// Map key: chatID (string). Map value: *reactionRefresher.
	// Cleared on Stop.
	reactionRefreshers sync.Map

	// lastPollAt is the unix timestamp (seconds) of the most recent successful
	// poll. Updated atomically on every successful GetUpdates response.
	// Used by health endpoint and external watchdogs to detect stuck polling.
	// Zero until the first successful poll completes.
	lastPollAt int64
}

// New constructs a Max channel from validated creds and config.
// It performs no I/O — call Start to begin polling/serving.
func New(
	name string,
	creds instanceCreds,
	cfg instanceConfig,
	msgBus *bus.MessageBus,
	pairingSvc store.PairingStore,
	pendingStore store.PendingMessageStore,
	audioMgr *audio.Manager,
) (*Channel, error) {
	if name == "" {
		return nil, errors.New("max: channel name is required")
	}
	if msgBus == nil {
		return nil, errors.New("max: msgBus is required")
	}
	if creds.BotToken == "" {
		return nil, errors.New("max: bot_token is required")
	}

	base := channels.NewBaseChannel(name, msgBus, cfg.AllowFrom)
	base.SetType(channels.TypeMax)
	base.SetPairingService(pairingSvc)
	base.SetHistoryLimit(cfg.HistoryLimit)
	if cfg.RequireMention != nil {
		base.SetRequireMention(*cfg.RequireMention)
	} else {
		base.SetRequireMention(true)
	}

	c := &Channel{
		BaseChannel:  base,
		creds:        creds,
		cfg:          cfg,
		client:       NewClient(creds.BotToken),
		pendingStore: pendingStore,
		audioMgr:     audioMgr,
		pollDone:     make(chan struct{}),
		handlerSem:   make(chan struct{}, handlerPoolSize),
	}

	return c, nil
}

// Start probes the API to validate the token, captures bot identity,
// then spawns the polling goroutine. Subsequent calls are no-ops.
func (c *Channel) Start(ctx context.Context) error {
	c.startOnce.Do(func() {
		c.MarkStarting("Validating Max bot token")

		// Probe — validates token and populates BotID/Username if not set.
		probeCtx, probeCancel := context.WithTimeout(ctx, probeTimeout)
		me, err := c.client.GetMe(probeCtx)
		probeCancel()
		if err != nil {
			c.startErr = fmt.Errorf("validate max bot: %w", err)
			c.MarkFailed(
				"Failed to validate bot token",
				err.Error(),
				channels.ChannelFailureKindUnknown,
				false,
			)
			return
		}
		// Cache identity so we can detect mentions and skip self-loops.
		c.creds.BotID = me.UserID
		if me.Username != "" {
			c.creds.Username = me.Username
		}

		// Spawn polling goroutine bound to a cancellable context.
		pollCtx, cancel := context.WithCancel(context.Background())
		c.pollCancel = cancel

		// Stash for the webhook handler so async dispatch uses the same
		// long-lived context as polling.
		c.runCtxMu.Lock()
		c.pollRunCtx = pollCtx
		c.runCtxMu.Unlock()

		c.SetRunning(true)
		c.MarkHealthy(connectedSummary(me.Username, me.UserID))

		slog.Info("max: bot connected",
			"channel", c.Name(),
			"bot_id", me.UserID,
			"username", me.Username,
			"first_name", me.FirstName,
		)

		// Run pollLoop under a supervisor that restarts it on unexpected exit.
		// This protects against transient bugs (e.g., HTTP timeout
		// misclassification), panics, and any other goroutine death we
		// haven't anticipated.
		go c.runPollSupervisor(pollCtx)

		// Day 4: subscribe webhook here if cfg.Mode == "webhook".
		// For now polling is the only supported mode.
	})
	return c.startErr
}

// Stop cancels polling, waits for in-flight handlers, and updates health.
// Idempotent: subsequent calls are no-ops.
func (c *Channel) Stop(ctx context.Context) error {
	c.stopOnce.Do(func() {
		slog.Info("max: stopping channel", "channel", c.Name())
		c.SetRunning(false)
		c.MarkStopped("Stopped")

		// Signal polling goroutine to exit.
		if c.pollCancel != nil {
			c.pollCancel()
		}

		// Wait for polling to exit (bounded).
		select {
		case <-c.pollDone:
			slog.Info("max: polling stopped", "channel", c.Name())
		case <-time.After(pollStopTimeout):
			slog.Warn("max: polling did not exit within timeout",
				"channel", c.Name(), "timeout", pollStopTimeout)
		}

		// Stop all reaction refreshers — must happen BEFORE handlerWg.Wait
		// because refreshers do not run as handlers but still hold goroutines.
		c.stopAllReactionRefreshers()

		// Wait for in-flight handlers (bounded).
		handlerDone := make(chan struct{})
		go func() {
			c.handlerWg.Wait()
			close(handlerDone)
		}()
		select {
		case <-handlerDone:
			slog.Info("max: channel stopped", "channel", c.Name())
		case <-time.After(handlerStopTimeout):
			slog.Warn("max: handler goroutines did not drain within timeout",
				"channel", c.Name(), "timeout", handlerStopTimeout)
		}
	})
	return nil
}

// Send delivers an outbound message produced by the agent loop.
// Chunks long text by 4000-char limit, formats as markdown, and posts to Max.
// Implementation lives in outbound.go.
func (c *Channel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	return c.send(ctx, msg)
}

// BlockReplyEnabled returns the per-instance block_reply setting if configured,
// or nil to inherit the gateway-level default.
//
// Implements channels.BlockReplyChannel.
func (c *Channel) BlockReplyEnabled() *bool {
	return c.cfg.BlockReply
}

// connectedSummary builds a human-readable status string for the health endpoint.
func connectedSummary(username string, userID int64) string {
	if username != "" {
		return fmt.Sprintf("Connected as @%s (id=%d)", username, userID)
	}
	return fmt.Sprintf("Connected (bot_id=%d)", userID)
}

// runPollSupervisor monitors pollLoop and restarts it on unexpected exit.
// This provides resilience against transient bugs and panics that would
// otherwise leave the channel silently dead (process alive, no polling).
//
// Behaviour:
//   - pollLoop exits cleanly when ctx is cancelled → supervisor returns.
//   - pollLoop exits unexpectedly (panic, network error misclassification,
//     etc.) → supervisor logs WARN, waits restartDelay, relaunches pollLoop.
//   - More than pollSupervisorMaxRestarts unexpected exits within
//     pollSupervisorWindow → supervisor gives up, marks channel FAILED.
//
// The rate-limit protects against tight restart loops when something is
// fundamentally broken (e.g. bad credentials, persistent connectivity loss).
// In that case operators see a clear FAILED state in the health endpoint
// instead of a busy goroutine churning forever.
func (c *Channel) runPollSupervisor(ctx context.Context) {
	var restartTimes []time.Time

	for {
		if ctx.Err() != nil {
			slog.Info("max: poll supervisor exiting (context cancelled)",
				"channel", c.Name())
			return
		}

		// On restart (not the first iteration), recreate pollDone so any
		// observer waiting on it sees a fresh "in flight" channel. Stop()
		// reads the field then waits on whatever pollDone points to, so
		// races are bounded to a small window during Stop+restart overlap;
		// the worst case is a redundant timeout in Stop, which is benign.
		if len(restartTimes) > 0 {
			c.pollDone = make(chan struct{})
		}

		// Run pollLoop. It will return on:
		//   - ctx cancelled (normal Stop) — caller sees ctx.Err() != nil
		//   - panic (recovered inside pollLoop's defer)
		//   - any unexpected exit (e.g., bug)
		c.pollLoop(ctx)

		// Distinguish normal vs unexpected exit.
		if ctx.Err() != nil {
			slog.Info("max: pollLoop exited cleanly (context done)",
				"channel", c.Name())
			return
		}

		// Unexpected exit: rate-limit restart attempts.
		now := time.Now()
		cutoff := now.Add(-pollSupervisorWindow)
		kept := restartTimes[:0]
		for _, t := range restartTimes {
			if t.After(cutoff) {
				kept = append(kept, t)
			}
		}
		restartTimes = kept

		if len(restartTimes) >= pollSupervisorMaxRestarts {
			slog.Error("max: polling restart limit exceeded, marking channel failed",
				"channel", c.Name(),
				"restarts", len(restartTimes),
				"window", pollSupervisorWindow)
			c.MarkFailed(
				"Polling repeatedly failed",
				fmt.Sprintf("pollLoop restarted %d times within %v",
					len(restartTimes), pollSupervisorWindow),
				channels.ChannelFailureKindUnknown,
				false,
			)
			c.SetRunning(false)
			return
		}

		restartTimes = append(restartTimes, now)
		slog.Warn("max: pollLoop exited unexpectedly, restarting",
			"channel", c.Name(),
			"restart_count", len(restartTimes),
			"max_restarts", pollSupervisorMaxRestarts,
			"window", pollSupervisorWindow,
			"delay_s", pollSupervisorRestartDelay.Seconds())

		// Wait before restart; abort wait if ctx cancelled mid-sleep.
		select {
		case <-time.After(pollSupervisorRestartDelay):
		case <-ctx.Done():
			return
		}
	}
}

// LastPollAt returns the unix timestamp (seconds) of the most recent
// successful poll. Returns 0 if polling has not yet completed one cycle.
// Thread-safe; reads via atomic load.
func (c *Channel) LastPollAt() int64 {
	return atomic.LoadInt64(&c.lastPollAt)
}

// PollAge returns the duration since the last successful poll.
// Returns -1 if polling has not yet completed one cycle.
// Used by health endpoint and watchdogs to detect stuck polling.
func (c *Channel) PollAge() time.Duration {
	ts := atomic.LoadInt64(&c.lastPollAt)
	if ts == 0 {
		return -1
	}
	return time.Since(time.Unix(ts, 0))
}
