package mattermost

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	mattermostMaxMessageLen = 16383 // Mattermost's max chars per post (16383)
	pollInterval           = 3 * time.Second
)

// Channel connects to Mattermost via REST API for both DM and channel messaging.
// It polls for new DM messages and posts agent responses back to the DM channel.
type Channel struct {
	*channels.BaseChannel
	config     config.MattermostConfig
	httpClient *http.Client
	serverURL  string // base URL (no trailing slash)
	botToken   string // personal access token or bot token
	botUserID  string // bot's Mattermost user ID
	teamID     string // team ID (for team operations if needed)

	pollCancel context.CancelFunc
	pollDone   chan struct{}
	handlerWg  sync.WaitGroup
	handlerSem chan struct{}

	// DM channel last-seen tracking: userID → last create_at seen
	dmLastTimes  map[string]int64 // sender user ID → last create_at
	dmLastTimesMu sync.Mutex

	// Cache: sender userID → DM channel ID (for fast outbound routing)
	dmChannels   map[string]string // sender userID → DM channel ID
	dmChannelsMu sync.RWMutex
}

// Option configures optional dependencies.
type Option func(*Channel)

func WithAgentStore(s store.AgentStore) Option {
	return func(c *Channel) { /* available via BaseChannel.ContactCollector */ }
}

// New creates a new Mattermost channel from config.
func New(cfg config.MattermostConfig, msgBus *bus.MessageBus, chanOpts ...Option) (*Channel, error) {
	if cfg.ServerURL == "" {
		return nil, fmt.Errorf("mattermost server_url is required")
	}
	if cfg.BotToken == "" {
		return nil, fmt.Errorf("mattermost bot_token is required")
	}

	serverURL := strings.TrimRight(cfg.ServerURL, "/")

	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	base := channels.NewBaseChannel(channels.TypeMattermost, msgBus, cfg.AllowFrom)
	base.ValidatePolicy(cfg.DMPolicy, cfg.GroupPolicy)

	ch := &Channel{
		BaseChannel: base,
		config:      cfg,
		httpClient:  httpClient,
		serverURL:   serverURL,
		botToken:    cfg.BotToken,
		teamID:      cfg.TeamID,
		handlerSem:  make(chan struct{}, 5),
		dmLastTimes: make(map[string]int64),
		dmChannels:  make(map[string]string),
	}

	for _, o := range chanOpts {
		o(ch)
	}

	return ch, nil
}

// Start begins polling for new DM messages.
func (c *Channel) Start(ctx context.Context) error {
	// Verify bot credentials and get user ID
	if err := c.verifyBot(ctx); err != nil {
		return fmt.Errorf("mattermost bot verification failed: %w", err)
	}

	// Start polling
	pollCtx, cancel := context.WithCancel(ctx)
	c.pollCancel = cancel
	c.pollDone = make(chan struct{})

	c.SetRunning(true)
	slog.Info("mattermost channel started (DM mode)",
		"server", c.serverURL,
		"bot_user_id", c.botUserID,
	)

	go c.pollLoop(pollCtx)

	return nil
}

// Stop gracefully shuts down the channel.
func (c *Channel) Stop(_ context.Context) error {
	c.SetRunning(false)
	if c.pollCancel != nil {
		c.pollCancel()
	}
	if c.pollDone != nil {
		select {
		case <-c.pollDone:
		case <-time.After(10 * time.Second):
		}
	}
	done := make(chan struct{})
	go func() { c.handlerWg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
	}
	return nil
}

// verifyBot calls GET /api/v4/users/me to verify the token and get the bot's user ID.
func (c *Channel) verifyBot(ctx context.Context) error {
	body, err := c.apiGet(ctx, "/api/v4/users/me")
	if err != nil {
		return err
	}

	var user struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		IsBot    bool   `json:"is_bot"`
	}
	if err := json.Unmarshal(body, &user); err != nil {
		return fmt.Errorf("parse user response: %w", err)
	}

	if user.ID == "" {
		return fmt.Errorf("empty user ID from /users/me")
	}

	c.botUserID = user.ID
	slog.Info("mattermost bot verified", "user_id", user.ID, "username", user.Username, "is_bot", user.IsBot)
	return nil
}

// pollLoop continuously polls for new DM messages.
func (c *Channel) pollLoop(ctx context.Context) {
	defer close(c.pollDone)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("mattermost poll loop stopped")
			return
		case <-ticker.C:
			c.pollDMs(ctx)
		}
	}
}

// pollDMs checks for new DM messages addressed to this bot.
func (c *Channel) pollDMs(ctx context.Context) {
	// GET /api/v4/users/{user_id}/channels — returns all channels including DMs (type "D")
	body, err := c.apiGet(ctx, "/api/v4/users/"+c.botUserID+"/channels")
	if err != nil {
		slog.Debug("mattermost: failed to fetch channels", "error", err)
		return
	}

	var allChannels []struct {
		ID      string `json:"id"`
		Type    string `json:"type"` // "O"=public, "P"=private, "D"=direct
		Display string `json:"display_name"`
	}
	if err := json.Unmarshal(body, &allChannels); err != nil {
		slog.Debug("mattermost: failed to parse channels", "error", err)
		return
	}

	for _, ch := range allChannels {
		// Only poll DM channels
		if ch.Type != "D" {
			continue
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
		c.pollDMChannel(ctx, ch.ID)
	}
}

// pollDMChannel fetches new messages from a single DM channel.
func (c *Channel) pollDMChannel(ctx context.Context, channelID string) {
	// Use a per-channel watermark via channelID
	c.dmLastTimesMu.Lock()
	since, exists := c.dmLastTimes[channelID]
	c.dmLastTimesMu.Unlock()

	if !exists {
		// First time seeing this channel — skip history, set to now-60s
		since = time.Now().UnixMilli() - 60000
		c.dmLastTimesMu.Lock()
		c.dmLastTimes[channelID] = since
		c.dmLastTimesMu.Unlock()
		return // skip first poll (just sets the watermark)
	}

	path := fmt.Sprintf("/api/v4/channels/%s/posts?since=%d&per_page=30", channelID, since)
	body, err := c.apiGet(ctx, path)
	if err != nil {
		return
	}

	var resp struct {
		Order []string `json:"order"`
		Posts map[string]struct {
			ID        string `json:"id"`
			CreateAt  int64  `json:"create_at"`
			Message   string `json:"message"`
			UserID    string `json:"user_id"`
			ChannelID string `json:"channel_id"`
			Type      string `json:"type"`
		} `json:"posts"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return
	}

	if len(resp.Order) == 0 {
		return
	}

	maxCreateAt := since
	for i := len(resp.Order) - 1; i >= 0; i-- {
		postID := resp.Order[i]
		post, ok := resp.Posts[postID]
		if !ok {
			continue
		}

		if post.CreateAt <= since {
			continue
		}

		// Skip our own messages
		if post.UserID == c.botUserID {
			if post.CreateAt > maxCreateAt {
				maxCreateAt = post.CreateAt
			}
			continue
		}

		// Skip system messages
		if strings.HasPrefix(post.Type, "system_") {
			continue
		}

		// Skip empty messages
		if strings.TrimSpace(post.Message) == "" {
			continue
		}

		if post.CreateAt > maxCreateAt {
			maxCreateAt = post.CreateAt
		}

		// Cache: sender → DM channel ID for outbound routing
		c.dmChannelsMu.Lock()
		c.dmChannels[post.UserID] = post.ChannelID
		c.dmChannelsMu.Unlock()

		// Dispatch
		c.handlerSem <- struct{}{}
		c.handlerWg.Add(1)
		go func(post struct {
			ID        string `json:"id"`
			CreateAt  int64  `json:"create_at"`
			Message   string `json:"message"`
			UserID    string `json:"user_id"`
			ChannelID string `json:"channel_id"`
			Type      string `json:"type"`
		}) {
			defer c.handlerWg.Done()
			defer func() { <-c.handlerSem }()
			c.handleMessage(ctx, post.UserID, post.ChannelID, post.Message, post.ID)
		}(post)
	}

	if maxCreateAt > since {
		c.dmLastTimesMu.Lock()
		c.dmLastTimes[channelID] = maxCreateAt
		c.dmLastTimesMu.Unlock()
	}
}

// handleMessage processes an inbound DM from Mattermost.
func (c *Channel) handleMessage(ctx context.Context, senderID, channelID, message, messageID string) {
	senderName := c.getUserName(ctx, senderID)

	// DM = direct peer kind
	peerKind := "direct"

	metadata := map[string]string{
		"message_id":      messageID,
		"channel_id":      channelID,
		"sender_name":     senderName,
		"sender_id":       senderID,
		"platform":        "mattermost",
		"mattermost_team": c.teamID,
	}

	slog.Info("mattermost inbound DM",
		"sender_id", senderID,
		"sender_name", senderName,
		"channel_id", channelID,
		"message_len", len(message),
	)

	// Forward to agent runtime — chatID = DM channel ID so outbound knows where to reply
	c.HandleMessage(senderID, channelID, message, nil, metadata, peerKind)
}

// getUserName fetches a user's display name via the API.
func (c *Channel) getUserName(ctx context.Context, userID string) string {
	if userID == "" {
		return "unknown"
	}
	body, err := c.apiGet(ctx, "/api/v4/users/"+userID)
	if err != nil {
		return userID
	}
	var user struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name,omitempty"`
		Nickname    string `json:"nickname,omitempty"`
	}
	if err := json.Unmarshal(body, &user); err != nil {
		return userID
	}
	if user.DisplayName != "" {
		return user.DisplayName
	}
	if user.Nickname != "" {
		return user.Nickname
	}
	return user.Username
}
