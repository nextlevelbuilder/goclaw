package googlechat

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	defaultWebhookPath = "/googlechat/events"
	typingEmoji        = "\u23f3" // Unicode hourglass for thinking indicator
)

// Channel connects to Google Chat via webhook.
type Channel struct {
	*channels.BaseChannel
	cfg            config.GoogleChatConfig
	client         *ChatClient
	pairingService store.PairingStore
	dedup          sync.Map // message name → struct{}
	reactions      sync.Map // chatID → *reactionState
	groupHistory   *channels.PendingHistory
	historyLimit   int
}

type reactionState struct {
	messageName  string // full message resource name
	reactionName string // reaction resource name for deletion
}

// Compile-time interface assertions.
var _ channels.Channel = (*Channel)(nil)
var _ channels.WebhookChannel = (*Channel)(nil)
var _ channels.ReactionChannel = (*Channel)(nil)
var _ channels.BlockReplyChannel = (*Channel)(nil)

// New creates a new Google Chat channel.
func New(cfg config.GoogleChatConfig, msgBus *bus.MessageBus, pairingSvc store.PairingStore) (*Channel, error) {
	saJSON, err := resolveServiceAccountJSON(cfg)
	if err != nil {
		return nil, fmt.Errorf("google_chat credentials: %w", err)
	}

	client, err := NewChatClient(saJSON)
	if err != nil {
		return nil, fmt.Errorf("google_chat api client: %w", err)
	}

	base := channels.NewBaseChannel(channels.TypeGoogleChat, msgBus, cfg.AllowFrom)
	base.ValidatePolicy(cfg.DMPolicy, cfg.GroupPolicy)

	switch cfg.ReactionLevel {
	case "", "off", "minimal", "full":
		// valid
	default:
		slog.Warn("googlechat: unrecognized reaction_level, defaulting to off", "value", cfg.ReactionLevel)
	}

	historyLimit := cfg.HistoryLimit
	if historyLimit == 0 {
		historyLimit = channels.DefaultGroupHistoryLimit
	}

	return &Channel{
		BaseChannel:    base,
		cfg:            cfg,
		client:         client,
		pairingService: pairingSvc,
		groupHistory:   channels.NewPendingHistory(),
		historyLimit:   historyLimit,
	}, nil
}

func (c *Channel) Start(_ context.Context) error {
	slog.Info("starting google chat bot (webhook mode)")
	c.SetRunning(true)
	return nil
}

func (c *Channel) Stop(_ context.Context) error {
	slog.Info("stopping google chat bot")
	c.SetRunning(false)
	return nil
}

// BlockReplyEnabled returns the per-channel block_reply override (nil = inherit).
func (c *Channel) BlockReplyEnabled() *bool { return c.cfg.BlockReply }

// WebhookHandler returns the webhook path and handler for mounting on the main gateway mux.
func (c *Channel) WebhookHandler() (string, http.Handler) {
	path := c.cfg.WebhookPath
	if path == "" {
		path = defaultWebhookPath
	}

	handler := NewWebhookHandler(c.cfg.ProjectNumber, func(event *SpaceEvent) {
		c.handleMessageEvent(context.Background(), event)
	})

	return path, http.HandlerFunc(handler)
}

// Send delivers an outbound message to a Google Chat space.
func (c *Channel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return fmt.Errorf("googlechat bot not running")
	}
	if msg.ChatID == "" {
		return fmt.Errorf("empty chat ID for googlechat send")
	}

	text := msg.Content
	if text == "" {
		return nil
	}

	// Convert markdown and chunk by bytes (UTF-8 safe, limit 3900B per chunk).
	chunks := chunkByBytes(markdownToGoogleChat(text), googleChatMaxMessageBytes)
	for _, chunk := range chunks {
		if err := c.client.SendMessage(ctx, msg.ChatID, chunk); err != nil {
			return fmt.Errorf("googlechat send: %w", err)
		}
	}
	return nil
}

// OnReactionEvent handles agent status changes by adding/removing emoji reactions.
func (c *Channel) OnReactionEvent(ctx context.Context, chatID, messageID, status string) error {
	if c.cfg.ReactionLevel == "off" || messageID == "" {
		return nil
	}
	if c.cfg.ReactionLevel == "minimal" && status != "done" && status != "error" {
		return nil
	}

	// Terminal states: remove reaction
	if status == "done" || status == "error" {
		return c.removeReaction(ctx, chatID)
	}

	// Active states: add reaction if not present
	if _, loaded := c.reactions.Load(chatID); loaded {
		return nil
	}

	reactionName, err := c.client.AddReaction(ctx, messageID, typingEmoji)
	if err != nil {
		slog.Debug("googlechat: add reaction failed", "message", messageID, "error", err)
		return nil
	}

	c.reactions.Store(chatID, &reactionState{
		messageName:  messageID,
		reactionName: reactionName,
	})
	return nil
}

// ClearReaction removes the typing reaction from a message.
func (c *Channel) ClearReaction(ctx context.Context, chatID, _ string) error {
	return c.removeReaction(ctx, chatID)
}

func (c *Channel) removeReaction(ctx context.Context, chatID string) error {
	val, ok := c.reactions.LoadAndDelete(chatID)
	if !ok {
		return nil
	}
	rs, ok := val.(*reactionState)
	if !ok {
		return nil
	}
	if rs.reactionName == "" {
		return nil
	}
	if err := c.client.DeleteReaction(ctx, rs.reactionName); err != nil {
		slog.Debug("googlechat: remove reaction failed", "reaction", rs.reactionName, "error", err)
	}
	return nil
}
