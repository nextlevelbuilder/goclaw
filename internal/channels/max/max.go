package max

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
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

		c.SetRunning(true)
		c.MarkHealthy(connectedSummary(me.Username, me.UserID))

		slog.Info("max: bot connected",
			"channel", c.Name(),
			"bot_id", me.UserID,
			"username", me.Username,
			"first_name", me.FirstName,
		)

		go c.pollLoop(pollCtx)

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
// Day 3 will implement this with chunking, formatting, and media.
func (c *Channel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	return errors.New("max: Send not yet implemented (Day 3 work)")
}

// connectedSummary builds a human-readable status string for the health endpoint.
func connectedSummary(username string, userID int64) string {
	if username != "" {
		return fmt.Sprintf("Connected as @%s (id=%d)", username, userID)
	}
	return fmt.Sprintf("Connected (bot_id=%d)", userID)
}
