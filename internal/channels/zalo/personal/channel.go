package personal

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/replycontext"
	"github.com/nextlevelbuilder/goclaw/internal/channels/typing"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal/protocol"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Channel connects to Zalo Personal Chat via the internal protocol port (from zcago, MIT).
// WARNING: Zalo Personal is an unofficial, reverse-engineered integration. Account may be locked/banned.
type Channel struct {
	*channels.BaseChannel
	config      config.ZaloPersonalConfig
	typingCtrls sync.Map // threadID → *typing.Controller
	replyCache  *replycontext.Cache

	mu       sync.RWMutex // protects sess and listener
	sess     *protocol.Session
	listener *protocol.Listener

	// Pre-loaded credentials (from DB or from file/QR as fallback).
	preloadedCreds *protocol.Credentials

	stopCh   chan struct{}
	stopOnce sync.Once
}

// New creates a new Zalo Personal channel from config.
func New(cfg config.ZaloPersonalConfig, msgBus *bus.MessageBus, pairingSvc store.PairingStore, pendingStore store.PendingMessageStore) (*Channel, error) {
	base := channels.NewBaseChannel(channels.TypeZaloPersonal, msgBus, cfg.AllowFrom)

	if cfg.DMPolicy == "" {
		cfg.DMPolicy = "allowlist"
	}
	if cfg.GroupPolicy == "" {
		cfg.GroupPolicy = "allowlist"
	}
	base.ValidatePolicy(cfg.DMPolicy, cfg.GroupPolicy)

	historyLimit := cfg.HistoryLimit
	if historyLimit == 0 {
		historyLimit = channels.DefaultGroupHistoryLimit
	}

	requireMention := true
	if cfg.RequireMention != nil {
		requireMention = *cfg.RequireMention
	}

	ch := &Channel{
		BaseChannel: base,
		config:      cfg,
		replyCache:  replycontext.NewCache(replycontext.DefaultOptions()),
		stopCh:      make(chan struct{}),
	}
	ch.SetPairingService(pairingSvc)
	ch.SetGroupHistory(channels.MakeHistory(channels.TypeZaloPersonal, pendingStore, base.TenantID()))
	ch.SetHistoryLimit(historyLimit)
	ch.SetRequireMention(requireMention)
	return ch, nil
}

// BlockReplyEnabled returns the per-channel block_reply override (nil = inherit gateway default).
func (c *Channel) BlockReplyEnabled() *bool { return c.config.BlockReply }

// ChatBehaviorConfig returns the per-channel chat_behavior override.
func (c *Channel) ChatBehaviorConfig() *config.ChatBehaviorConfig { return c.config.ChatBehavior }

// session returns the current session snapshot (thread-safe).
func (c *Channel) session() *protocol.Session {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sess
}

// getListener returns the current listener snapshot (thread-safe).
func (c *Channel) getListener() *protocol.Listener {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.listener
}

// ListGroups implements channels.GroupListProvider — returns the real Zalo
// group ID + display name for every group the authenticated account belongs
// to. Lets the agent resolve a group by name (e.g. "Ban Điều Hành") to the
// chat ID the message tool actually requires, instead of guessing/passing
// the display name as target.
//
// Also warms the approvedGroups cache (BaseChannel.MarkGroupApproved) for
// every returned group ID. Send() uses that cache to decide whether a
// target is a group (ThreadTypeGroup) or a user (ThreadTypeUser) when the
// outbound message carries no explicit "group_id" metadata — which is
// exactly the case for a message-tool forward whose ORIGIN session isn't
// itself a group (e.g. forwarding from a DM into a group): the cache is
// otherwise only populated by inbound group traffic under the "pairing"
// group policy, so under "allowlist" (this deployment's default) a
// same-session-never-messaged-before group would default to ThreadTypeUser
// and the send would go nowhere, with no error surfaced anywhere.
func (c *Channel) ListGroups(ctx context.Context) ([]channels.GroupInfo, error) {
	sess := c.session()
	if sess == nil {
		return nil, fmt.Errorf("zalo_personal: not connected")
	}
	groups, err := protocol.FetchGroups(ctx, sess)
	if err != nil {
		return nil, err
	}
	out := make([]channels.GroupInfo, len(groups))
	for i, g := range groups {
		out[i] = channels.GroupInfo{GroupID: g.GroupID, Name: g.Name, TotalMember: g.TotalMember}
		c.MarkGroupApproved(g.GroupID)
	}
	return out, nil
}

// Start authenticates and begins listening for Zalo messages.
func (c *Channel) Start(ctx context.Context) error {
	if gh := c.GroupHistory(); gh != nil {
		gh.StartFlusher()
	}
	slog.Warn("security.unofficial_api",
		"channel", "zalo_personal",
		"msg", "Zalo Personal is unofficial and reverse-engineered. Account may be locked/banned. Use at own risk.",
	)

	sess, err := c.authenticate(ctx)
	if err != nil {
		return fmt.Errorf("zalo_personal auth: %w", err)
	}

	ln, err := protocol.NewListener(sess)
	if err != nil {
		return fmt.Errorf("zalo_personal listener: %w", err)
	}
	if err := ln.Start(ctx); err != nil {
		return fmt.Errorf("zalo_personal listener start: %w", err)
	}

	c.mu.Lock()
	c.sess = sess
	c.listener = ln
	c.mu.Unlock()

	slog.Info("zalo_personal connected", "uid", sess.UID)

	c.SetRunning(true)
	go c.listenLoop(ctx)
	// Best-effort: warm the approvedGroups cache so a forward TO a group works
	// correctly even if its origin session was never itself a group (see
	// ListGroups doc comment). approvedGroups is in-memory only and wiped on
	// every restart, so this must run again on every Start, not just once
	// per account lifetime. Failure here (e.g. transient API error) is not
	// fatal — the cache just stays cold until the next successful listing.
	go func() {
		if _, err := c.ListGroups(ctx); err != nil {
			slog.Warn("zalo_personal: failed to warm group approval cache on start", "error", err)
		}
	}()

	slog.Info("zalo_personal listener loop started")
	return nil
}

// SetPendingCompaction configures LLM-based auto-compaction for pending messages.
func (c *Channel) SetPendingCompaction(cfg *channels.CompactionConfig) {
	if gh := c.GroupHistory(); gh != nil {
		gh.SetCompactionConfig(cfg)
	}
}

// SetPendingHistoryTenantID propagates tenant_id to the pending history for DB operations.
func (c *Channel) SetPendingHistoryTenantID(id uuid.UUID) {
	if gh := c.GroupHistory(); gh != nil {
		gh.SetTenantID(id)
	}
}

// Stop gracefully shuts down the Zalo Personal channel.
func (c *Channel) Stop(_ context.Context) error {
	if gh := c.GroupHistory(); gh != nil {
		gh.StopFlusher()
	}
	slog.Info("stopping zalo_personal channel")
	c.stopOnce.Do(func() { close(c.stopCh) })
	c.typingCtrls.Range(func(key, val any) bool {
		if ctrl, ok := val.(*typing.Controller); ok {
			ctrl.Stop()
		}
		c.typingCtrls.Delete(key)
		return true
	})
	if c.replyCache != nil {
		c.replyCache.Clear()
	}
	if ln := c.getListener(); ln != nil {
		ln.Stop()
	}
	c.SetRunning(false)
	return nil
}
