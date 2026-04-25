package pancake

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
	_ channels.Channel           = (*Channel)(nil)
	_ channels.WebhookChannel    = (*Channel)(nil)
	_ channels.BlockReplyChannel = (*Channel)(nil)
)

const (
	dedupTTL        = 24 * time.Hour
	dedupCleanEvery = 5 * time.Minute
	outboundEchoTTL = 45 * time.Second
)

// Channel implements channels.Channel and channels.WebhookChannel for Pancake (pages.fm).
// One channel instance = one Pancake page, which may serve multiple platforms (FB, Zalo, IG, etc.)
type Channel struct {
	*channels.BaseChannel
	config        pancakeInstanceConfig
	apiClient     *APIClient
	pageID        string
	webhookPageID string // native platform ID used in webhook event.page_id (may differ from Pancake internal pageID)
	pageName      string // resolved from Pancake page metadata at Start()
	platform      string // resolved from Pancake page metadata at Start()
	webhookSecret string // optional HMAC-SHA256 secret for webhook verification

	// dedup prevents processing duplicate webhook deliveries.
	dedup sync.Map // eventKey(string) → time.Time

	// recentOutbound suppresses short-lived webhook echoes of our own text replies.
	recentOutbound sync.Map // conversationID + "\x00" + normalized content → time.Time

	// postFetcher fetches and caches page post content for comment context enrichment.
	postFetcher *PostFetcher

	// commentReplyDisabledOnce prevents repeated info logs when COMMENT webhooks
	// arrive but the feature is disabled in channel config.
	commentReplyDisabledOnce sync.Once

	// reactSem bounds concurrent Facebook comment-like calls (cap 10).
	reactSem chan struct{}

	stopCh  chan struct{}
	stopCtx context.Context
	stopFn  context.CancelFunc
}

// New creates a Pancake Channel from parsed credentials and config.
func New(cfg pancakeInstanceConfig, creds pancakeCreds,
	msgBus *bus.MessageBus, _ store.PairingStore) (*Channel, error) {

	if creds.APIKey == "" {
		return nil, fmt.Errorf("pancake: api_key is required")
	}
	if creds.PageAccessToken == "" {
		return nil, fmt.Errorf("pancake: page_access_token is required")
	}
	if cfg.PageID == "" {
		return nil, fmt.Errorf("pancake: page_id is required")
	}

	base := channels.NewBaseChannel(channels.TypePancake, msgBus, cfg.AllowFrom)
	stopCtx, stopFn := context.WithCancel(context.Background())

	apiClient := NewAPIClient(creds.APIKey, creds.PageAccessToken, cfg.PageID)
	ch := &Channel{
		BaseChannel:   base,
		config:        cfg,
		apiClient:     apiClient,
		pageID:        cfg.PageID,
		webhookPageID: cfg.WebhookPageID,
		platform:      cfg.Platform,
		webhookSecret: creds.WebhookSecret,
		postFetcher:   NewPostFetcher(apiClient, cfg.PostContextCacheTTL),
		reactSem:      make(chan struct{}, 10),
		stopCh:        make(chan struct{}),
		stopCtx:       stopCtx,
		stopFn:        stopFn,
	}
	ch.postFetcher.stopCtx = stopCtx

	return ch, nil
}

// Factory creates a Pancake Channel from DB instance data.
// Implements channels.ChannelFactory.
func Factory(name string, creds json.RawMessage, cfg json.RawMessage,
	msgBus *bus.MessageBus, pairingSvc store.PairingStore) (channels.Channel, error) {

	var c pancakeCreds
	if err := json.Unmarshal(creds, &c); err != nil {
		return nil, fmt.Errorf("pancake: decode credentials: %w", err)
	}

	var ic pancakeInstanceConfig
	if len(cfg) > 0 {
		if err := json.Unmarshal(cfg, &ic); err != nil {
			return nil, fmt.Errorf("pancake: decode config: %w", err)
		}
	}

	ch, err := New(ic, c, msgBus, pairingSvc)
	if err != nil {
		return nil, err
	}
	ch.SetName(name)
	return ch, nil
}

// Start connects the channel: verifies token, resolves platform, registers webhook.
func (ch *Channel) Start(ctx context.Context) error {
	ch.MarkStarting("connecting to Pancake page")

	if err := ch.apiClient.VerifyToken(ctx); err != nil {
		ch.MarkFailed("token invalid", err.Error(), channels.ChannelFailureKindAuth, false)
		return err
	}

	// Resolve platform and page name from page metadata (best-effort — don't fail on this).
	if ch.platform == "" || ch.pageName == "" {
		if page, err := ch.apiClient.GetPage(ctx); err != nil {
			slog.Warn("pancake: could not resolve platform from page metadata", "page_id", ch.pageID, "err", err)
		} else {
			if page.Platform != "" {
				slog.Debug("pancake: platform auto-detected; set platform explicitly in config to avoid startup API call",
					"page_id", ch.pageID, "platform", page.Platform)
				ch.platform = page.Platform
			}
			if page.Name != "" {
				ch.pageName = page.Name
			}
		}
	}

	if ch.webhookSecret == "" {
		slog.Warn("security.pancake_webhook_no_secret",
			"page_id", ch.pageID,
			"note", "webhook_secret not configured; incoming webhook requests will not be authenticated")
	}

	// Without HMAC, any actor reaching the webhook endpoint can trigger Pancake API calls.
	if ch.config.Features.AutoReact && ch.webhookSecret == "" {
		slog.Warn("security.pancake_auto_react_without_hmac: auto_react is enabled but "+
			"webhook_secret is not set; configure webhook_secret to prevent "+
			"unauthenticated like-comment triggers",
			"page_id", ch.pageID)
	}

	globalRouter.register(ch)
	ch.MarkHealthy("connected to page " + ch.pageID)
	ch.SetRunning(true)

	// Background goroutine: evict stale dedup entries to prevent memory growth.
	go ch.runDedupCleaner()

	slog.Info("pancake channel started",
		"page_id", ch.pageID,
		"platform", ch.platform,
		"name", ch.Name())
	return nil
}

// Stop gracefully shuts down the channel.
func (ch *Channel) Stop(_ context.Context) error {
	globalRouter.unregister(ch.pageID, ch.webhookPageID)
	ch.stopFn()
	close(ch.stopCh)
	ch.SetRunning(false)
	ch.MarkStopped("stopped")
	slog.Info("pancake channel stopped", "page_id", ch.pageID, "name", ch.Name())
	return nil
}

// Send delivers an outbound message via Pancake API.
// Routes to sendCommentReply or sendInboxReply based on metadata["pancake_mode"].
func (ch *Channel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	slog.Debug("pancake: Send called",
		"page_id", ch.pageID,
		"chat_id", msg.ChatID,
		"content_len", len(msg.Content),
		"platform", ch.platform,
		"channel_name", ch.Name())

	if msg.ChatID == "" {
		return fmt.Errorf("pancake: chat_id (conversation_id) is required for outbound message")
	}

	// NO_REPLY / suppressed-error path: empty content with no media means the
	// caller wants downstream cleanup only. Pancake API rejects empty payloads,
	// so short-circuit before dispatch.
	if msg.Content == "" && len(msg.Media) == 0 {
		return nil
	}

	switch msg.Metadata["pancake_mode"] {
	case "comment":
		return ch.sendCommentReply(ctx, msg)
	default: // "inbox" or unset — existing behavior
		return ch.sendInboxReply(ctx, msg)
	}
}

// sendInboxReply handles outbound inbox messages (existing logic extracted from Send).
func (ch *Channel) sendInboxReply(ctx context.Context, msg bus.OutboundMessage) error {
	conversationID := msg.ChatID
	text := FormatOutbound(msg.Content, ch.platform)

	// Handle media attachments.
	attachmentIDs, err := ch.handleMediaAttachments(ctx, msg)
	if err != nil {
		slog.Warn("pancake: media upload failed, sending text only",
			"page_id", ch.pageID, "err", err)
	}

	// Deliver uploaded media first, then follow with text chunks if needed.
	if len(attachmentIDs) > 0 {
		if err := ch.apiClient.SendAttachmentMessage(ctx, conversationID, attachmentIDs); err != nil {
			ch.handleAPIError(err)
			return err
		}
		if text == "" {
			return nil
		}
	}

	// Text-only: split into platform-appropriate chunks.
	// Store echo fingerprints BEFORE sending so that webhook echoes arriving
	// while the HTTP round-trip is in flight are already recognized as self-sent.
	parts := splitMessage(text, ch.maxMessageLength())
	for _, part := range parts {
		ch.rememberOutboundEcho(conversationID, part)
	}
	for _, part := range parts {
		if err := ch.apiClient.SendMessage(ctx, conversationID, part); err != nil {
			ch.handleAPIError(err)
			ch.forgetOutboundEcho(conversationID, part)
			return err
		}
	}
	return nil
}

// sendCommentReply posts a public reply to a comment and optionally sends a
// one-time private DM to the commenter (best-effort). Stateless — no GoClaw
// dedup state; webhook-level comment_id dedup + FB platform per-comment
// idempotency prevent duplicates.
func (ch *Channel) sendCommentReply(ctx context.Context, msg bus.OutboundMessage) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	conversationID := msg.ChatID

	commentID := msg.Metadata["reply_to_comment_id"]
	if commentID == "" {
		return fmt.Errorf("pancake: reply_to_comment_id missing in outbound metadata for comment reply")
	}

	text := FormatOutbound(msg.Content, ch.platform)
	parts := splitMessage(text, ch.maxMessageLength())
	for _, part := range parts {
		ch.rememberOutboundEcho(conversationID, part)
	}
	for _, part := range parts {
		if err := ch.apiClient.ReplyComment(ctx, conversationID, commentID, part); err != nil {
			ch.handleAPIError(err)
			ch.forgetOutboundEcho(conversationID, part)
			return err
		}
	}

	if ch.config.Features.PrivateReply {
		senderID := msg.Metadata["sender_id"]
		if senderID != "" {
			ch.sendPrivateReply(
				ctx,
				senderID,
				conversationID,
				msg.Metadata["post_id"],
				msg.Metadata["display_name"],
			)
		}
	}

	return nil
}

// sendPrivateReply sends a one-time DM to a commenter (best-effort,
// fire-and-forget). Idempotency relies on the caller-side webhook dedup +
// Facebook's per-comment private_replies endpoint returning an error when a
// DM was already sent — we log the warn and move on.
func (ch *Channel) sendPrivateReply(ctx context.Context, senderID, conversationID, postID, commenterName string) {
	if !ch.config.Features.PrivateReply || senderID == "" {
		return
	}

	postTitle := ""
	if postID != "" && ch.postFetcher != nil {
		if post, perr := ch.postFetcher.GetPost(ctx, postID); perr == nil && post != nil {
			postTitle = post.Message
		}
	}

	message := renderPrivateReplyMessage(ch.config.PrivateReplyMessage, map[string]string{
		"commenter_name": commenterName,
		"post_title":     postTitle,
	})

	if err := ch.apiClient.PrivateReply(ctx, conversationID, message); err != nil {
		slog.Warn("pancake: private_reply send failed",
			"page_id", ch.pageID, "sender_id", senderID, "conv_id", conversationID, "err", err)
		return
	}
	slog.Debug("pancake: private_reply sent",
		"page_id", ch.pageID, "sender_id", senderID, "conv_id", conversationID)
}

// BlockReplyEnabled returns the per-channel block_reply override (nil = inherit gateway default).
func (ch *Channel) BlockReplyEnabled() *bool { return ch.config.BlockReply }

// WebhookHandler returns the shared webhook path and global router as handler.
// Only the first pancake instance mounts the route; others return ("", nil).
func (ch *Channel) WebhookHandler() (string, http.Handler) {
	return globalRouter.webhookRoute()
}

// handleAPIError maps Pancake API errors to channel health states.
func (ch *Channel) handleAPIError(err error) {
	if err == nil {
		return
	}
	switch {
	case isAuthError(err):
		ch.MarkFailed("token expired or invalid", err.Error(), channels.ChannelFailureKindAuth, false)
	case isRateLimitError(err):
		ch.MarkDegraded("rate limited", err.Error(), channels.ChannelFailureKindNetwork, true)
	default:
		ch.MarkDegraded("api error", err.Error(), channels.ChannelFailureKindUnknown, true)
	}
}

// maxMessageLength returns the platform-specific character limit.
func (ch *Channel) maxMessageLength() int {
	switch ch.platform {
	case "tiktok", "shopee":
		return 500
	case "instagram":
		return 1000
	case "facebook", "zalo":
		return 2000
	case "whatsapp":
		return 4096
	case "line":
		return 5000
	default:
		return 2000
	}
}

