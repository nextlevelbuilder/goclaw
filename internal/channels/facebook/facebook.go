package facebook

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Compile-time interface assertions.
var (
	_ channels.Channel        = (*Channel)(nil)
	_ channels.WebhookChannel = (*Channel)(nil)
)

const (
	webhookPath        = "/channels/facebook/webhook"
	dedupTTL           = 24 * time.Hour  // matches Facebook's max retry window
	dedupCleanEvery    = 5 * time.Minute // how often to evict stale dedup entries
	adminReplyCooldown = 5 * time.Minute
	botEchoWindow      = 15 * time.Second
)

// Channel implements channels.Channel and channels.WebhookChannel for Facebook Fanpage.
// Supports comment auto-reply, Messenger auto-reply, and first inbox DM.
type Channel struct {
	*channels.BaseChannel
	config      facebookInstanceConfig
	graphClient *GraphClient
	webhookH    *WebhookHandler
	pageID      string

	// dedup prevents processing duplicate webhook deliveries.
	// Value is the time.Time the event was first seen; entries are evicted after dedupTTL.
	dedup sync.Map // eventKey(string) → time.Time

	// firstInboxSent tracks which senders have already received a first-inbox DM (in-memory).
	firstInboxSent sync.Map // senderID(string) → struct{}

	// adminReplied tracks conversations where admin (page) sent a message recently.
	// Bot skips auto-reply for these conversations to avoid duplicate responses.
	adminReplied sync.Map // chatID(string) → time.Time

	// botSentAt tracks when bot last sent a reply to each conversation.
	// Used to distinguish bot replies from admin replies in Graph API checks.
	botSentAt sync.Map // chatID(string) → time.Time

	// postFetcher caches post content to enrich comment context.
	postFetcher *PostFetcher

	// stopCh + stopCtx serve complementary roles:
	// stopCh for select{} in goroutines (runDedupCleaner), stopCtx for HTTP client cancellation.
	stopCh  chan struct{}
	stopCtx context.Context
	stopFn  context.CancelFunc
}

// New creates a Facebook channel from parsed credentials and config.
func New(cfg facebookInstanceConfig, creds facebookCreds,
	msgBus *bus.MessageBus, _ store.PairingStore) (*Channel, error) {

	if creds.PageAccessToken == "" {
		return nil, fmt.Errorf("facebook: page_access_token is required")
	}
	if cfg.PageID == "" {
		return nil, fmt.Errorf("facebook: page_id is required")
	}
	if creds.AppSecret == "" {
		return nil, fmt.Errorf("facebook: app_secret is required")
	}
	if creds.VerifyToken == "" {
		return nil, fmt.Errorf("facebook: verify_token is required")
	}

	base := channels.NewBaseChannel(channels.TypeFacebook, msgBus, cfg.AllowFrom)

	graphClient := NewGraphClient(creds.PageAccessToken, cfg.PageID)
	postFetcher := NewPostFetcher(graphClient, cfg.PostContextCacheTTL)

	stopCtx, stopFn := context.WithCancel(context.Background())

	ch := &Channel{
		BaseChannel: base,
		config:      cfg,
		graphClient: graphClient,
		pageID:      cfg.PageID,
		postFetcher: postFetcher,
		stopCh:      make(chan struct{}),
		stopCtx:     stopCtx,
		stopFn:      stopFn,
	}

	wh := NewWebhookHandler(creds.AppSecret, creds.VerifyToken)
	wh.onComment = ch.handleCommentEvent
	wh.onMessage = ch.handleMessagingEvent
	ch.webhookH = wh

	return ch, nil
}

// Factory creates a Facebook Channel from DB instance data.
// Implements channels.ChannelFactory.
func Factory(name string, creds json.RawMessage, cfg json.RawMessage,
	msgBus *bus.MessageBus, pairingSvc store.PairingStore) (channels.Channel, error) {

	var c facebookCreds
	if err := json.Unmarshal(creds, &c); err != nil {
		return nil, fmt.Errorf("facebook: decode credentials: %w", err)
	}

	var ic facebookInstanceConfig
	if len(cfg) > 0 {
		if err := json.Unmarshal(cfg, &ic); err != nil {
			return nil, fmt.Errorf("facebook: decode config: %w", err)
		}
	}

	ch, err := New(ic, c, msgBus, pairingSvc)
	if err != nil {
		return nil, err
	}
	ch.SetName(name)
	return ch, nil
}

// Start connects the channel: verifies the page token, subscribes webhooks, marks healthy.
func (ch *Channel) Start(ctx context.Context) error {
	ch.MarkStarting("connecting to Facebook page")

	if err := ch.graphClient.VerifyToken(ctx); err != nil {
		ch.MarkFailed("token invalid", err.Error(), channels.ChannelFailureKindAuth, false)
		return err
	}

	// Best-effort: subscribe app to webhooks.
	if err := ch.graphClient.SubscribeApp(ctx); err != nil {
		slog.Warn("facebook: webhook subscription failed (check app install on page)", "err", err)
	}

	globalRouter.register(ch)
	ch.MarkHealthy("connected to page " + ch.pageID)
	ch.SetRunning(true)

	// Background goroutine: evict stale dedup entries to prevent memory growth.
	go ch.runDedupCleaner()

	slog.Info("facebook channel started", "page_id", ch.pageID, "name", ch.Name())
	return nil
}

// Stop gracefully shuts down the channel.
func (ch *Channel) Stop(_ context.Context) error {
	globalRouter.unregister(ch.pageID)
	ch.stopFn()      // cancel stopCtx → cancels inflight Graph API calls
	close(ch.stopCh) // stop background goroutines
	ch.SetRunning(false)
	ch.MarkStopped("stopped")
	slog.Info("facebook channel stopped", "page_id", ch.pageID, "name", ch.Name())
	return nil
}

func (ch *Channel) adminRepliedRecently(chatID string, now time.Time) bool {
	val, ok := ch.adminReplied.Load(chatID)
	if !ok {
		return false
	}
	repliedAt, ok := val.(time.Time)
	if !ok {
		ch.adminReplied.Delete(chatID)
		return false
	}
	if now.Sub(repliedAt) < adminReplyCooldown {
		return true
	}
	ch.adminReplied.Delete(chatID)
	return false
}

func (ch *Channel) isBotEcho(chatID string, eventAt time.Time) bool {
	val, ok := ch.botSentAt.Load(chatID)
	if !ok {
		return false
	}
	sentAt, ok := val.(time.Time)
	if !ok {
		ch.botSentAt.Delete(chatID)
		return false
	}
	return eventAt.Sub(sentAt).Abs() < botEchoWindow
}

// Send delivers an outbound message. Dispatches to comment reply or Messenger based on fb_mode metadata.
func (ch *Channel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	mode := msg.Metadata["fb_mode"]

	switch mode {
	case "messenger":
		if ch.adminRepliedRecently(msg.ChatID, time.Now()) {
			slog.Info("facebook: skipping bot reply (admin already responded)",
				"chat_id", msg.ChatID)
			return nil
		}

		text := FormatForMessenger(msg.Content)
		parts := splitMessage(text, messengerMaxChars)
		sentAt := time.Now()
		ch.botSentAt.Store(msg.ChatID, sentAt)
		sentAny := false
		for _, part := range parts {
			if _, err := ch.graphClient.SendMessage(ctx, msg.ChatID, part); err != nil {
				if !sentAny {
					ch.botSentAt.Delete(msg.ChatID)
				}
				ch.handleAPIError(err)
				return err
			}
			sentAny = true
			ch.botSentAt.Store(msg.ChatID, time.Now())
		}

	default: // "comment"
		commentID := msg.Metadata["reply_to_comment_id"]
		if commentID == "" {
			return fmt.Errorf("facebook: reply_to_comment_id missing in outbound metadata")
		}
		text := FormatForComment(msg.Content)
		if _, err := ch.graphClient.ReplyToComment(ctx, commentID, text); err != nil {
			ch.handleAPIError(err)
			return err
		}

		// First inbox: send a private DM after comment reply (best-effort).
		if ch.config.Features.FirstInbox {
			senderID := msg.Metadata["sender_id"]
			if senderID != "" {
				ch.sendFirstInbox(ctx, senderID)
			}
		}
	}

	return nil
}

// WebhookHandler returns the shared webhook path and the global router as handler.
// Only the first facebook instance mounts the route; others return ("", nil).
func (ch *Channel) WebhookHandler() (string, http.Handler) {
	return globalRouter.webhookRoute()
}

// handleAPIError maps Graph API errors to channel health states.
func (ch *Channel) handleAPIError(err error) {
	if err == nil {
		return
	}
	switch {
	case IsAuthError(err):
		ch.MarkFailed("token expired", err.Error(), channels.ChannelFailureKindAuth, false)
	case IsPermissionError(err):
		ch.MarkFailed("permission denied", err.Error(), channels.ChannelFailureKindAuth, false)
	case IsRateLimitError(err):
		ch.MarkDegraded("rate limited", err.Error(), channels.ChannelFailureKindNetwork, true)
	default:
		ch.MarkDegraded("api error", err.Error(), channels.ChannelFailureKindUnknown, true)
	}
}

// sendFirstInbox sends a one-time private DM after a comment reply (best-effort).
func (ch *Channel) sendFirstInbox(ctx context.Context, senderID string) {
	if _, alreadySent := ch.firstInboxSent.LoadOrStore(senderID, struct{}{}); alreadySent {
		return
	}
	message := ch.config.FirstInboxMessage
	if message == "" {
		message = "Cảm ơn bạn đã comment! Mình có thể hỗ trợ thêm qua tin nhắn riêng."
	}
	if _, err := ch.graphClient.SendMessage(ctx, senderID, message); err != nil {
		slog.Warn("facebook: first inbox send failed", "sender_id", senderID, "err", err)
		ch.firstInboxSent.Delete(senderID) // allow retry on next comment
	}
}

// runDedupCleaner evicts stale entries from dedup, adminReplied, and botSentAt
// maps every dedupCleanEvery to prevent unbounded memory growth.
func (ch *Channel) runDedupCleaner() {
	ticker := time.NewTicker(dedupCleanEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ch.stopCh:
			return
		case <-ticker.C:
			now := time.Now()
			ch.dedup.Range(func(k, v any) bool {
				if t, ok := v.(time.Time); ok && now.Sub(t) > dedupTTL {
					ch.dedup.Delete(k)
				}
				return true
			})
			ch.adminReplied.Range(func(k, v any) bool {
				if t, ok := v.(time.Time); ok && now.Sub(t) > adminReplyCooldown {
					ch.adminReplied.Delete(k)
				}
				return true
			})
			ch.botSentAt.Range(func(k, v any) bool {
				if t, ok := v.(time.Time); ok && now.Sub(t) > botEchoWindow {
					ch.botSentAt.Delete(k)
				}
				return true
			})
		}
	}
}

// isDup checks and records a dedup key. Returns true if the key was already seen.
func (ch *Channel) isDup(key string) bool {
	_, loaded := ch.dedup.LoadOrStore(key, time.Now())
	return loaded
}
