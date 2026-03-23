package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mymmrac/telego"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Channel connects to Telegram via the Bot API using long polling.
type Channel struct {
	*channels.BaseChannel
	bot              *telego.Bot
	config           config.TelegramConfig
	httpClient       *http.Client
	transport        *http.Transport
	ipv4Once         sync.Once          // guards enableIPv4Only to prevent data race
	pairingService   store.PairingStore
	agentStore      store.AgentStore              // for agent key lookup (nil if not configured)
	configPermStore store.ConfigPermissionStore   // for group file writer management (nil if not configured)
	teamStore       store.TeamStore               // for /tasks, /task_detail commands (nil if not configured)
	placeholders     sync.Map         // localKey string → messageID int
	stopThinking     sync.Map         // localKey string → *thinkingCancel
	typingCtrls      sync.Map         // localKey string → *typing.Controller
	reactions        sync.Map         // localKey string → *StatusReactionController
	pairingReplySent sync.Map         // userID string → time.Time (debounce pairing replies)
	threadIDs        sync.Map         // localKey string → messageThreadID int (for forum topic routing)
	approvedGroups   sync.Map         // chatIDStr string → true (cached group pairing approval)
	groupHistory     *channels.PendingHistory
	historyLimit     int
	requireMention   bool
	pollCancel       context.CancelFunc // cancels the long polling context
	pollDone         chan struct{}       // closed when polling goroutine exits
	handlerWg        sync.WaitGroup     // tracks in-flight handler goroutines for graceful shutdown
	handlerSem       chan struct{}       // bounded semaphore for concurrent handler goroutines
}

type thinkingCancel struct {
	fn context.CancelFunc
}

func (c *thinkingCancel) Cancel() {
	if c != nil && c.fn != nil {
		c.fn()
	}
}

// New creates a new Telegram channel from config.
// pairingSvc is optional (nil = fall back to allowlist only).
// agentStore is optional (nil = group file writer commands disabled).
// configPermStore is optional (nil = group file writer commands disabled).
// teamStore is optional (nil = /tasks, /task_detail commands disabled).
func New(cfg config.TelegramConfig, msgBus *bus.MessageBus, pairingSvc store.PairingStore, agentStore store.AgentStore, configPermStore store.ConfigPermissionStore, teamStore store.TeamStore, pendingStore store.PendingMessageStore) (*Channel, error) {
	var opts []telego.BotOption

	if cfg.APIServer != "" {
		opts = append(opts, telego.WithAPIServer(cfg.APIServer))
	}

	// Isolate transport per account: prevents cross-bot connection pool contention
	// and allows per-account IPv4 fallback without affecting other bots.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = 64 // default 2 is too low for high-concurrency bots

	if cfg.Proxy != "" {
		proxyURL, parseErr := url.Parse(cfg.Proxy)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid proxy URL %q: %w", cfg.Proxy, parseErr)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}

	httpClient := &http.Client{
		Timeout:   60 * time.Second, // Must exceed getUpdates Timeout to avoid long-poll race (#361)
		Transport: transport,
	}
	// Apply ForceIPv4 at init if configured (explicit, predictable, no runtime heuristic).
	if cfg.ForceIPv4 {
		applyIPv4Dialer(transport)
		slog.Info("telegram: forced IPv4 for account via config")
	}

	opts = append(opts, telego.WithHTTPClient(httpClient))

	bot, err := telego.NewBot(cfg.Token, opts...)
	if err != nil {
		return nil, fmt.Errorf("create telegram bot: %w", err)
	}

	base := channels.NewBaseChannel(channels.TypeTelegram, msgBus, cfg.AllowFrom)
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
		BaseChannel:     base,
		bot:             bot,
		config:          cfg,
		httpClient:      httpClient,
		transport:       transport,
		pairingService:  pairingSvc,
		agentStore:      agentStore,
		configPermStore: configPermStore,
		teamStore:       teamStore,
		groupHistory:    channels.MakeHistory(channels.TypeTelegram, pendingStore, base.TenantID()),
		historyLimit:    historyLimit,
		requireMention:  requireMention,
	}, nil
}

// Start begins long polling for Telegram updates.
func (c *Channel) Start(ctx context.Context) error {
	slog.Info("starting telegram bot (polling mode)")

	// Create a cancellable context for the polling goroutine.
	// Stop() cancels this context to cleanly shut down long polling.
	pollCtx, cancel := context.WithCancel(ctx)
	c.pollCancel = cancel
	c.pollDone = make(chan struct{})

	updates, err := c.bot.UpdatesViaLongPolling(pollCtx, &telego.GetUpdatesParams{
		Timeout: 25, // Long-poll seconds; keep below HTTP client Timeout (#361)
		AllowedUpdates: []string{
			"message",
			"edited_message",
			"callback_query",
			"my_chat_member",
		},
	})
	if err != nil {
		cancel()
		return fmt.Errorf("start long polling: %w", err)
	}

	c.SetRunning(true)
	c.groupHistory.StartFlusher()
	c.handlerSem = make(chan struct{}, 20) // limit concurrent message handlers
	slog.Info("telegram bot connected", "username", c.bot.Username())

	// Register bot menu commands with retry.
	go func() {
		commands := DefaultMenuCommands()
		syncCtx, cancel := context.WithTimeout(pollCtx, probeOverallTimeout)
		defer cancel()

		for attempt := 1; attempt <= 3; attempt++ {
			if err := c.SyncMenuCommands(syncCtx, commands); err != nil {
				slog.Warn("failed to sync telegram menu commands", "error", err, "attempt", attempt)
				if attempt < 3 {
					select {
					case <-syncCtx.Done():
						return
					case <-time.After(time.Duration(attempt*5) * time.Second):
					}
				}
			} else {
				slog.Info("telegram menu commands synced")
				return
			}
		}
	}()

	go func() {
		defer close(c.pollDone)
		for {
			select {
			case <-pollCtx.Done():
				return
			case update, ok := <-updates:
				if !ok {
					slog.Info("telegram updates channel closed")
					return
				}
				if update.Message != nil {
					select {
					case c.handlerSem <- struct{}{}:
						c.handlerWg.Add(1)
						go func(u telego.Update) {
							defer c.handlerWg.Done()
							defer func() { <-c.handlerSem }()
							c.handleMessage(pollCtx, u)
						}(update)
					case <-pollCtx.Done():
						return
					}
				} else if update.CallbackQuery != nil {
					select {
					case c.handlerSem <- struct{}{}:
						c.handlerWg.Add(1)
						go func(q *telego.CallbackQuery) {
							defer c.handlerWg.Done()
							defer func() { <-c.handlerSem }()
							c.handleCallbackQuery(pollCtx, q)
						}(update.CallbackQuery)
					case <-pollCtx.Done():
						return
					}
				} else {
					// Log non-message updates for delivery diagnostics
					updateType := "unknown"
					switch {
					case update.EditedMessage != nil:
						updateType = "edited_message"
					case update.ChannelPost != nil:
						updateType = "channel_post"
					case update.MyChatMember != nil:
						updateType = "my_chat_member"
					case update.ChatMember != nil:
						updateType = "chat_member"
					}
					slog.Debug("telegram update skipped (no message)", "type", updateType, "update_id", update.UpdateID)
				}
			}
		}
	}()

	return nil
}

// StreamEnabled reports whether streaming is active for the given chat type.
// Controlled by separate dm_stream / group_stream config flags (both default false).
//
// DM streaming: uses sendMessageDraft (stealth preview) by default, falls back to
// sendMessage+editMessageText if draft API is unavailable. Controlled by draft_transport config.
// Group streaming: sends a new message, edits progressively, hands off to Send().
func (c *Channel) StreamEnabled(isGroup bool) bool {
	if isGroup {
		return c.config.GroupStream != nil && *c.config.GroupStream
	}
	return c.config.DMStream != nil && *c.config.DMStream
}

// draftTransportEnabled returns whether sendMessageDraft should be used for DM streaming.
// Default: false (disabled). When enabled, uses stealth preview with no per-edit notifications,
// but may cause "reply to deleted message" artifacts on some Telegram clients (tdesktop#10315).
func (c *Channel) draftTransportEnabled() bool {
	if c.config.DraftTransport == nil {
		return false
	}
	return *c.config.DraftTransport
}

// ReasoningStreamEnabled returns whether reasoning should be shown as a separate message.
// Default: true. Set "reasoning_stream": false to hide reasoning (only show answer).
func (c *Channel) ReasoningStreamEnabled() bool {
	if c.config.ReasoningStream == nil {
		return true
	}
	return *c.config.ReasoningStream
}

// BlockReplyEnabled returns the per-channel block_reply override (nil = inherit gateway default).
func (c *Channel) BlockReplyEnabled() *bool { return c.config.BlockReply }

// SetPendingCompaction configures LLM-based auto-compaction for pending messages.
func (c *Channel) SetPendingCompaction(cfg *channels.CompactionConfig) {
	c.groupHistory.SetCompactionConfig(cfg)
}

// SetPendingHistoryTenantID propagates tenant_id to the pending history for DB operations.
func (c *Channel) SetPendingHistoryTenantID(id uuid.UUID) { c.groupHistory.SetTenantID(id) }

// Stop shuts down the Telegram bot by cancelling the long polling context
// and waiting for the polling goroutine to exit.
func (c *Channel) Stop(_ context.Context) error {
	slog.Info("stopping telegram bot")
	c.SetRunning(false)
	c.groupHistory.StopFlusher()

	if c.pollCancel != nil {
		c.pollCancel()
	}

	// Wait for the polling goroutine to fully exit so that
	// Telegram releases the getUpdates lock before a new instance starts.
	if c.pollDone != nil {
		select {
		case <-c.pollDone:
			slog.Info("telegram polling goroutine stopped")
		case <-time.After(10 * time.Second):
			slog.Warn("telegram polling goroutine did not exit within timeout")
		}
	}

	// Wait for in-flight handler goroutines to finish processing.
	handlerDone := make(chan struct{})
	go func() {
		c.handlerWg.Wait()
		close(handlerDone)
	}()
	select {
	case <-handlerDone:
		slog.Info("telegram bot stopped")
	case <-time.After(15 * time.Second):
		slog.Warn("telegram handler goroutines did not drain within timeout")
	}
	return nil
}

// applyIPv4Dialer forces a transport to use IPv4 only by overriding DialContext.
func applyIPv4Dialer(t *http.Transport) {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if network == "tcp" {
			network = "tcp4"
		}
		return dialer.DialContext(ctx, network, addr)
	}
}

// enableIPv4Only forces the bot's transport to use IPv4 only for all future
// requests. Safe to call from multiple goroutines concurrently (uses sync.Once).
func (c *Channel) enableIPv4Only() {
	if c == nil || c.transport == nil {
		return
	}
	c.ipv4Once.Do(func() {
		applyIPv4Dialer(c.transport)
		slog.Info("telegram: enabled sticky IPv4 fallback", "bot", c.bot.Username())
	})
}

// parseChatID converts a string chat ID to int64.
func parseChatID(chatIDStr string) (int64, error) {
	var id int64
	_, err := fmt.Sscanf(chatIDStr, "%d", &id)
	return id, err
}

// parseRawChatID extracts the numeric chat ID from a potentially composite localKey.
// "-12345" → -12345, "-12345:topic:99" → -12345
// TS ref: buildTelegramGroupPeerId() in src/telegram/bot/helpers.ts builds "{chatId}:topic:{topicId}".
func parseRawChatID(key string) (int64, error) {
	raw := key
	if idx := strings.Index(key, ":topic:"); idx > 0 {
		raw = key[:idx]
	} else if idx := strings.Index(key, ":thread:"); idx > 0 {
		raw = key[:idx]
	}
	return parseChatID(raw)
}

// CreateForumTopic creates a new forum topic in a supergroup.
// Implements tools.ForumTopicCreator interface.
func (c *Channel) CreateForumTopic(ctx context.Context, chatID int64, name string, iconColor int, iconEmojiID string) (int, string, error) {
	params := &telego.CreateForumTopicParams{
		ChatID: telego.ChatID{ID: chatID},
		Name:   name,
	}
	if iconColor > 0 {
		params.IconColor = iconColor
	}
	if iconEmojiID != "" {
		params.IconCustomEmojiID = iconEmojiID
	}

	topic, err := c.bot.CreateForumTopic(ctx, params)
	if err != nil {
		return 0, "", fmt.Errorf("telegram API: %w", err)
	}
	return topic.MessageThreadID, topic.Name, nil
}

// telegramGeneralTopicID is the fixed topic ID for the "General" topic in forum supergroups.
// TS ref: TELEGRAM_GENERAL_TOPIC_ID in src/telegram/bot/helpers.ts:12.
const telegramGeneralTopicID = 1

// resolveThreadIDForSend returns the thread ID for Telegram send/edit API calls.
// General topic (1) must be omitted — Telegram rejects it with "thread not found".
// TS ref: buildTelegramThreadParams() in src/telegram/bot/helpers.ts:127-143.
func resolveThreadIDForSend(threadID int) int {
	if threadID == telegramGeneralTopicID {
		return 0
	}
	return threadID
}
