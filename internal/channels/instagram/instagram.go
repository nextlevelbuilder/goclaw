package instagram

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
	webhookPath     = "/channels/instagram/webhook"
	dedupTTL        = 24 * time.Hour  // matches Instagram's max retry window
	dedupCleanEvery = 5 * time.Minute // evict stale dedup entries

	// adminReplyCooldown pauses bot auto-reply for a conversation after the
	// IG account owner manually replies in the Instagram app. Meta delivers
	// those as echo events with sender.ID == ch.instagramID.
	adminReplyCooldown = 5 * time.Minute

	// botEchoWindow classifies an echo event as "bot's own message" when it
	// arrives within this window of a Send(). Anything outside the window
	// delivered as echo is treated as an admin manual reply.
	botEchoWindow = 15 * time.Second
)

// Channel implements channels.Channel and channels.WebhookChannel for Instagram Direct Messaging.
type Channel struct {
	*channels.BaseChannel
	config      instagramInstanceConfig
	graphClient *GraphClient
	webhookH    *WebhookHandler
	instagramID string

	// dedup prevents processing duplicate webhook deliveries.
	dedup sync.Map // eventKey(string) → time.Time

	// adminReplied tracks conversations where the IG account owner replied
	// manually via the Instagram app. Bot skips auto-reply during the cooldown.
	adminReplied sync.Map // chatID(string) → time.Time

	// botSentAt tracks when bot last sent to each conversation, so echo events
	// within botEchoWindow can be classified as bot's own (not admin manual).
	botSentAt sync.Map // chatID(string) → time.Time

	// stopCh + stopCtx for graceful shutdown.
	stopCh   chan struct{}
	stopOnce sync.Once
	stopCtx  context.Context
	stopFn   context.CancelFunc
}

// New creates an Instagram channel from parsed credentials and config.
func New(cfg instagramInstanceConfig, creds instagramCreds,
	msgBus *bus.MessageBus, _ store.PairingStore) (*Channel, error) {

	if creds.UserAccessToken == "" {
		return nil, fmt.Errorf("instagram: user_access_token is required")
	}
	if creds.AppSecret == "" {
		return nil, fmt.Errorf("instagram: app_secret is required")
	}
	if cfg.InstagramUserID == "" {
		return nil, fmt.Errorf("instagram: instagram_user_id is required")
	}
	if creds.VerifyToken == "" {
		return nil, fmt.Errorf("instagram: verify_token is required")
	}

	base := channels.NewBaseChannel(channels.TypeInstagram, msgBus, cfg.AllowFrom)

	graphClient := NewGraphClient(creds.UserAccessToken, cfg.InstagramUserID)

	stopCtx, stopFn := context.WithCancel(context.Background())

	ch := &Channel{
		BaseChannel: base,
		config:      cfg,
		graphClient: graphClient,
		instagramID: cfg.InstagramUserID,
		stopCh:      make(chan struct{}),
		stopCtx:     stopCtx,
		stopFn:      stopFn,
	}

	// onMessage is intentionally nil: the globalRouter's ServeHTTP wraps each
	// instance's WebhookHandler and binds onMessage to per-entry dispatch.
	ch.webhookH = NewWebhookHandler(creds.AppSecret, creds.VerifyToken)

	return ch, nil
}

// Factory creates an Instagram Channel from DB instance data.
func Factory(name string, creds json.RawMessage, cfg json.RawMessage,
	msgBus *bus.MessageBus, pairingSvc store.PairingStore) (channels.Channel, error) {

	var c instagramCreds
	if err := json.Unmarshal(creds, &c); err != nil {
		return nil, fmt.Errorf("instagram: decode credentials: %w", err)
	}

	var ic instagramInstanceConfig
	if len(cfg) > 0 {
		if err := json.Unmarshal(cfg, &ic); err != nil {
			return nil, fmt.Errorf("instagram: decode config: %w", err)
		}
	}

	ch, err := New(ic, c, msgBus, pairingSvc)
	if err != nil {
		return nil, err
	}
	ch.SetName(name)
	return ch, nil
}

// Start connects the channel: verifies token, marks healthy.
func (ch *Channel) Start(ctx context.Context) error {
	ch.MarkStarting("connecting to Instagram account")

	if err := ch.graphClient.VerifyToken(ctx); err != nil {
		ch.MarkFailed("token invalid", err.Error(), channels.ChannelFailureKindAuth, false)
		return err
	}

	globalRouter.register(ch)
	ch.MarkHealthy("connected to Instagram " + ch.instagramID)
	ch.SetRunning(true)

	go ch.runDedupCleaner()

	slog.Info("instagram channel started", "instagram_id", ch.instagramID, "name", ch.Name())
	return nil
}

// Stop gracefully shuts down the channel. Safe to call multiple times.
func (ch *Channel) Stop(_ context.Context) error {
	globalRouter.unregister(ch.instagramID)
	ch.stopOnce.Do(func() {
		ch.stopFn()
		close(ch.stopCh)
	})
	ch.SetRunning(false)
	ch.MarkStopped("stopped")
	slog.Info("instagram channel stopped", "instagram_id", ch.instagramID, "name", ch.Name())
	return nil
}

// Send delivers an outbound message to Instagram DM.
func (ch *Channel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if msg.Content == "" && len(msg.Media) == 0 {
		return nil
	}

	// Send typing indicator if enabled.
	if ch.config.Features.Typing {
		_ = ch.graphClient.SendTypingOn(ctx, msg.ChatID)
	}

	// Skip if admin already replied in this conversation recently.
	if ch.adminRepliedRecently(msg.ChatID, time.Now()) {
		slog.Info("instagram: skipping bot reply (admin already responded)", "chat_id", msg.ChatID)
		return nil
	}

	// Mark bot-sent BEFORE SendMessage so the returning echo event can be
	// classified as bot's own (not admin manual).
	sentAt := time.Now()
	ch.botSentAt.Store(msg.ChatID, sentAt)

	text := FormatMessage(msg.Content)
	_, err := ch.graphClient.SendMessage(ctx, msg.ChatID, text)
	if err != nil {
		ch.botSentAt.Delete(msg.ChatID)
		ch.handleAPIError(err)
		return err
	}

	return nil
}

// adminRepliedRecently reports whether the IG account owner replied in the
// given conversation within adminReplyCooldown.
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

// isBotEcho reports whether an echo event within botEchoWindow of our last
// Send() — i.e., the echo is the bot's own message, not an admin manual reply.
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

// messagingEventTime converts webhook timestamp (seconds or ms epoch) to time.Time.
func messagingEventTime(ts int64) time.Time {
	switch {
	case ts > 1_000_000_000_000:
		return time.UnixMilli(ts)
	case ts > 0:
		return time.Unix(ts, 0)
	default:
		return time.Now()
	}
}

// WebhookHandler returns the webhook path and handler.
func (ch *Channel) WebhookHandler() (string, http.Handler) {
	return globalRouter.webhookRoute()
}

// handleAPIError maps API errors to channel health states.
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

// runDedupCleaner evicts stale dedup entries.
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
				if t, ok := v.(time.Time); ok && now.Sub(t) > botEchoWindow*4 {
					ch.botSentAt.Delete(k)
				}
				return true
			})
		}
	}
}

// isDup checks and records a dedup key.
func (ch *Channel) isDup(key string) bool {
	_, loaded := ch.dedup.LoadOrStore(key, time.Now())
	return loaded
}
