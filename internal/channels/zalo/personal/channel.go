package personal

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/typing"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal/protocol"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Channel connects to Zalo Personal Chat via the internal protocol port (from zcago, MIT).
// WARNING: Zalo Personal is an unofficial, reverse-engineered integration. Account may be locked/banned.
type Channel struct {
	*channels.BaseChannel
	config          config.ZaloPersonalConfig
	pairingService  store.PairingStore
	pairingDebounce sync.Map // senderID -> time.Time
	approvedGroups         sync.Map  // groupID → true (in-memory cache for paired groups)
	groupNames             sync.Map  // groupID → name string (cached for contact collection)
	groupNamesRefreshedAt  time.Time // last refresh time (throttle, protected by mu)
	typingCtrls            sync.Map  // threadID → *typing.Controller

	mu       sync.RWMutex // protects sess and listener
	sess     *protocol.Session
	listener *protocol.Listener

	// Pre-loaded credentials (from DB or from file/QR as fallback).
	preloadedCreds *protocol.Credentials

	groupHistory   *channels.PendingHistory
	historyLimit   int
	requireMention bool
	stopCh         chan struct{}
	stopOnce       sync.Once
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

	requireMention := true
	if cfg.RequireMention != nil {
		requireMention = *cfg.RequireMention
	}

	historyLimit := cfg.HistoryLimit
	if historyLimit == 0 {
		historyLimit = channels.DefaultGroupHistoryLimit
	}

	return &Channel{
		BaseChannel:    base,
		config:         cfg,
		pairingService: pairingSvc,
		groupHistory:   channels.MakeHistory(channels.TypeZaloPersonal, pendingStore, base.TenantID()),
		historyLimit:   historyLimit,
		requireMention: requireMention,
		stopCh:         make(chan struct{}),
	}, nil
}

// BlockReplyEnabled returns the per-channel block_reply override (nil = inherit gateway default).
func (c *Channel) BlockReplyEnabled() *bool { return c.config.BlockReply }

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

// Start authenticates and begins listening for Zalo messages.
func (c *Channel) Start(ctx context.Context) error {
	c.groupHistory.StartFlusher()
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

	// Pre-cache group names for contact collection (best-effort).
	go c.cacheGroupNames(sess)

	c.SetRunning(true)
	go c.listenLoop(ctx)

	slog.Info("zalo_personal listener loop started")
	return nil
}

// SetPendingCompaction configures LLM-based auto-compaction for pending messages.
func (c *Channel) SetPendingCompaction(cfg *channels.CompactionConfig) {
	c.groupHistory.SetCompactionConfig(cfg)
}

// SetPendingHistoryTenantID propagates tenant_id to the pending history for DB operations.
func (c *Channel) SetPendingHistoryTenantID(id uuid.UUID) { c.groupHistory.SetTenantID(id) }

// cacheGroupNames fetches group list and caches names for contact collection.
func (c *Channel) cacheGroupNames(sess *protocol.Session) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	groups, err := protocol.FetchGroups(ctx, sess)
	if err != nil {
		slog.Debug("zalo_personal: cache group names failed", "error", err)
		return
	}
	for _, g := range groups {
		if g.Name != "" {
			c.groupNames.Store(g.GroupID, g.Name)
		}
	}
	slog.Info("zalo_personal: cached group names", "count", len(groups))
}

// resolveGroupName returns cached group name. If unknown, triggers async refresh
// and returns empty (name will be available on next message).
func (c *Channel) resolveGroupName(groupID string) string {
	if name, ok := c.groupNames.Load(groupID); ok {
		return name.(string)
	}
	// Unknown group — trigger async refresh (non-blocking, throttled).
	c.refreshGroupNamesOnce()
	return ""
}

// refreshGroupNamesOnce triggers a background group name refresh, throttled to
// at most once per 5 minutes to avoid API spam.
func (c *Channel) refreshGroupNamesOnce() {
	now := time.Now()
	c.mu.RLock()
	last := c.groupNamesRefreshedAt
	c.mu.RUnlock()
	if now.Sub(last) < 5*time.Minute {
		return
	}
	c.mu.Lock()
	// Double-check after acquiring write lock.
	if now.Sub(c.groupNamesRefreshedAt) < 5*time.Minute {
		c.mu.Unlock()
		return
	}
	c.groupNamesRefreshedAt = now
	c.mu.Unlock()

	sess := c.session()
	if sess == nil {
		return
	}
	go c.cacheGroupNames(sess)
}

// Stop gracefully shuts down the Zalo Personal channel.
func (c *Channel) Stop(_ context.Context) error {
	c.groupHistory.StopFlusher()
	slog.Info("stopping zalo_personal channel")
	c.stopOnce.Do(func() { close(c.stopCh) })
	c.typingCtrls.Range(func(key, val any) bool {
		if ctrl, ok := val.(*typing.Controller); ok {
			ctrl.Stop()
		}
		c.typingCtrls.Delete(key)
		return true
	})
	if ln := c.getListener(); ln != nil {
		ln.Stop()
	}
	c.SetRunning(false)
	return nil
}
