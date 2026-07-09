// Package dingtalk implements a DingTalk (钉钉) channel over Stream mode.
//
// Stream mode is a long-lived WebSocket connection to DingTalk's gateway, so
// the gateway needs no public IP and no filed callback domain. Inbound robot
// messages arrive on that socket; replies go back over the per-message session
// webhook or, when it has expired, the proactive robot OpenAPI.
//
// The reference implementation is DingTalk's official OpenClaw connector
// (TypeScript). GoClaw is a Go port of OpenClaw, so its channel config is
// already congruent — dm_policy, group_policy, require_mention and friends mean
// the same thing on both sides. What could not be reused is the code: there is
// no Node plugin host here, and channels register at compile time.
//
// Structural sibling: internal/channels/feishu. The notable divergence is
// transport — Lark ships no Go SDK so Feishu hand-rolls a WebSocket client and
// protobuf codec, whereas DingTalk's official Go SDK handles the socket,
// heartbeat, and reconnect for us (wired in Phase 2).
package dingtalk

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Channel is a single DingTalk robot app bound to one GoClaw agent.
type Channel struct {
	*channels.BaseChannel

	cfg      Config
	client   *Client
	audioMgr *audio.Manager

	// newTransport builds the Stream-mode connection. It is a field rather
	// than a direct call so tests can substitute a fake and drive the inbound
	// pipeline without a socket.
	newTransport func(h chatbot.IChatBotMessageHandler) streamTransport

	// stateMu guards transport and runCtx. Start and Stop genuinely race: the
	// InstanceLoader runs Start on its own goroutine and calls Stop from another
	// when Start overruns its timeout (instance_loader.go:411-454), so Stop can
	// read transport while Start is still assigning it.
	stateMu   sync.Mutex
	transport streamTransport

	// runCtx is the channel-scoped context for inbound work; the SDK callback's
	// own ctx dies with the frame. runCancel unblocks that work at shutdown.
	runCtx    context.Context
	runCancel context.CancelFunc

	// cardLimiter paces AI Card writes. Per-channel, because DingTalk meters the
	// card API per app and one channel instance is exactly one app.
	cardLimiter *cardRateLimiter

	// chatMeta maps a chatID to the routing facts CreateStream needs but the
	// key cannot carry (conversation id, group-ness).
	chatMeta sync.Map

	// cards holds a finished card per chatID, between FinalizeStream and the
	// Send() that repaints it with the final answer.
	cards sync.Map

	// liveCards tracks every open card so Stop() can drive them terminal rather
	// than leaving them spinning at INPUTING forever.
	liveCards sync.Map

	// dedup guards against DingTalk redelivering a message. Keyed by MsgId,
	// which is stable across server-side resends (unlike the per-delivery
	// frame header id, which the SDK does not surface to us). Entries evict
	// themselves after dedupTTL.
	dedup sync.Map

	// now is the clock. A field so the session-webhook expiry check is testable
	// without sleeping.
	now func() time.Time

	stopOnce sync.Once
	stopCh   chan struct{}
}

// Compile-time interface conformance. StreamingChannel is asserted in
// stream_channel.go.
var _ channels.Channel = (*Channel)(nil)

// New builds a DingTalk channel from a resolved config.
func New(cfg Config, msgBus *bus.MessageBus, pairingSvc store.PairingStore,
	pendingStore store.PendingMessageStore, audioMgr *audio.Manager) (*Channel, error) {

	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	base := channels.NewBaseChannel(channels.TypeDingtalk, msgBus, cfg.AllowFrom)
	base.ValidatePolicy(cfg.DMPolicy, cfg.GroupPolicy)

	historyLimit := cfg.HistoryLimit
	if historyLimit == 0 {
		historyLimit = channels.DefaultGroupHistoryLimit
	}

	ch := &Channel{
		BaseChannel: base,
		cfg:         cfg,
		client:      NewClient(cfg.ClientID, cfg.ClientSecret),
		audioMgr:    audioMgr,
		cardLimiter: newCardRateLimiter(),
		now:         time.Now,
		stopCh:      make(chan struct{}),
	}
	ch.newTransport = func(h chatbot.IChatBotMessageHandler) streamTransport {
		return newSDKTransport(cfg.ClientID, cfg.ClientSecret, cfg.Endpoint, h)
	}
	ch.SetPairingService(pairingSvc)
	ch.SetGroupHistory(channels.MakeHistory(channels.TypeDingtalk, pendingStore, base.TenantID()))
	ch.SetHistoryLimit(historyLimit)
	ch.SetRequireMention(cfg.RequireMentionOrDefault())

	return ch, nil
}

// Start opens the Stream-mode connection.
//
// The SDK's Start dials and returns; its read and keepalive goroutines outlive
// this call. A dial failure is returned rather than retried: the manager records
// it as a start failure and keeps booting the other channels, so a bad AppKey
// surfaces on the dashboard instead of hiding behind an infinite retry loop.
func (c *Channel) Start(ctx context.Context) error {
	c.GroupHistory().StartFlusher()
	slog.Info("starting dingtalk bot", "channel", c.Name())

	c.stateMu.Lock()
	c.runCtx, c.runCancel = context.WithCancel(ctx)
	transport := c.newTransport(c.handleBotMessage)
	c.transport = transport
	c.stateMu.Unlock()

	if err := transport.Start(ctx); err != nil {
		c.GroupHistory().StopFlusher()
		// The failure kind is genuinely ambiguous: the SDK negotiates the
		// socket endpoint with the app credentials, so a wrong AppKey and an
		// unreachable network surface identically here. Reporting either one
		// would mislead half the time; the detail string carries the truth.
		c.MarkFailed("stream connect failed", err.Error(), channels.ChannelFailureKindUnknown, true)
		return fmt.Errorf("dingtalk: start stream: %w", err)
	}

	c.SetRunning(true)
	c.MarkHealthy("connected")
	return nil
}

// Stop closes the connection and stops background work. Safe to call twice, and
// safe on a channel whose Start failed — the InstanceLoader calls Stop on a
// partially-started channel after a start timeout.
func (c *Channel) Stop(ctx context.Context) error {
	c.stopOnce.Do(func() { close(c.stopCh) })

	// Finalize cards before cancelling runCtx: this needs the network, and the
	// callers that matter (gateway_lifecycle.go:201, instance_loader.go:430)
	// hand us a live context for exactly that reason.
	c.finalizeLiveCards(ctx)

	c.stateMu.Lock()
	transport, cancel := c.transport, c.runCancel
	c.stateMu.Unlock()

	// Unblock any inbound goroutine still waiting on a saturated bus.
	if cancel != nil {
		cancel()
	}
	if transport != nil {
		transport.Close()
	}
	if gh := c.GroupHistory(); gh != nil {
		gh.StopFlusher()
	}
	c.SetRunning(false)
	c.MarkStopped("stopped")
	slog.Info("stopped dingtalk bot", "channel", c.Name())
	return nil
}

// handleBotMessage lives in inbound.go.
