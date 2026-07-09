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
	"log/slog"
	"sync"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Channel is a single DingTalk robot app bound to one GoClaw agent.
type Channel struct {
	*channels.BaseChannel

	cfg      Config
	audioMgr *audio.Manager

	// dedup guards against DingTalk redelivering a message. Keyed by MsgId,
	// which is stable across server-side resends (unlike the per-delivery
	// frame header id, which the SDK does not surface to us). Entries evict
	// themselves after dedupTTL.
	dedup sync.Map

	stopOnce sync.Once
	stopCh   chan struct{}
}

// Compile-time interface conformance. StreamingChannel is asserted in Phase 6,
// once CreateStream/FinalizeStream exist.
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
		audioMgr:    audioMgr,
		stopCh:      make(chan struct{}),
	}
	ch.SetPairingService(pairingSvc)
	ch.SetGroupHistory(channels.MakeHistory(channels.TypeDingtalk, pendingStore, base.TenantID()))
	ch.SetHistoryLimit(historyLimit)
	ch.SetRequireMention(cfg.RequireMentionOrDefault())

	return ch, nil
}

// Start opens the Stream-mode connection.
//
// Phase 1: no transport yet. The channel reports running so the manager and the
// dashboard treat it as live, but nothing is received or sent.
func (c *Channel) Start(_ context.Context) error {
	c.GroupHistory().StartFlusher()
	slog.Info("starting dingtalk bot", "channel", c.Name())
	c.SetRunning(true)
	c.MarkHealthy("connected")
	return nil
}

// Stop closes the connection and stops background work. Safe to call twice.
func (c *Channel) Stop(_ context.Context) error {
	c.stopOnce.Do(func() { close(c.stopCh) })
	if gh := c.GroupHistory(); gh != nil {
		gh.StopFlusher()
	}
	c.SetRunning(false)
	c.MarkStopped("stopped")
	slog.Info("stopped dingtalk bot", "channel", c.Name())
	return nil
}

// Send delivers an outbound message. Implemented in Phase 4.
func (c *Channel) Send(_ context.Context, _ bus.OutboundMessage) error {
	return nil
}
