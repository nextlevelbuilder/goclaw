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
	bot               *telego.Bot
	config            config.TelegramConfig
	httpClient        *http.Client
	transport         *http.Transport
	ipv4Once          sync.Once // guards enableIPv4Only to prevent data race
	agentStore        store.AgentStore            // for agent key lookup (nil if not configured)
	configPermStore   store.ConfigPermissionStore // for group file writer management (nil if not configured)
	teamStore         store.TeamStore             // for /tasks, /task_detail commands (nil if not configured)
	subagentTaskStore store.SubagentTaskStore     // for /subagents, /subagent commands (nil if not configured)
	placeholders      sync.Map                    // localKey string → messageID int
	stopThinking      sync.Map                    // localKey string → *thinkingCancel
	typingCtrls       sync.Map                    // localKey string → *typing.Controller
	reactions         sync.Map                    // localKey string → *StatusReactionController
	threadIDs         sync.Map                    // localKey string → messageThreadID int (for forum topic routing)
	mentionMode       string             // "strict" (default) or "yield"
	pollCancel        context.CancelFunc // cancels the long polling context
	pollDone          chan struct{}      // closed when polling goroutine exits
	handlerWg         sync.WaitGroup     // tracks in-flight handler goroutines for graceful shutdown
	handlerSem        chan struct{}      // bounded semaphore for concurrent handler goroutines
	pendingDraftID    sync.Map           // localKey string → int (draftID)
	// pairingService, approvedGroups, pairingDebounce, groupHistory, historyLimit, requireMention
	// are inherited from channels.BaseChannel.
}

type thinkingCancel struct {
	fn context.CancelFunc
}

func (c *thinkingCancel) Cancel() {
	if c != nil && c.fn != nil {
		c.fn()
	}
}

// Option configures optional dependencies for the Telegram channel.
type Option func(*Channel)

// WithAgentStore sets the agent store for agent key resolution.
func WithAgentStore(s store.AgentStore) Option { return func(c *Channel) { c.agentStore = s } }

// WithConfigPermStore sets the config permission store for group file writer management.
func WithConfigPermStore(s store.ConfigPermissionStore) Option {
	return func(c *Channel) { c.configPermStore = s }
}

// WithTeamStore sets the team store for /tasks, /task_detail commands.
func WithTeamStore(s store.TeamStore) Option { return func(c *Channel) { c.teamStore = s } }

// WithSubagentTaskStore sets the subagent task store for /subagents, /subagent commands.
func WithSubagentTaskStore(s store.SubagentTaskStore) Option {
	return func(c *Channel) { c.subagentTaskStore = s }
}

// WithPendingMessageStore sets the pending message store for group history buffering.
func WithPendingMessageStore(s store.PendingMessageStore) Option {
	return func(c *Channel) {
		c.SetGroupHistory(channels.MakeHistory(channels.TypeTelegram, s, c.TenantID()))
	}
}

// New creates a new Telegram channel from config.
// pairingSvc is optional (nil = fall back to allowlist only).
// Optional stores are set via Option functions.
func New(cfg config.TelegramConfig, msgBus *bus.MessageBus, pairingSvc store.PairingStore, chanOpts ...Option) (*Channel, error) {
	var botOpts []telego.BotOption

	if cfg.APIServer != "" {
		botOpts = append(botOpts, telego.WithAPIServer(cfg.APIServer))
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

	botOpts = append(botOpts, telego.WithHTTPClient(httpClient))

	bot, err := telego.NewBot(cfg.Token, botOpts...)
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

	mentionMode := cfg.MentionMode
	if mentionMode == "" {
		mentionMode = "strict"
	}
	if mentionMode != "strict" && mentionMode != "yield" {
		slog.Warn("telegram: unknown mention_mode, defaulting to strict", "value", mentionMode)
		mentionMode = "strict"
	}

	ch := &Channel{
		BaseChannel: base,
		bot:         bot,
		config:      cfg,
		httpClient:  httpClient,
		transport:   transport,
		mentionMode: mentionMode,
	}
	ch.SetPairingService(pairingSvc)
	ch.SetGroupHistory(channels.MakeHistory(channels.TypeTelegram, nil, base.TenantID()))
	ch.SetHistoryLimit(historyLimit)
	ch.SetRequireMention(requireMention)
	for _, o := range chanOpts {
		o(ch)
	}
	return ch, nil
}

// Start begins long polling for Telegram updates.
func (c *Channel) Start(ctx context.Context) error {
	slog.Info("starting telegram bot (polling mode)")
	c.MarkStarting("Validating Telegram bot")

	probeCtx, probeCancel := context.WithTimeout(ctx, probeOverallTimeout)
	me, err := c.bot.GetMe(probeCtx)
	probeCancel()
	if err != nil {
		return fmt.Errorf("validate telegram bot: %w", err)
	}
	username := ""
	if me != nil {
		username = me.Username
	}

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
	c.MarkHealthy(connectedSummary(username))
	if gh := c.GroupHistory(); gh != nil {
		gh.StartFlusher()
	}
	c.handlerSem = make(chan struct{}, 20) // limit concurrent message handlers
	slog.Info("telegram bot connected", "username", username)

	// Register bot menu commands with retry.
	go func() {
		commands := DefaultMenuCommands()
		syncCtx, cancel := context.WithTimeout(pollCtx, probeOverallTimeout)
		defer cancel()
		var lastErr error

		for attempt := 1; attempt <= 3; attempt++ {
			if err := c.SyncMenuCommands(syncCtx, commands); err != nil {
				lastErr = err
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
		if lastErr != nil {
			slog.Warn("telegram menu commands remain unsynced", "error", lastErr)
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
					if pollCtx.Err() == nil {
						c.MarkFailed("Polling stopped unexpectedly", "Telegram updates channel closed unexpectedly.", channels.ChannelFailureKindNetwork, true)
					}
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

// Stop shuts down the Telegram bot by cancelling the long polling context
// and waiting for the polling goroutine to exit.
func (c *Channel) Stop(_ context.Context) error {
	slog.Info("stopping telegram bot")
	c.SetRunning(false)
	c.MarkStopped("Stopped")
	if gh := c.GroupHistory(); gh != nil {
		gh.StopFlusher()
	}

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

func connectedSummary(username string) string {
	if username == "" {
		return "Connected"
	}
	return fmt.Sprintf("Connected as @%s", username)
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

// migrateGroupChat handles a Telegram group→supergroup migration by updating
// all DB references (paired_devices, sessions, channel_contacts) and invalidating
// in-memory caches. Safe to call multiple times (idempotent).
func (c *Channel) migrateGroupChat(ctx context.Context, oldChatID, newChatID int64) {
	oldStr := fmt.Sprintf("%d", oldChatID)
	newStr := fmt.Sprintf("%d", newChatID)

	slog.Info("telegram: migrating group chat",
		"old_chat_id", oldStr, "new_chat_id", newStr, "channel", c.Name())

	// Update DB (paired_devices, sessions, channel_contacts).
	if ps := c.PairingService(); ps != nil {
		if err := ps.MigrateGroupChatID(ctx, c.Name(), oldStr, newStr); err != nil {
			slog.Error("telegram: failed to migrate group chat in DB",
				"old_chat_id", oldStr, "new_chat_id", newStr, "error", err)
			return
		}
	}

	// Invalidate approvedGroups cache.
	c.ClearGroupApproval(oldStr)
	c.MarkGroupApproved(newStr)

	// Clear pairing reply debounce for old group sender.
	oldGroupSender := fmt.Sprintf("group:%d", oldChatID)
	c.ClearPairingDebounce(oldGroupSender)

	// Clear in-memory pending history for old key (will rebuild from DB on next access).
	if gh := c.GroupHistory(); gh != nil {
		gh.Clear(oldStr)
	}
}
