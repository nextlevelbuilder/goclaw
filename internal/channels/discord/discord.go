package discord

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/typing"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const pairingDebounceTime = 60 * time.Second

// Channel connects to Discord via the Bot API using gateway events.
type Channel struct {
	*channels.BaseChannel
	session         *discordgo.Session
	config          config.DiscordConfig
	botUserID       string   // populated on start
	requireMention  bool     // require @bot mention in groups (default true)
	placeholders    sync.Map // placeholderKey string → messageID string
	typingCtrls     sync.Map // channelID string → *typing.Controller
	pairingService  store.PairingStore
	pairingDebounce sync.Map // senderID → time.Time
	approvedGroups  sync.Map // chatID → true (in-memory cache for paired groups)
	groupHistory    *channels.PendingHistory
	historyLimit    int
}

// New creates a new Discord channel from config.
func New(cfg config.DiscordConfig, msgBus *bus.MessageBus, pairingSvc store.PairingStore) (*Channel, error) {
	session, err := discordgo.New("Bot " + cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("create discord session: %w", err)
	}

	// Request necessary intents
	session.Identify.Intents = discordgo.IntentsGuildMessages |
		discordgo.IntentsDirectMessages |
		discordgo.IntentsMessageContent

	base := channels.NewBaseChannel("discord", msgBus, cfg.AllowFrom)
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
		BaseChannel:    base,
		session:        session,
		config:         cfg,
		requireMention: requireMention,
		pairingService: pairingSvc,
		groupHistory:   channels.NewPendingHistory(),
		historyLimit:   historyLimit,
	}, nil
}

// Start opens the Discord gateway connection and begins receiving events.
func (c *Channel) Start(_ context.Context) error {
	slog.Info("starting discord bot")

	c.session.AddHandler(c.handleMessage)

	if err := c.session.Open(); err != nil {
		return fmt.Errorf("open discord session: %w", err)
	}

	// Fetch bot identity
	user, err := c.session.User("@me")
	if err != nil {
		c.session.Close()
		return fmt.Errorf("fetch discord bot identity: %w", err)
	}
	c.botUserID = user.ID

	c.SetRunning(true)
	slog.Info("discord bot connected", "username", user.Username, "id", user.ID)

	return nil
}

// Stop closes the Discord gateway connection.
func (c *Channel) Stop(_ context.Context) error {
	slog.Info("stopping discord bot")
	c.SetRunning(false)
	return c.session.Close()
}

// Send delivers an outbound message to a Discord channel.
func (c *Channel) Send(_ context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return fmt.Errorf("discord bot not running")
	}

	channelID := msg.ChatID
	if channelID == "" {
		return fmt.Errorf("empty chat ID for discord send")
	}

	// Resolve placeholder key from metadata (inbound message ID), fall back to channelID.
	// Keying by message ID prevents race conditions when multiple messages
	// arrive in the same channel before the first response is sent.
	placeholderKey := channelID
	if pk := msg.Metadata["placeholder_key"]; pk != "" {
		placeholderKey = pk
	}

	// Placeholder update (e.g. LLM retry notification): edit the placeholder
	// but keep it alive for the final response. Don't stop typing or cleanup.
	if msg.Metadata["placeholder_update"] == "true" {
		if pID, ok := c.placeholders.Load(placeholderKey); ok {
			msgID := pID.(string)
			_, _ = c.session.ChannelMessageEdit(channelID, msgID, msg.Content)
		}
		return nil
	}

	// Stop typing indicator controller
	if ctrl, ok := c.typingCtrls.LoadAndDelete(channelID); ok {
		ctrl.(*typing.Controller).Stop()
	}

	content := msg.Content

	// NO_REPLY cleanup: content is empty when agent suppresses reply.
	// Delete placeholder and return without sending any message.
	if content == "" {
		if pID, ok := c.placeholders.Load(placeholderKey); ok {
			c.placeholders.Delete(placeholderKey)
			msgID := pID.(string)
			_ = c.session.ChannelMessageDelete(channelID, msgID)
		}
		return nil
	}

	// Try to edit the placeholder "Thinking..." message with the first chunk,
	// then send the rest as follow-up messages.
	if pID, ok := c.placeholders.Load(placeholderKey); ok {
		c.placeholders.Delete(placeholderKey)
		msgID := pID.(string)

		const maxLen = 2000
		editContent := content
		remaining := ""

		if len(editContent) > maxLen {
			// Break at a newline if possible
			cutAt := maxLen
			if idx := lastIndexByte(content[:maxLen], '\n'); idx > maxLen/2 {
				cutAt = idx + 1
			}
			editContent = content[:cutAt]
			remaining = content[cutAt:]
		}

		if _, editErr := c.session.ChannelMessageEdit(channelID, msgID, editContent); editErr == nil {
			// Send remaining content as follow-up messages
			if remaining != "" {
				return c.sendChunked(channelID, remaining)
			}
			return nil
		} else {
			slog.Warn("discord: placeholder edit failed, sending new message",
				"channel_id", channelID, "placeholder_id", msgID, "error", editErr)
		}
		// Fall through to send new message if edit fails
	}

	// Send as new message(s), chunking if needed
	return c.sendChunked(channelID, content)
}

// sendChunked sends a message, splitting into multiple messages if over 2000 chars.
func (c *Channel) sendChunked(channelID, content string) error {
	const maxLen = 2000

	for len(content) > 0 {
		chunk := content
		if len(chunk) > maxLen {
			// Try to break at a newline
			cutAt := maxLen
			if idx := lastIndexByte(content[:maxLen], '\n'); idx > maxLen/2 {
				cutAt = idx + 1
			}
			chunk = content[:cutAt]
			content = content[cutAt:]
		} else {
			content = ""
		}

		if _, err := c.session.ChannelMessageSend(channelID, chunk); err != nil {
			return fmt.Errorf("send discord message: %w", err)
		}
	}

	return nil
}

// handleMessage processes incoming Discord messages.
func (c *Channel) handleMessage(_ *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore bot's own messages
	if m.Author == nil || m.Author.ID == c.botUserID {
		return
	}

	// Ignore bot messages
	if m.Author.Bot {
		return
	}

	senderID := m.Author.ID
	senderName := resolveDisplayName(m)

	channelID := m.ChannelID
	isDM := m.GuildID == ""

	// DM/Group policy check
	peerKind := "group"
	if isDM {
		peerKind = "direct"
	}

	if isDM {
		if !c.checkDMPolicy(senderID, channelID) {
			return
		}
	} else {
		if !c.checkGroupPolicy(senderID, channelID) {
			slog.Debug("discord group message rejected by policy",
				"user_id", senderID,
				"username", senderName,
			)
			return
		}
	}

	// Check allowlist (for "open" policy, still apply allowlist if configured)
	if !c.IsAllowed(senderID) {
		slog.Debug("discord message rejected by allowlist",
			"user_id", senderID,
			"username", senderName,
		)
		return
	}

	// Build content
	content := m.Content

	// Append attachment URLs
	for _, att := range m.Attachments {
		if content != "" {
			content += "\n"
		}
		content += fmt.Sprintf("[attachment: %s]", att.URL)
	}

	if content == "" {
		content = "[empty message]"
	}

	// Mention gating: in groups, only respond when bot is @mentioned (default true).
	// When not mentioned, record message to pending history for later context.
	if peerKind == "group" && c.requireMention {
		mentioned := false
		for _, u := range m.Mentions {
			if u.ID == c.botUserID {
				mentioned = true
				break
			}
		}
		if !mentioned {
			c.groupHistory.Record(channelID, channels.HistoryEntry{
				Sender:    senderName,
				Body:      content,
				Timestamp: m.Timestamp,
				MessageID: m.ID,
			}, c.historyLimit)

			slog.Debug("discord group message recorded (no mention)",
				"channel_id", channelID,
				"user_id", senderID,
				"username", senderName,
			)
			return
		}
	}

	slog.Debug("discord message received",
		"sender_id", senderID,
		"channel_id", channelID,
		"is_dm", isDM,
		"preview", channels.Truncate(content, 50),
	)

	// Send typing indicator with keepalive + TTL safety net.
	// Discord typing expires after 10s, so keepalive every 9s.
	// TTL auto-stops after 60s to prevent stuck indicators.
	typingCtrl := typing.New(typing.Options{
		MaxDuration:       60 * time.Second,
		KeepaliveInterval: 9 * time.Second,
		StartFn: func() error {
			return c.session.ChannelTyping(channelID)
		},
	})
	// Stop previous typing controller for this channel (if any)
	if prev, ok := c.typingCtrls.Load(channelID); ok {
		prev.(*typing.Controller).Stop()
	}
	c.typingCtrls.Store(channelID, typingCtrl)
	typingCtrl.Start()

	// Send placeholder "Thinking..." message.
	// Key by inbound message ID (not channel ID) to avoid race conditions
	// when multiple messages arrive in the same channel concurrently.
	placeholder, err := c.session.ChannelMessageSend(channelID, "Thinking...")
	if err == nil {
		c.placeholders.Store(m.ID, placeholder.ID)
	}

	// Strip bot @mention from content — it's just the trigger, not meaningful.
	content = strings.ReplaceAll(content, "<@"+c.botUserID+">", "")
	content = strings.TrimSpace(content)

	// Build final content with group context.
	finalContent := content
	if peerKind == "group" {
		annotated := fmt.Sprintf("[From: %s (<@%s>)]\n%s", senderName, senderID, content)
		if c.historyLimit > 0 {
			finalContent = c.groupHistory.BuildContext(channelID, annotated, c.historyLimit)
		} else {
			finalContent = annotated
		}
	}

	metadata := map[string]string{
		"message_id":      m.ID,
		"user_id":         senderID,
		"username":        m.Author.Username,
		"display_name":    senderName,
		"guild_id":        m.GuildID,
		"channel_id":      channelID,
		"is_dm":           fmt.Sprintf("%t", isDM),
		"placeholder_key": m.ID, // keyed by inbound message ID for placeholder lookup
	}

	c.HandleMessage(senderID, channelID, finalContent, nil, metadata, peerKind)

	// Clear pending history after sending to agent.
	if peerKind == "group" {
		c.groupHistory.Clear(channelID)
	}
}

// checkGroupPolicy evaluates the group policy for a sender, with pairing support.
func (c *Channel) checkGroupPolicy(senderID, channelID string) bool {
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
		if _, cached := c.approvedGroups.Load(channelID); cached {
			return true
		}
		groupSenderID := fmt.Sprintf("group:%s", channelID)
		if c.pairingService != nil && c.pairingService.IsPaired(groupSenderID, c.Name()) {
			c.approvedGroups.Store(channelID, true)
			return true
		}
		c.sendPairingReply(groupSenderID, channelID)
		return false
	default: // "open"
		return true
	}
}

// checkDMPolicy evaluates the DM policy for a sender, handling pairing flow.
func (c *Channel) checkDMPolicy(senderID, channelID string) bool {
	dmPolicy := c.config.DMPolicy
	if dmPolicy == "" {
		dmPolicy = "pairing"
	}

	switch dmPolicy {
	case "disabled":
		slog.Debug("discord DM rejected: disabled", "sender_id", senderID)
		return false
	case "open":
		return true
	case "allowlist":
		if !c.IsAllowed(senderID) {
			slog.Debug("discord DM rejected by allowlist", "sender_id", senderID)
			return false
		}
		return true
	default: // "pairing"
		paired := false
		if c.pairingService != nil {
			paired = c.pairingService.IsPaired(senderID, c.Name())
		}
		inAllowList := c.HasAllowList() && c.IsAllowed(senderID)

		if paired || inAllowList {
			return true
		}

		c.sendPairingReply(senderID, channelID)
		return false
	}
}

// sendPairingReply sends a pairing code to the user via DM.
func (c *Channel) sendPairingReply(senderID, channelID string) {
	if c.pairingService == nil {
		return
	}

	// Debounce
	if lastSent, ok := c.pairingDebounce.Load(senderID); ok {
		if time.Since(lastSent.(time.Time)) < pairingDebounceTime {
			return
		}
	}

	code, err := c.pairingService.RequestPairing(senderID, c.Name(), channelID, "default")
	if err != nil {
		slog.Debug("discord pairing request failed", "sender_id", senderID, "error", err)
		return
	}

	replyText := fmt.Sprintf(
		"GoClaw: access not configured.\n\nYour Discord user ID: %s\n\nPairing code: %s\n\nAsk the bot owner to approve with:\n  goclaw pairing approve %s",
		senderID, code, code,
	)

	if _, err := c.session.ChannelMessageSend(channelID, replyText); err != nil {
		slog.Warn("failed to send discord pairing reply", "error", err)
	} else {
		c.pairingDebounce.Store(senderID, time.Now())
		slog.Info("discord pairing reply sent", "sender_id", senderID, "code", code)
	}
}

// resolveDisplayName returns the best available display name for a Discord message author.
// Priority: server nickname > global display name > username.
func resolveDisplayName(m *discordgo.MessageCreate) string {
	if m.Member != nil && m.Member.Nick != "" {
		return m.Member.Nick
	}
	if m.Author.GlobalName != "" {
		return m.Author.GlobalName
	}
	return m.Author.Username
}

// lastIndexByte returns the last index of byte c in s, or -1.
func lastIndexByte(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}
