package max

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Channel implements the channels.Channel interface for Max Messenger.
// It embeds channels.BaseChannel for shared policy, allowlist, pairing,
// health, and HandleMessage routing.
type Channel struct {
	*channels.BaseChannel

	creds      instanceCreds
	cfg        instanceConfig
	httpClient *http.Client

	// Optional stores. nil-safe — channel must function with all of these unset
	// for tests and minimal deployments.
	pendingStore store.PendingMessageStore
	audioMgr     *audio.Manager

	// Lifecycle.
	startOnce sync.Once
	stopOnce  sync.Once
	startErr  error
	cancelFn  context.CancelFunc
	pollDone  chan struct{}
	handlerWg sync.WaitGroup

	// Polling cursor — sequence marker returned by Max GET /updates.
	// Persisted in-process for now; consider DB persistence in Day 4 if
	// long-running deployments lose updates on restart.
	markerMu sync.Mutex
	marker   *int64
}

// New constructs a Max channel from validated creds and config.
// It performs no I/O — call Start to begin polling/serving.
//
// Naming follows Telegram pattern: New for the constructor with Option pattern,
// factory.go uses this directly via FactoryWithPendingStoreAndAudio.
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
		// Default: require mention in groups, like other channels.
		base.SetRequireMention(true)
	}

	c := &Channel{
		BaseChannel:  base,
		creds:        creds,
		cfg:          cfg,
		pendingStore: pendingStore,
		audioMgr:     audioMgr,
		// HTTP client with reasonable defaults for Max API.
		// Long-poll requests can take up to 90s; we allow some headroom.
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
		pollDone: make(chan struct{}),
	}

	return c, nil
}

// Start begins listening for inbound messages. Non-blocking after setup.
//
// Day 1: stub — marks channel as running, no polling/webhook yet.
// Day 2 will:
//   - Call client.GetMe() to verify token and populate creds.BotID/Username if missing.
//   - Spawn polling goroutine (cfg.Mode == "polling")
//     OR subscribe webhook via POST /subscriptions (cfg.Mode == "webhook").
func (c *Channel) Start(ctx context.Context) error {
	c.startOnce.Do(func() {
		c.MarkStarting("Starting Max channel")

		runCtx, cancel := context.WithCancel(context.Background())
		c.cancelFn = cancel

		// Day 2: spawn pollLoop or webhook subscription using runCtx.
		_ = runCtx

		c.SetRunning(true)
		c.MarkHealthy("Channel started (skeleton — no I/O yet)")
	})
	return c.startErr
}

// Stop gracefully shuts down the channel. Idempotent.
//
// Day 1: stub — just cancels run context and flips running flag.
// Day 2 will additionally:
//   - Wait for poll loop to exit via <-c.pollDone with timeout.
//   - Drain in-flight handlers via c.handlerWg.Wait().
//   - Optionally unsubscribe webhook (Day 4 — depends on multi-instance semantics).
func (c *Channel) Stop(ctx context.Context) error {
	c.stopOnce.Do(func() {
		if c.cancelFn != nil {
			c.cancelFn()
		}
		// Day 2: wait for polling goroutine to exit.
		// select {
		// case <-c.pollDone:
		// case <-time.After(10 * time.Second):
		//     slog.Warn("max: polling did not stop in time")
		// }
		c.SetRunning(false)
		c.MarkStopped("Channel stopped")
	})
	return nil
}

// Send delivers an outbound message produced by the agent loop.
//
// Day 1: stub — returns ErrNotImplemented. Day 3 will:
//   - Resolve recipient: parse msg.ChatID — DM uses "user:<id>", group uses "chat:<id>".
//   - Format Markdown text per https://dev.max.ru/docs-api#Форматирование текста.
//   - Chunk text by 4000-char limit at sentence/paragraph boundaries.
//   - Upload media via POST /uploads, then attach to message.
//   - POST /messages with chat_id or user_id query param.
func (c *Channel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	return errors.New("max: Send not yet implemented (Day 3 work)")
}
