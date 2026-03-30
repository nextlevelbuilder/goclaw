package whatsapp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const pairingDebounceTime = 60 * time.Second

// Channel connects to a WhatsApp bridge via WebSocket.
// The bridge (e.g. whatsapp-web.js based) handles the actual WhatsApp
// protocol; this channel just sends/receives JSON messages over WS.
type Channel struct {
	*channels.BaseChannel
	conn            *websocket.Conn
	config          config.WhatsAppConfig
	mu              sync.Mutex
	connected       bool
	ctx             context.Context
	cancel          context.CancelFunc
	pairingService  store.PairingStore
	pairingDebounce sync.Map // senderID → time.Time
	approvedGroups  sync.Map // chatID → true (in-memory cache for paired groups)
}

// New creates a new WhatsApp channel from config.
func New(cfg config.WhatsAppConfig, msgBus *bus.MessageBus, pairingSvc store.PairingStore) (*Channel, error) {
	if cfg.BridgeURL == "" {
		return nil, fmt.Errorf("whatsapp bridge_url is required")
	}

	base := channels.NewBaseChannel(channels.TypeWhatsApp, msgBus, cfg.AllowFrom)
	base.ValidatePolicy(cfg.DMPolicy, cfg.GroupPolicy)

	return &Channel{
		BaseChannel:    base,
		config:         cfg,
		pairingService: pairingSvc,
	}, nil
}

// Start connects to the WhatsApp bridge WebSocket and begins listening.
func (c *Channel) Start(ctx context.Context) error {
	slog.Info("starting whatsapp channel", "bridge_url", c.config.BridgeURL)

	c.ctx, c.cancel = context.WithCancel(ctx)

	if err := c.connect(); err != nil {
		// Don't fail hard — reconnect loop will keep trying
		slog.Warn("initial whatsapp bridge connection failed, will retry", "error", err)
	}

	go c.listenLoop()

	c.SetRunning(true)
	return nil
}

// BlockReplyEnabled returns the per-channel block_reply override (nil = inherit gateway default).
func (c *Channel) BlockReplyEnabled() *bool { return c.config.BlockReply }

// Stop gracefully shuts down the WhatsApp channel.
func (c *Channel) Stop(_ context.Context) error {
	slog.Info("stopping whatsapp channel")

	if c.cancel != nil {
		c.cancel()
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
	c.connected = false
	c.SetRunning(false)

	return nil
}

// Send delivers an outbound message to the WhatsApp bridge.
func (c *Channel) Send(_ context.Context, msg bus.OutboundMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("whatsapp bridge not connected")
	}

	payload := map[string]any{
		"type":    "message",
		"to":      msg.ChatID,
		"content": msg.Content,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal whatsapp message: %w", err)
	}

	if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("send whatsapp message: %w", err)
	}

	return nil
}

// connect establishes the WebSocket connection to the bridge.
func (c *Channel) connect() error {
	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second

	conn, _, err := dialer.Dial(c.config.BridgeURL, nil)
	if err != nil {
		return fmt.Errorf("dial whatsapp bridge %s: %w", c.config.BridgeURL, err)
	}

	c.mu.Lock()
	c.conn = conn
	c.connected = true
	c.mu.Unlock()

	slog.Info("whatsapp bridge connected", "url", c.config.BridgeURL)
	return nil
}

// listenLoop reads messages from the bridge with automatic reconnection.
func (c *Channel) listenLoop() {
	backoff := time.Second

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()

		if conn == nil {
			// Not connected — attempt reconnect with backoff
			slog.Info("attempting whatsapp bridge reconnect", "backoff", backoff)

			select {
			case <-c.ctx.Done():
				return
			case <-time.After(backoff):
			}

			if err := c.connect(); err != nil {
				slog.Warn("whatsapp bridge reconnect failed", "error", err)
				backoff = min(backoff*2, 30*time.Second)
				continue
			}

			backoff = time.Second // reset on success
			continue
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			slog.Warn("whatsapp read error, will reconnect", "error", err)

			c.mu.Lock()
			if c.conn != nil {
				_ = c.conn.Close()
				c.conn = nil
			}
			c.connected = false
			c.mu.Unlock()

			continue
		}

		var msg map[string]any
		if err := json.Unmarshal(message, &msg); err != nil {
			slog.Warn("invalid whatsapp message JSON", "error", err)
			continue
		}

		msgType, _ := msg["type"].(string)
		if msgType == "message" {
			c.handleIncomingMessage(msg)
		}
	}
}

// handleIncomingMessage processes a message received from the bridge.
// Expected format: {"type":"message","from":"...","chat":"...","content":"...","id":"...","from_name":"...","media":[...]}
func (c *Channel) handleIncomingMessage(msg map[string]any) {
	ctx := context.Background()
	ctx = store.WithTenantID(ctx, c.TenantID())
	senderID, ok := msg["from"].(string)
	if !ok || senderID == "" {
		return
	}

	chatID, _ := msg["chat"].(string)
	if chatID == "" {
		chatID = senderID
	}

	// WhatsApp groups have chatID ending in "@g.us"
	peerKind := "direct"
	if strings.HasSuffix(chatID, "@g.us") {
		peerKind = "group"
	}

	// DM/Group policy check
	if peerKind == "direct" {
		if !c.checkDMPolicy(ctx, senderID, chatID) {
			return
		}
	} else {
		if !c.checkGroupPolicy(ctx, senderID, chatID) {
			slog.Debug("whatsapp group message rejected by policy", "sender_id", senderID)
			return
		}
	}

	// Allowlist check
	if !c.IsAllowed(senderID) {
		slog.Debug("whatsapp message rejected by allowlist", "sender_id", senderID)
		return
	}

	content, _ := msg["content"].(string)
	if content == "" {
		content = "[empty message]"
	}

	var media []string
	if mediaData, ok := msg["media"].([]any); ok {
		media = make([]string, 0, len(mediaData))
		for _, m := range mediaData {
			if path, ok := m.(string); ok {
				media = append(media, path)
			}
		}
	}

	metadata := make(map[string]string)
	if messageID, ok := msg["id"].(string); ok {
		metadata["message_id"] = messageID
	}
	if userName, ok := msg["from_name"].(string); ok {
		metadata["user_name"] = userName
	}

	slog.Debug("whatsapp message received",
		"sender_id", senderID,
		"chat_id", chatID,
		"preview", channels.Truncate(content, 50),
	)

	// Collect contact for processed messages.
	if cc := c.ContactCollector(); cc != nil {
		cc.EnsureContact(ctx, c.Type(), c.Name(), senderID, senderID, metadata["user_name"], "", peerKind, "user")
	}

	// Annotate with sender identity so the agent knows who is messaging.
	if senderName := metadata["user_name"]; senderName != "" {
		content = fmt.Sprintf("[From: %s]\n%s", senderName, content)
	}

	c.HandleMessage(senderID, chatID, content, media, metadata, peerKind)
}

// checkGroupPolicy evaluates the group policy for a sender, with pairing support.
func (c *Channel) checkGroupPolicy(ctx context.Context, senderID, chatID string) bool {
	groupPolicy := c.config.GroupPolicy
	if groupPolicy == "" {
		groupPolicy = "open"
	}

	switch groupPolicy {
	case "disabled":
		return false
	case "allowlist":
		return c.IsAllowed(senderID)
	case "pairing":
		if c.IsAllowed(senderID) {
			return true
		}
		if _, cached := c.approvedGroups.Load(chatID); cached {
			return true
		}
		groupSenderID := fmt.Sprintf("group:%s", chatID)
		if c.pairingService != nil {
			paired, err := c.pairingService.IsPaired(ctx, groupSenderID, c.Name())
			if err != nil {
				slog.Warn("security.pairing_check_failed, assuming paired (fail-open)",
					"group_sender", groupSenderID, "channel", c.Name(), "error", err)
				paired = true
			}
			if paired {
				c.approvedGroups.Store(chatID, true)
				return true
			}
		}
		c.sendPairingReply(ctx, groupSenderID, chatID)
		return false
	default: // "open"
		return true
	}
}

// checkDMPolicy evaluates the DM policy for a sender, handling pairing flow.
func (c *Channel) checkDMPolicy(ctx context.Context, senderID, chatID string) bool {
	dmPolicy := c.config.DMPolicy
	if dmPolicy == "" {
		dmPolicy = "pairing"
	}

	switch dmPolicy {
	case "disabled":
		slog.Debug("whatsapp DM rejected: disabled", "sender_id", senderID)
		return false
	case "open":
		return true
	case "allowlist":
		if !c.IsAllowed(senderID) {
			slog.Debug("whatsapp DM rejected by allowlist", "sender_id", senderID)
			return false
		}
		return true
	default: // "pairing"
		paired := false
		if c.pairingService != nil {
			p, err := c.pairingService.IsPaired(ctx, senderID, c.Name())
			if err != nil {
				slog.Warn("security.pairing_check_failed, assuming paired (fail-open)",
					"sender_id", senderID, "channel", c.Name(), "error", err)
				paired = true
			} else {
				paired = p
			}
		}
		inAllowList := c.HasAllowList() && c.IsAllowed(senderID)

		if paired || inAllowList {
			return true
		}

		c.sendPairingReply(ctx, senderID, chatID)
		return false
	}
}

// sendPairingReply sends a pairing code to the user via the WS bridge.
func (c *Channel) sendPairingReply(ctx context.Context, senderID, chatID string) {
	if c.pairingService == nil {
		return
	}

	// Debounce
	if lastSent, ok := c.pairingDebounce.Load(senderID); ok {
		if time.Since(lastSent.(time.Time)) < pairingDebounceTime {
			return
		}
	}

	code, err := c.pairingService.RequestPairing(ctx, senderID, c.Name(), chatID, "default", nil)
	if err != nil {
		slog.Debug("whatsapp pairing request failed", "sender_id", senderID, "error", err)
		return
	}

	replyText := fmt.Sprintf(
		"GoClaw: access not configured.\n\nYour WhatsApp ID: %s\n\nPairing code: %s\n\nAsk the bot owner to approve with:\n  goclaw pairing approve %s",
		senderID, code, code,
	)

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		slog.Warn("whatsapp bridge not connected, cannot send pairing reply")
		return
	}

	payload, _ := json.Marshal(map[string]any{
		"type":    "message",
		"to":      chatID,
		"content": replyText,
	})

	if err := c.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		slog.Warn("failed to send whatsapp pairing reply", "error", err)
	} else {
		c.pairingDebounce.Store(senderID, time.Now())
		slog.Info("whatsapp pairing reply sent", "sender_id", senderID, "code", code)
	}
}
