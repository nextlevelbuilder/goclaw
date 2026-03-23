// Package feishu implements the Feishu/Lark channel using native HTTP + WebSocket.
// Supports: DM + Group, WebSocket + Webhook, mentions, media, streaming cards.
// Default domain: Lark Global (open.larksuite.com).
package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
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
	defaultTextChunkLimit = 4000
	defaultMediaMaxMB     = 30
	defaultWebhookPort    = 3000
	defaultWebhookPath    = "/feishu/events"
	senderCacheTTL        = 10 * time.Minute
	pairingDebounceTime   = 60 * time.Second
)

// Channel connects to Feishu/Lark via native HTTP + WebSocket.
type Channel struct {
	*channels.BaseChannel
	cfg             config.FeishuConfig
	client          *LarkClient
	botOpenID       string
	pairingService  store.PairingStore
	senderCache     sync.Map // open_id → *senderCacheEntry
	dedup           sync.Map // message_id → struct{}
	pairingDebounce sync.Map // senderID → time.Time
	reactions       sync.Map // chatID → *reactionState
	approvedGroups  sync.Map // chatID → true (in-memory cache for paired groups)
	groupAllowList  []string
	groupHistory    *channels.PendingHistory
	historyLimit    int
	stopCh          chan struct{}
	httpServer      *http.Server
	wsClient        *WSClient
}

// reactionState tracks an active typing reaction on a user's message.
type reactionState struct {
	messageID  string // Lark message ID (om_xxx)
	reactionID string // reaction ID returned by API for deletion
}

type senderCacheEntry struct {
	name      string
	expiresAt time.Time
}

// New creates a new Feishu/Lark channel.
func New(cfg config.FeishuConfig, msgBus *bus.MessageBus, pairingSvc store.PairingStore, pendingStore store.PendingMessageStore) (*Channel, error) {
	if cfg.AppID == "" || cfg.AppSecret == "" {
		return nil, fmt.Errorf("feishu app_id and app_secret are required")
	}

	// Resolve domain
	domain := resolveDomain(cfg.Domain)

	client := NewLarkClient(cfg.AppID, cfg.AppSecret, domain)

	base := channels.NewBaseChannel(channels.TypeFeishu, msgBus, cfg.AllowFrom)
	base.ValidatePolicy(cfg.DMPolicy, cfg.GroupPolicy)

	historyLimit := cfg.HistoryLimit
	if historyLimit == 0 {
		historyLimit = channels.DefaultGroupHistoryLimit
	}

	return &Channel{
		BaseChannel:    base,
		cfg:            cfg,
		client:         client,
		pairingService: pairingSvc,
		groupAllowList: cfg.GroupAllowFrom,
		groupHistory:   channels.MakeHistory(channels.TypeFeishu, pendingStore, base.TenantID()),
		historyLimit:   historyLimit,
		stopCh:         make(chan struct{}),
	}, nil
}

// Start begins receiving Feishu events via WebSocket or Webhook.
func (c *Channel) Start(ctx context.Context) error {
	c.groupHistory.StartFlusher()
	slog.Info("starting feishu/lark bot")

	// Probe bot identity
	if err := c.probeBotInfo(ctx); err != nil {
		slog.Warn("feishu bot probe failed (will continue)", "error", err)
	} else {
		slog.Info("feishu bot connected", "bot_open_id", c.botOpenID)
	}

	mode := c.cfg.ConnectionMode
	if mode == "" {
		mode = "websocket"
	}

	c.SetRunning(true)

	switch mode {
	case "webhook":
		return c.startWebhook(ctx)
	default: // "websocket"
		return c.startWebSocket(ctx)
	}
}

// BlockReplyEnabled returns the per-channel block_reply override (nil = inherit gateway default).
func (c *Channel) BlockReplyEnabled() *bool { return c.cfg.BlockReply }

// SetPendingCompaction configures LLM-based auto-compaction for pending messages.
func (c *Channel) SetPendingCompaction(cfg *channels.CompactionConfig) {
	c.groupHistory.SetCompactionConfig(cfg)
}

// SetPendingHistoryTenantID propagates tenant_id to the pending history for DB operations.
func (c *Channel) SetPendingHistoryTenantID(id uuid.UUID) { c.groupHistory.SetTenantID(id) }

// Stop shuts down the Feishu channel.
func (c *Channel) Stop(_ context.Context) error {
	c.groupHistory.StopFlusher()
	slog.Info("stopping feishu/lark bot")
	close(c.stopCh)

	if c.wsClient != nil {
		c.wsClient.Stop()
	}

	if c.httpServer != nil {
		c.httpServer.Close()
	}

	c.SetRunning(false)
	return nil
}

// Send delivers an outbound message to a Feishu chat.
func (c *Channel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return fmt.Errorf("feishu bot not running")
	}

	chatID := msg.ChatID
	if chatID == "" {
		return fmt.Errorf("empty chat ID for feishu send")
	}

	// Determine receive_id_type
	receiveIDType := resolveReceiveIDType(chatID)

	// Send text content
	text := msg.Content
	if text != "" {
		// Resolve render mode
		renderMode := c.cfg.RenderMode
		if renderMode == "" {
			renderMode = "auto"
		}

		useCard := false
		switch renderMode {
		case "card":
			useCard = true
		case "auto":
			useCard = shouldUseCard(text)
		}

		chunkLimit := c.cfg.TextChunkLimit
		if chunkLimit <= 0 {
			chunkLimit = defaultTextChunkLimit
		}

		if useCard {
			if err := c.sendMarkdownCard(ctx, chatID, receiveIDType, text, nil); err != nil {
				return err
			}
		} else {
			if err := c.sendChunkedText(ctx, chatID, receiveIDType, text, chunkLimit); err != nil {
				return err
			}
		}
	}

	// Send media attachments
	for _, media := range msg.Media {
		if err := c.sendMediaAttachment(ctx, chatID, receiveIDType, media); err != nil {
			slog.Warn("feishu send media failed", "url", media.URL, "error", err)
		}
	}

	return nil
}

// --- Connection modes ---

// wsEventAdapter adapts Channel's event handling to the WSEventHandler interface.
type wsEventAdapter struct {
	ch *Channel
}

func (a *wsEventAdapter) HandleEvent(ctx context.Context, payload []byte) error {
	var event MessageEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		slog.Debug("feishu ws: parse event failed", "error", err)
		return fmt.Errorf("parse event: %w", err)
	}
	if event.Header.EventType == "im.message.receive_v1" {
		a.ch.handleMessageEvent(ctx, &event)
	}
	return nil
}

func (c *Channel) startWebSocket(ctx context.Context) error {
	slog.Info("feishu: starting WebSocket connection")

	domain := resolveDomain(c.cfg.Domain)
	c.wsClient = NewWSClient(c.cfg.AppID, c.cfg.AppSecret, domain, &wsEventAdapter{ch: c})

	go func() {
		if err := c.wsClient.Start(ctx); err != nil {
			slog.Error("feishu websocket error", "error", err)
		}
	}()

	slog.Info("feishu WebSocket client started")
	return nil
}

// WebhookHandler returns the webhook HTTP handler and path for mounting on the main gateway mux.
// Returns ("", nil) if not in webhook mode or if webhook_port > 0 (separate server).
func (c *Channel) WebhookHandler() (string, http.Handler) {
	mode := c.cfg.ConnectionMode
	if mode != "webhook" {
		return "", nil
	}
	// Only mount on main mux when webhook_port is 0 (share main server port).
	if c.cfg.WebhookPort > 0 {
		return "", nil
	}

	path := c.cfg.WebhookPath
	if path == "" {
		path = defaultWebhookPath
	}

	handler := NewWebhookHandler(c.cfg.VerificationToken, c.cfg.EncryptKey, func(event *MessageEvent) {
		ctx := store.WithTenantID(context.Background(), c.TenantID())
		c.handleMessageEvent(ctx, event)
	})

	return path, http.HandlerFunc(handler)
}

func (c *Channel) startWebhook(ctx context.Context) error {
	// If webhook_port is 0, the handler is mounted on the main gateway mux
	// via WebhookHandler() — no separate server needed.
	if c.cfg.WebhookPort <= 0 {
		slog.Info("feishu: webhook handler mounted on main gateway mux", "path", c.webhookPath())
		return nil
	}

	port := c.cfg.WebhookPort
	path := c.webhookPath()

	slog.Info("feishu: starting Webhook server", "port", port, "path", path)

	handler := NewWebhookHandler(c.cfg.VerificationToken, c.cfg.EncryptKey, func(event *MessageEvent) {
		ctx := store.WithTenantID(context.Background(), c.TenantID())
		c.handleMessageEvent(ctx, event)
	})

	mux := http.NewServeMux()
	mux.HandleFunc(path, handler)

	c.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		if err := c.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("feishu webhook server error", "error", err)
		}
	}()

	slog.Info("feishu Webhook server listening", "port", port)
	return nil
}

// --- Bot probe ---

func (c *Channel) probeBotInfo(ctx context.Context) error {
	openID, err := c.client.GetBotInfo(ctx)
	if err != nil {
		return fmt.Errorf("fetch bot info: %w", err)
	}
	if openID == "" {
		return fmt.Errorf("bot open_id is empty")
	}
	c.botOpenID = openID
	return nil
}

// --- Send helpers ---

func (c *Channel) sendChunkedText(ctx context.Context, chatID, receiveIDType, text string, chunkLimit int) error {
	for len(text) > 0 {
		chunk := text
		if len(chunk) > chunkLimit {
			cutAt := chunkLimit
			if idx := strings.LastIndex(text[:chunkLimit], "\n"); idx > chunkLimit/2 {
				cutAt = idx + 1
			}
			chunk = text[:cutAt]
			text = text[cutAt:]
		} else {
			text = ""
		}

		if err := c.sendText(ctx, chatID, receiveIDType, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (c *Channel) sendText(ctx context.Context, chatID, receiveIDType, text string) error {
	content := buildPostContent(text)

	_, err := c.client.SendMessage(ctx, receiveIDType, chatID, "post", content)
	if err != nil {
		return fmt.Errorf("feishu send text: %w", err)
	}
	return nil
}

func (c *Channel) sendMarkdownCard(ctx context.Context, chatID, receiveIDType, text string, metadata map[string]string) error {
	card := buildMarkdownCard(text)
	cardJSON, err := json.Marshal(card)
	if err != nil {
		return fmt.Errorf("marshal card: %w", err)
	}

	_, err = c.client.SendMessage(ctx, receiveIDType, chatID, "interactive", string(cardJSON))
	if err != nil {
		return fmt.Errorf("feishu send card: %w", err)
	}
	return nil
}

// webhookPath returns the configured webhook path or the default.
func (c *Channel) webhookPath() string {
	if c.cfg.WebhookPath != "" {
		return c.cfg.WebhookPath
	}
	return defaultWebhookPath
}

// --- Domain resolution ---

func resolveDomain(domain string) string {
	switch domain {
	case "feishu":
		return "https://open.feishu.cn"
	case "", "lark":
		return "https://open.larksuite.com"
	default:
		if !strings.HasPrefix(domain, "http") {
			return "https://" + domain
		}
		return domain
	}
}

func resolveReceiveIDType(id string) string {
	if strings.HasPrefix(id, "oc_") {
		return "chat_id"
	}
	if strings.HasPrefix(id, "ou_") {
		return "open_id"
	}
	if strings.HasPrefix(id, "on_") {
		return "union_id"
	}
	return "chat_id"
}

// --- Content builders ---

// mentionRe matches @ou_xxx patterns (Lark open_id) for outbound mention conversion.
var mentionRe = regexp.MustCompile(`@(ou_[a-zA-Z0-9_]+)`)

// hasMentions checks if text contains @ou_xxx patterns.
func hasMentions(text string) bool {
	return mentionRe.MatchString(text)
}

// buildPostContent creates a Lark "post" message body.
// If the text contains @ou_xxx patterns, they are converted to native "at" elements
// so Lark renders real @mentions with notifications.
func buildPostContent(text string) string {
	var elements []map[string]any

	if hasMentions(text) {
		// Split text around @ou_xxx patterns → alternating md + at elements.
		matches := mentionRe.FindAllStringIndex(text, -1)
		prev := 0
		for _, loc := range matches {
			// Text before the mention
			if loc[0] > prev {
				elements = append(elements, map[string]any{
					"tag":  "md",
					"text": text[prev:loc[0]],
				})
			}
			// The mention itself: extract ou_xxx from "@ou_xxx"
			userID := text[loc[0]+1 : loc[1]] // skip "@"
			elements = append(elements, map[string]any{
				"tag":     "at",
				"user_id": userID,
			})
			prev = loc[1]
		}
		// Remaining text after last mention
		if prev < len(text) {
			elements = append(elements, map[string]any{
				"tag":  "md",
				"text": text[prev:],
			})
		}
	} else {
		elements = []map[string]any{{"tag": "md", "text": text}}
	}

	content := map[string]any{
		"zh_cn": map[string]any{
			"content": [][]map[string]any{elements},
		},
	}
	data, _ := json.Marshal(content)
	return string(data)
}

// convertMentionsForCard replaces @ou_xxx in text with Lark card markdown mention tags.
// e.g. "@ou_abc123" → "<at id=ou_abc123></at>"
// This syntax works in interactive card markdown content.
func convertMentionsForCard(text string) string {
	return mentionRe.ReplaceAllString(text, `<at id=$1></at>`)
}

func buildMarkdownCard(text string) map[string]any {
	return map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"wide_screen_mode": true,
		},
		"body": map[string]any{
			"elements": []map[string]any{
				{
					"tag":     "markdown",
					"content": convertMentionsForCard(text),
				},
			},
		},
	}
}

// shouldUseCard detects if content benefits from card rendering (code blocks, tables).
func shouldUseCard(text string) bool {
	return strings.Contains(text, "```") ||
		strings.Contains(text, "| --- ") ||
		strings.Contains(text, "|---|")
}

// isDuplicate returns true if messageID was already processed.
func (c *Channel) isDuplicate(messageID string) bool {
	_, loaded := c.dedup.LoadOrStore(messageID, struct{}{})
	if !loaded {
		time.AfterFunc(5*time.Minute, func() {
			c.dedup.Delete(messageID)
		})
	}
	return loaded
}

// --- ReactionChannel implementation ---

const typingEmoji = "Typing" // Lark emoji type for typing indicator (matching TS)

// OnReactionEvent handles agent status change events by adding/removing a typing reaction
// on the user's original message. messageID is the Lark message ID (e.g. "om_xxx").
func (c *Channel) OnReactionEvent(ctx context.Context, chatID string, messageID string, status string) error {
	if c.cfg.ReactionLevel == "off" || messageID == "" {
		return nil
	}

	// Minimal mode: only act on terminal states.
	if c.cfg.ReactionLevel == "minimal" && status != "done" && status != "error" {
		return nil
	}

	// Terminal states: remove typing reaction.
	if status == "done" || status == "error" {
		return c.removeTypingReaction(ctx, chatID)
	}

	// Active states (thinking, tool): add typing reaction if not already present.
	if _, loaded := c.reactions.Load(chatID); loaded {
		return nil // already has a reaction
	}

	reactionID, err := c.client.AddMessageReaction(ctx, messageID, typingEmoji)
	if err != nil {
		slog.Debug("feishu: add typing reaction failed", "message_id", messageID, "error", err)
		return nil // non-critical, don't fail the run
	}

	c.reactions.Store(chatID, &reactionState{
		messageID:  messageID,
		reactionID: reactionID,
	})
	return nil
}

// ClearReaction removes the typing reaction from a message.
func (c *Channel) ClearReaction(ctx context.Context, chatID string, _ string) error {
	return c.removeTypingReaction(ctx, chatID)
}

// removeTypingReaction removes the stored typing reaction for a chatID.
func (c *Channel) removeTypingReaction(ctx context.Context, chatID string) error {
	val, ok := c.reactions.LoadAndDelete(chatID)
	if !ok {
		return nil
	}
	rs := val.(*reactionState)
	if rs.reactionID == "" {
		return nil
	}
	if err := c.client.DeleteMessageReaction(ctx, rs.messageID, rs.reactionID); err != nil {
		slog.Debug("feishu: remove typing reaction failed", "message_id", rs.messageID, "error", err)
	}
	return nil
}

// ListGroupMembers returns all members of a Lark group chat.
// Also syncs discovered members into the contact store (if available).
func (c *Channel) ListGroupMembers(ctx context.Context, chatID string) ([]channels.GroupMember, error) {
	members, err := c.client.ListChatMembers(ctx, chatID)
	if err != nil {
		slog.Warn("feishu.list_group_members", "chat_id", chatID, "error", err)
		return nil, err
	}
	result := make([]channels.GroupMember, len(members))
	for i, m := range members {
		result[i] = channels.GroupMember{
			MemberID: m.MemberID,
			Name:     m.Name,
		}
		// Auto-sync member into contact store
		if cc := c.ContactCollector(); cc != nil {
			cc.EnsureContact(ctx, channels.TypeFeishu, c.Name(), m.MemberID, m.MemberID, m.Name, "", "group")
		}
	}
	return result, nil
}

// Ensure Channel implements the channels.Channel, WebhookChannel, ReactionChannel, and GroupMemberProvider interfaces at compile time.
var _ channels.Channel = (*Channel)(nil)
var _ channels.WebhookChannel = (*Channel)(nil)
var _ channels.ReactionChannel = (*Channel)(nil)
var _ channels.GroupMemberProvider = (*Channel)(nil)
