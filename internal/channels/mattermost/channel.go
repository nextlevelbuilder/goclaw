package mattermost

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	maxMessageLen       = 16383 // Mattermost post limit
	userCacheTTL        = 1 * time.Hour
	healthProbeTimeout  = 2500 * time.Millisecond
	pairingDebounceTime = 60 * time.Second

	// Reconnect backoff parameters
	reconnectBaseDelay = 1 * time.Second
	reconnectMaxDelay  = 30 * time.Second
)

// Channel connects to Mattermost via the APIv4 + WebSocket for event-driven messaging.
type Channel struct {
	*channels.BaseChannel
	client         *MMClient
	wsConn         *MMWebSocketConn
	config         config.MattermostConfig
	botUserID      string // populated on Start() via GetMe()
	botUsername    string // populated on Start() — used for mention gating
	teamID         string // default team ID (resolved on Start)
	requireMention bool

	placeholders   sync.Map // localKey -> postID
	dedup          sync.Map // channelID+postID -> time.Time
	threadParticip sync.Map // channelID+rootID -> time.Time (auto-reply without @mention)
	reactions      sync.Map // chatID:messageID -> *reactionState
	typingCancels  sync.Map // channelID -> context.CancelFunc (periodic typing indicator)

	// High-churn map: sync.Mutex + regular map for debounce timers
	debounceMu     sync.Mutex
	debounceTimers map[string]*debounceEntry

	// Read-heavy map: sync.RWMutex + regular map for user display name cache
	userCacheMu sync.RWMutex
	userCache   map[string]cachedUser

	pairingService store.PairingStore
	groupHistory   *channels.PendingHistory
	historyLimit   int
	debounceDelay  time.Duration
	threadTTL      time.Duration
	wg             sync.WaitGroup
	cancelFn       context.CancelFunc
}

type cachedUser struct {
	displayName string
	username    string
	fetchedAt   time.Time
}

type debounceEntry struct {
	mu       sync.Mutex
	timer    *time.Timer
	content  string
	media    []string
	metadata map[string]string
	senderID string
	chatID   string
	peerKind string
}

// Compile-time interface assertions.
var _ channels.Channel = (*Channel)(nil)
var _ channels.StreamingChannel = (*Channel)(nil)
var _ channels.ReactionChannel = (*Channel)(nil)
var _ channels.BlockReplyChannel = (*Channel)(nil)

// New creates a new Mattermost channel from config.
func New(cfg config.MattermostConfig, msgBus *bus.MessageBus, pairingSvc store.PairingStore, pendingStore store.PendingMessageStore) (*Channel, error) {
	if cfg.ServerURL == "" {
		return nil, fmt.Errorf("mattermost server_url is required")
	}
	if cfg.BotToken == "" {
		return nil, fmt.Errorf("mattermost token is required")
	}

	base := channels.NewBaseChannel(channels.TypeMattermost, msgBus, cfg.AllowFrom)
	base.ValidatePolicy(cfg.DMPolicy, cfg.GroupPolicy)

	requireMention := true
	if cfg.RequireMention != nil {
		requireMention = *cfg.RequireMention
	}

	historyLimit := cfg.HistoryLimit
	if historyLimit == 0 {
		historyLimit = channels.DefaultGroupHistoryLimit
	}

	debounceDelay := time.Duration(cfg.DebounceDelay) * time.Millisecond
	if cfg.DebounceDelay == 0 {
		debounceDelay = 300 * time.Millisecond
	}

	threadTTL := 24 * time.Hour // default: 24h
	if cfg.ThreadTTL != nil {
		if *cfg.ThreadTTL <= 0 {
			threadTTL = 0
		} else {
			threadTTL = time.Duration(*cfg.ThreadTTL) * time.Hour
		}
	}

	return &Channel{
		BaseChannel:    base,
		config:         cfg,
		requireMention: requireMention,
		pairingService: pairingSvc,
		groupHistory:   channels.MakeHistory(channels.TypeMattermost, pendingStore, base.TenantID()),
		historyLimit:   historyLimit,
		debounceDelay:  debounceDelay,
		threadTTL:      threadTTL,
		debounceTimers: make(map[string]*debounceEntry),
		userCache:      make(map[string]cachedUser),
	}, nil
}

// Start opens the API connection, authenticates, and begins the WebSocket event loop.
func (c *Channel) Start(ctx context.Context) error {
	c.groupHistory.StartFlusher()
	slog.Info("starting mattermost bot")

	serverURL := strings.TrimRight(c.config.ServerURL, "/")

	// Create lightweight API client
	c.client = NewMMClient(serverURL, c.config.BotToken)

	// Authenticate — verify token and get bot user info
	me, err := c.client.GetMe(ctx)
	if err != nil {
		return fmt.Errorf("mattermost auth failed (GetMe): %w", err)
	}
	c.botUserID = me.Id
	c.botUsername = me.Username

	// Resolve default team
	if c.config.TeamName != "" {
		team, err := c.client.GetTeamByName(ctx, c.config.TeamName)
		if err != nil {
			return fmt.Errorf("mattermost team %q not found: %w", c.config.TeamName, err)
		}
		c.teamID = team.Id
	} else {
		// Auto-detect: get first team the bot belongs to
		teams, err := c.client.GetTeamsForUser(ctx, c.botUserID)
		if err == nil && len(teams) > 0 {
			c.teamID = teams[0].Id
		}
	}

	// Connect WebSocket for real-time events
	wsConn, err := c.client.ConnectWebSocket(ctx)
	if err != nil {
		return fmt.Errorf("mattermost websocket connect failed: %w", err)
	}
	c.wsConn = wsConn

	smCtx, cancel := context.WithCancel(ctx)
	c.cancelFn = cancel

	c.wg.Add(2) // event loop (with reconnect) + periodic sweep

	// Goroutine 1: Event loop with auto-reconnect
	go func() {
		defer c.wg.Done()
		c.eventLoopWithReconnect(smCtx)
	}()

	// Goroutine 2: Periodic sweep for TTL-based map eviction
	go func() {
		defer c.wg.Done()
		ticker := time.NewTicker(2 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-smCtx.Done():
				return
			case <-ticker.C:
				c.sweepMaps()
			}
		}
	}()

	c.SetRunning(true)
	slog.Info("mattermost bot connected", "user_id", c.botUserID, "username", c.botUsername, "team_id", c.teamID)
	return nil
}

// sweepMaps performs age-based eviction across all TTL-controlled maps.
func (c *Channel) sweepMaps() {
	now := time.Now()

	c.dedup.Range(func(k, v any) bool {
		if now.Sub(v.(time.Time)) > 5*time.Minute {
			c.dedup.Delete(k)
		}
		return true
	})

	if c.threadTTL > 0 {
		c.threadParticip.Range(func(k, v any) bool {
			if now.Sub(v.(time.Time)) > c.threadTTL {
				c.threadParticip.Delete(k)
			}
			return true
		})
	}

	c.userCacheMu.Lock()
	for k, v := range c.userCache {
		if now.Sub(v.fetchedAt) > userCacheTTL {
			delete(c.userCache, k)
		}
	}
	c.userCacheMu.Unlock()
}

// eventLoopWithReconnect wraps the event loop with automatic WebSocket reconnection
// using exponential backoff (1s → 2s → 4s → ... → 30s max).
//
// Design: the original eventLoop would silently exit on WS disconnect, leaving
// the bot in a "zombie" state where it appears running but receives no messages.
// This wrapper ensures the bot auto-heals from transient network failures while
// stopping permanently on fatal auth errors (revoked token, etc.).
func (c *Channel) eventLoopWithReconnect(ctx context.Context) {
	attempt := 0
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		c.processEvents(ctx)

		// Context cancelled = intentional shutdown
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Exponential backoff for reconnect
		attempt++
		delay := time.Duration(float64(reconnectBaseDelay) * math.Pow(2, float64(attempt-1)))
		if delay > reconnectMaxDelay {
			delay = reconnectMaxDelay
		}
		slog.Warn("mattermost websocket disconnected, reconnecting",
			"attempt", attempt, "delay", delay)

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}

		// Attempt reconnect
		wsConn, err := c.client.ConnectWebSocket(ctx)
		if err != nil {
			slog.Error("mattermost websocket reconnect failed", "attempt", attempt, "error", err)
			if isNonRetryableAuthError(err.Error()) {
				slog.Error("mattermost: non-retryable auth error, stopping reconnect loop")
				return
			}
			continue
		}

		// Success — reset attempt counter
		c.wsConn = wsConn
		attempt = 0
		slog.Info("mattermost websocket reconnected successfully")
	}
}

// processEvents reads and dispatches WebSocket events until disconnected.
func (c *Channel) processEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-c.wsConn.EventChannel():
			if !ok {
				// Channel closed — need reconnect
				return
			}
			if evt == nil {
				continue
			}
			c.handleEvent(evt)
		}
	}
}

func (c *Channel) handleEvent(evt *MMWebSocketEvent) {
	switch evt.Event {
	case WebsocketEventPosted:
		c.handlePosted(evt)
	case WebsocketEventPostEdited:
		c.handlePostEdited(evt)
	}
}

// SetPendingCompaction configures LLM-based auto-compaction for pending messages.
func (c *Channel) SetPendingCompaction(cfg *channels.CompactionConfig) {
	c.groupHistory.SetCompactionConfig(cfg)
}

// SetPendingHistoryTenantID propagates tenant_id to the pending history for DB operations.
func (c *Channel) SetPendingHistoryTenantID(id uuid.UUID) { c.groupHistory.SetTenantID(id) }

// BlockReplyEnabled returns the channel-specific block_reply override.
func (c *Channel) BlockReplyEnabled() *bool { return c.config.BlockReply }

// Stop gracefully shuts down the Mattermost channel.
func (c *Channel) Stop(_ context.Context) error {
	c.groupHistory.StopFlusher()
	slog.Info("stopping mattermost bot")
	c.SetRunning(false)

	if c.cancelFn != nil {
		c.cancelFn()
	}

	if c.wsConn != nil {
		c.wsConn.Close()
	}

	// Flush all pending debounce entries before shutdown
	c.debounceMu.Lock()
	pendingKeys := make([]string, 0, len(c.debounceTimers))
	for k, entry := range c.debounceTimers {
		entry.mu.Lock()
		if entry.timer != nil {
			entry.timer.Stop()
		}
		entry.mu.Unlock()
		pendingKeys = append(pendingKeys, k)
	}
	c.debounceMu.Unlock()

	for _, k := range pendingKeys {
		c.flushDebounce(k)
	}

	// Wait for all goroutines with timeout
	doneCh := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(doneCh)
	}()

	select {
	case <-doneCh:
	case <-time.After(10 * time.Second):
		slog.Warn("mattermost bot stop timed out after 10s")
	}

	return nil
}

// resolveDisplayName fetches the display name for a user ID, with caching.
func (c *Channel) resolveDisplayName(userID string) string {
	c.userCacheMu.RLock()
	if cached, ok := c.userCache[userID]; ok && time.Since(cached.fetchedAt) < userCacheTTL {
		c.userCacheMu.RUnlock()
		return cached.displayName
	}
	c.userCacheMu.RUnlock()

	user, err := c.client.GetUser(context.Background(), userID)
	if err != nil {
		slog.Debug("mattermost: failed to resolve display name", "user_id", userID, "error", err)
		return userID
	}

	displayName := user.DisplayName()
	if displayName == "" {
		displayName = user.Username
	}

	c.userCacheMu.Lock()
	c.userCache[userID] = cachedUser{
		displayName: displayName,
		username:    user.Username,
		fetchedAt:   time.Now(),
	}
	c.userCacheMu.Unlock()

	return displayName
}

// isBotMentioned checks if the post formally mentions the bot via props, or if the message text contains @botUsername.
func (c *Channel) isBotMentioned(post *MMPost) bool {
	// 1. Check strict mention array (Mattermost auto-complete sends user IDs here)
	if mentions, ok := post.Props["mentions"].([]interface{}); ok {
		for _, m := range mentions {
			if id, ok := m.(string); ok && id == c.botUserID {
				return true
			}
		}
	}

	// 2. Fallback: text scan for @botUsername
	return strings.Contains(strings.ToLower(post.Message), "@"+strings.ToLower(c.botUsername))
}

// stripBotMention removes @botUsername from text.
func (c *Channel) stripBotMention(text string) string {
	// Case-insensitive replace
	lower := strings.ToLower(text)
	mention := "@" + strings.ToLower(c.botUsername)
	idx := strings.Index(lower, mention)
	if idx < 0 {
		return text
	}
	return strings.TrimSpace(text[:idx] + text[idx+len(mention):])
}
