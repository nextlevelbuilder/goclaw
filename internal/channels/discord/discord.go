package discord

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"

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
	placeholders    sync.Map // placeholderKey string → messageID string
	typingCtrls     sync.Map // channelID string → *typing.Controller
	agentStore      store.AgentStore            // for agent key lookup (nil = writer commands disabled)
	configPermStore store.ConfigPermissionStore // for group file writer management (nil = writer commands disabled)
	// pairingService, pairingDebounce, approvedGroups, groupHistory, historyLimit, requireMention
	// are inherited from channels.BaseChannel.
}

// New creates a new Discord channel from config.
// agentStore and configPermStore are optional (nil = writer commands disabled).
func New(cfg config.DiscordConfig, msgBus *bus.MessageBus, pairingSvc store.PairingStore,
	agentStore store.AgentStore, configPermStore store.ConfigPermissionStore,
	pendingStore store.PendingMessageStore) (*Channel, error) {
	session, err := discordgo.New("Bot " + cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("create discord session: %w", err)
	}

	// Request necessary intents
	session.Identify.Intents = discordgo.IntentsGuildMessages |
		discordgo.IntentsDirectMessages |
		discordgo.IntentsMessageContent

	base := channels.NewBaseChannel(channels.TypeDiscord, msgBus, cfg.AllowFrom)
	base.ValidatePolicy(cfg.DMPolicy, cfg.GroupPolicy)

	requireMention := true
	if cfg.RequireMention != nil {
		requireMention = *cfg.RequireMention
	}

	historyLimit := cfg.HistoryLimit
	if historyLimit == 0 {
		historyLimit = channels.DefaultGroupHistoryLimit
	}

	ch := &Channel{
		BaseChannel:     base,
		session:         session,
		config:          cfg,
		agentStore:      agentStore,
		configPermStore: configPermStore,
	}
	ch.SetRequireMention(requireMention)
	ch.SetPairingService(pairingSvc)
	ch.SetGroupHistory(channels.MakeHistory(channels.TypeDiscord, pendingStore, base.TenantID()))
	ch.SetHistoryLimit(historyLimit)
	return ch, nil
}

// Start opens the Discord gateway connection and begins receiving events.
func (c *Channel) Start(_ context.Context) error {
	c.GroupHistory().StartFlusher()
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

// Stop closes the Discord gateway connection.
func (c *Channel) Stop(_ context.Context) error {
	c.GroupHistory().StopFlusher()
	slog.Info("stopping discord bot")
	c.SetRunning(false)
	return c.session.Close()
}

// Send delivers an outbound message to a Discord channel.
func (c *Channel) Send(_ context.Context, msg bus.OutboundMessage) (err error) {
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

	typingCtrl := c.currentTypingCtrl(channelID)
	defer func() {
		c.finishTyping(channelID, typingCtrl, err)
	}()

	content := msg.Content

	// Handle outbound media attachments: send files via Discord's file upload API.
	if len(msg.Media) > 0 {
		// Delete placeholder if present
		if pID, ok := c.placeholders.Load(placeholderKey); ok {
			c.placeholders.Delete(placeholderKey)
			_ = c.session.ChannelMessageDelete(channelID, pID.(string))
		}
		return c.sendMediaMessage(channelID, content, msg.Media)
	}

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
// Uses markdown-aware chunking to avoid splitting inside fenced code blocks.
func (c *Channel) sendChunked(channelID, content string) error {
	const maxLen = 2000

	for _, chunk := range channels.ChunkMarkdown(content, maxLen) {
		if _, err := c.session.ChannelMessageSend(channelID, chunk); err != nil {
			return fmt.Errorf("send discord message: %w", err)
		}
	}

	return nil
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

func (c *Channel) currentTypingCtrl(channelID string) *typing.Controller {
	ctrl, ok := c.typingCtrls.Load(channelID)
	if !ok {
		return nil
	}

	typed, ok := ctrl.(*typing.Controller)
	if !ok {
		c.typingCtrls.Delete(channelID)
		return nil
	}

	return typed
}

func (c *Channel) finishTyping(channelID string, expected *typing.Controller, sendErr error) {
	if expected == nil {
		return
	}
	if sendErr != nil {
		slog.Warn("discord: outbound send failed; keeping typing indicator active until TTL",
			"channel_id", channelID, "error", sendErr)
		return
	}

	current, ok := c.typingCtrls.Load(channelID)
	if !ok {
		return
	}

	typed, ok := current.(*typing.Controller)
	if !ok {
		c.typingCtrls.Delete(channelID)
		return
	}
	if typed != expected {
		return
	}

	c.typingCtrls.Delete(channelID)
	typed.Stop()
}
