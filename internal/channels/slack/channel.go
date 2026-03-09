package slack

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	slackapi "github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	pairingDebounceTime = 60 * time.Second
	maxMessageLen       = 4000 // Slack mrkdwn text limit
	userCacheTTL        = 1 * time.Hour
	healthProbeTimeout  = 2500 * time.Millisecond
)

// Channel connects to Slack via Socket Mode for event-driven messaging.
type Channel struct {
	*channels.BaseChannel
	api            *slackapi.Client      // Bot Token API client (xoxb-)
	userAPI        *slackapi.Client      // User Token API client (xoxp-, optional)
	sm             *socketmode.Client    // Socket Mode client (xapp-)
	config         config.SlackConfig
	botUserID      string // populated on Start() via auth.test
	teamID         string // populated on Start() via auth.test
	requireMention bool   // require @bot in channels (default true)

	placeholders    sync.Map // localKey -> placeholderTS
	dedup           sync.Map // channel+ts -> time.Time
	threadParticip  sync.Map // channelID+threadTS -> time.Time (auto-reply without @mention)
	streams         sync.Map // localKey -> *streamState
	reactions       sync.Map // chatID:messageID -> *reactionState
	pairingDebounce sync.Map // senderID -> time.Time
	approvedGroups  sync.Map // channelID -> true

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
	threadTTL      time.Duration // thread participation expiry (0 = disabled)
	wg             sync.WaitGroup // tracks goroutines for clean shutdown
	cancelFn       context.CancelFunc
}

type cachedUser struct {
	displayName string
	fetchedAt   time.Time
}

// Compile-time interface assertions.
var _ channels.Channel = (*Channel)(nil)
var _ channels.StreamingChannel = (*Channel)(nil)
var _ channels.ReactionChannel = (*Channel)(nil)
var _ channels.BlockReplyChannel = (*Channel)(nil)

// New creates a new Slack channel from config.
func New(cfg config.SlackConfig, msgBus *bus.MessageBus, pairingSvc store.PairingStore) (*Channel, error) {
	if cfg.BotToken == "" {
		return nil, fmt.Errorf("slack bot_token is required")
	}
	if cfg.AppToken == "" {
		return nil, fmt.Errorf("slack app_token is required for Socket Mode")
	}

	// Token prefix validation: catch misconfigured tokens early.
	if !strings.HasPrefix(cfg.BotToken, "xoxb-") {
		return nil, fmt.Errorf("slack bot_token must start with xoxb-")
	}
	if !strings.HasPrefix(cfg.AppToken, "xapp-") {
		return nil, fmt.Errorf("slack app_token must start with xapp-")
	}
	if cfg.UserToken != "" && !strings.HasPrefix(cfg.UserToken, "xoxp-") {
		return nil, fmt.Errorf("slack user_token must start with xoxp-")
	}

	base := channels.NewBaseChannel("slack", msgBus, cfg.AllowFrom)
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
			threadTTL = 0 // explicitly disabled
		} else {
			threadTTL = time.Duration(*cfg.ThreadTTL) * time.Hour
		}
	}

	return &Channel{
		BaseChannel:    base,
		config:         cfg,
		requireMention: requireMention,
		pairingService: pairingSvc,
		groupHistory:   channels.NewPendingHistory(),
		historyLimit:   historyLimit,
		debounceDelay:  debounceDelay,
		threadTTL:      threadTTL,
		debounceTimers: make(map[string]*debounceEntry),
		userCache:      make(map[string]cachedUser),
	}, nil
}

// Start opens the Socket Mode connection and begins receiving events.
func (c *Channel) Start(ctx context.Context) error {
	slog.Info("starting slack bot (socket mode)")

	c.api = slackapi.New(
		c.config.BotToken,
		slackapi.OptionAppLevelToken(c.config.AppToken),
	)

	if c.config.UserToken != "" {
		c.userAPI = slackapi.New(c.config.UserToken)
		slog.Info("slack user token configured (custom identity enabled)")
	}

	authResp, err := c.api.AuthTest()
	if err != nil {
		return fmt.Errorf("slack auth.test failed: %w", err)
	}
	c.botUserID = authResp.UserID
	c.teamID = authResp.TeamID

	c.sm = socketmode.New(
		c.api,
		socketmode.OptionDebug(false),
	)

	smCtx, cancel := context.WithCancel(ctx)
	c.cancelFn = cancel

	c.wg.Add(3) // event loop + RunContext loop + periodic sweep

	// Goroutine 1: Event loop
	go func() {
		defer c.wg.Done()
		c.eventLoop(smCtx)
	}()

	// Goroutine 2: Socket Mode connection with dead socket error classification
	go func() {
		defer c.wg.Done()
		for {
			if err := c.sm.RunContext(smCtx); err != nil {
				if smCtx.Err() != nil {
					return
				}
				if isNonRetryableAuthError(err.Error()) {
					slog.Error("slack: non-retryable auth error, stopping channel", "error", err)
					c.SetRunning(false)
					return
				}
				slog.Warn("slack socket mode error, reconnecting in 5s", "error", err)
				time.Sleep(5 * time.Second)
			}
		}
	}()

	// Goroutine 3: Periodic sweep (every 2 minutes) for TTL-based map eviction
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
	slog.Info("slack bot connected", "user_id", c.botUserID, "team", authResp.Team)
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

	c.pairingDebounce.Range(func(k, v any) bool {
		if now.Sub(v.(time.Time)) > pairingDebounceTime*10 {
			c.pairingDebounce.Delete(k)
		}
		return true
	})

	c.streams.Range(func(k, v any) bool {
		st := v.(*streamState)
		st.mu.Lock()
		stale := now.Sub(st.lastUpdate) > 10*time.Minute
		st.mu.Unlock()
		if stale {
			c.streams.Delete(k)
		}
		return true
	})
}

// eventLoop processes Socket Mode events.
func (c *Channel) eventLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-c.sm.Events:
			if !ok {
				return
			}
			c.handleEvent(evt)
		}
	}
}

func (c *Channel) handleEvent(evt socketmode.Event) {
	switch evt.Type {
	case socketmode.EventTypeEventsAPI:
		c.handleEventsAPI(evt)
	case socketmode.EventTypeDisconnect:
		slog.Info("slack socket mode disconnecting (will auto-reconnect)")
	}
}

// Stop gracefully shuts down the Slack channel.
func (c *Channel) Stop(_ context.Context) error {
	slog.Info("stopping slack bot")
	c.SetRunning(false)

	if c.cancelFn != nil {
		c.cancelFn()
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
		slog.Warn("slack bot stop timed out after 10s")
	}

	return nil
}

// HandleMessage overrides BaseChannel to allow messages when the chatID (Slack channel)
// is in the allowlist, enabling group-level allowlisting without requiring individual user IDs.
// This is Slack-specific: other channels only check senderID in BaseChannel.HandleMessage.
func (c *Channel) HandleMessage(senderID, chatID, content string, mediaPaths []string, metadata map[string]string, peerKind string) {
	// Allow if either the sender or the Slack channel ID is in the allowlist.
	if !c.IsAllowed(senderID) && !c.IsAllowed(chatID) {
		return
	}

	userID := senderID
	if idx := strings.IndexByte(senderID, '|'); idx > 0 {
		userID = senderID[:idx]
	}

	var mediaFiles []bus.MediaFile
	for _, p := range mediaPaths {
		mediaFiles = append(mediaFiles, bus.MediaFile{Path: p})
	}

	c.Bus().PublishInbound(bus.InboundMessage{
		Channel:  c.Name(),
		SenderID: senderID,
		ChatID:   chatID,
		Content:  content,
		Media:    mediaFiles,
		PeerKind: peerKind,
		UserID:   userID,
		Metadata: metadata,
		AgentID:  c.AgentID(),
	})
}

// BlockReplyEnabled returns the per-channel block_reply override.
func (c *Channel) BlockReplyEnabled() *bool { return c.config.BlockReply }

// Send delivers an outbound message to Slack.
func (c *Channel) Send(_ context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return fmt.Errorf("slack bot not running")
	}

	channelID := msg.ChatID
	if channelID == "" {
		return fmt.Errorf("empty chat ID for slack send")
	}

	placeholderKey := channelID
	if pk := msg.Metadata["placeholder_key"]; pk != "" {
		placeholderKey = pk
	}
	threadTS := msg.Metadata["message_thread_id"]

	// Placeholder update (LLM retry notification)
	if msg.Metadata["placeholder_update"] == "true" {
		if pTS, ok := c.placeholders.Load(placeholderKey); ok {
			ts := pTS.(string)
			_, _, _, _ = c.api.UpdateMessage(channelID, ts,
				slackapi.MsgOptionText(msg.Content, false))
		}
		return nil
	}

	content := msg.Content

	// NO_REPLY: delete placeholder, return
	if content == "" {
		if pTS, ok := c.placeholders.Load(placeholderKey); ok {
			c.placeholders.Delete(placeholderKey)
			ts := pTS.(string)
			_, _, _ = c.api.DeleteMessage(channelID, ts)
		}
		return nil
	}

	content = markdownToSlackMrkdwn(content)

	// Edit placeholder with first chunk, send rest as follow-ups
	if pTS, ok := c.placeholders.Load(placeholderKey); ok {
		c.placeholders.Delete(placeholderKey)
		ts := pTS.(string)

		editContent, remaining := splitAtLimit(content, maxMessageLen)

		opts := []slackapi.MsgOption{slackapi.MsgOptionText(editContent, false)}
		if threadTS != "" {
			opts = append(opts, slackapi.MsgOptionTS(threadTS))
		}

		if _, _, _, editErr := c.api.UpdateMessage(channelID, ts, opts...); editErr == nil {
			if remaining != "" {
				return c.sendChunked(channelID, remaining, threadTS)
			}
			return nil
		} else {
			slog.Warn("slack placeholder edit failed, sending new message",
				"channel_id", channelID, "error", editErr)
		}
	}

	// Handle media attachments
	for _, media := range msg.Media {
		if err := c.uploadFile(channelID, threadTS, media); err != nil {
			slog.Warn("slack: file upload failed",
				"file", media.URL, "error", err)
			c.sendChunked(channelID, fmt.Sprintf("[File upload failed: %s]", media.URL), threadTS)
		}
	}

	return c.sendChunked(channelID, content, threadTS)
}

func (c *Channel) sendChunked(channelID, content, threadTS string) error {
	for len(content) > 0 {
		chunk, rest := splitAtLimit(content, maxMessageLen)
		content = rest

		opts := []slackapi.MsgOption{slackapi.MsgOptionText(chunk, false)}
		if threadTS != "" {
			opts = append(opts, slackapi.MsgOptionTS(threadTS))
		}

		if _, _, err := c.api.PostMessage(channelID, opts...); err != nil {
			return fmt.Errorf("send slack message: %w", err)
		}
	}
	return nil
}

// splitAtLimit splits content at maxLen runes, preferring newline boundaries.
func splitAtLimit(content string, maxLen int) (chunk, remaining string) {
	runes := []rune(content)
	if len(runes) <= maxLen {
		return content, ""
	}
	cutAt := maxLen
	// Try to break at a newline in the second half
	candidate := string(runes[:maxLen])
	if idx := strings.LastIndex(candidate, "\n"); idx > len(candidate)/2 {
		return content[:idx+1], content[idx+1:]
	}
	return string(runes[:cutAt]), string(runes[cutAt:])
}

// resolveDisplayName fetches and caches the Slack display name for a user ID.
func (c *Channel) resolveDisplayName(userID string) string {
	c.userCacheMu.RLock()
	cu, found := c.userCache[userID]
	c.userCacheMu.RUnlock()

	if found && time.Since(cu.fetchedAt) < userCacheTTL {
		return cu.displayName
	}

	user, err := c.api.GetUserInfo(userID)
	if err != nil {
		slog.Debug("slack: failed to resolve user", "user_id", userID, "error", err)
		return userID
	}

	name := user.Profile.DisplayName
	if name == "" {
		name = user.RealName
	}
	if name == "" {
		name = user.Name
	}

	c.userCacheMu.Lock()
	c.userCache[userID] = cachedUser{displayName: name, fetchedAt: time.Now()}
	c.userCacheMu.Unlock()

	return name
}

// nonRetryableAuthErrors matches Slack errors that indicate permanent auth failure.
var nonRetryableAuthErrors = regexp.MustCompile(
	`(?i)(invalid_auth|token_revoked|account_inactive|not_authed|team_not_found|missing_scope)`,
)

func isNonRetryableAuthError(errMsg string) bool {
	return nonRetryableAuthErrors.MatchString(errMsg)
}

// HealthProbe performs an auth.test call to verify the Slack connection is alive.
func (c *Channel) HealthProbe(ctx context.Context) (ok bool, elapsed time.Duration, err error) {
	if c.api == nil {
		return false, 0, fmt.Errorf("slack client not initialized (Start() not called)")
	}

	start := time.Now()
	probeCtx, cancel := context.WithTimeout(ctx, healthProbeTimeout)
	defer cancel()

	_, err = c.api.AuthTestContext(probeCtx)
	elapsed = time.Since(start)
	return err == nil, elapsed, err
}
