package line

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/line/line-bot-sdk-go/v7/linebot"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// replyTokenEntry caches a reply token with its receive time.
type replyTokenEntry struct {
	token      string
	receivedAt time.Time
}

// Channel implements the LINE Messaging API channel.
type Channel struct {
	*channels.BaseChannel
	bot            *linebot.Client
	cfg            config.LineConfig
	pairingService store.PairingStore
	replyTokens    sync.Map // chatID → replyTokenEntry
}

// New creates a new LINE channel.
func New(cfg config.LineConfig, msgBus *bus.MessageBus, pairingSvc store.PairingStore) (*Channel, error) {
	bot, err := linebot.New(cfg.ChannelSecret, cfg.ChannelAccessToken)
	if err != nil {
		return nil, err
	}

	base := channels.NewBaseChannel("line", msgBus, cfg.AllowFrom)
	base.ValidatePolicy(cfg.DMPolicy, cfg.GroupPolicy)

	return &Channel{
		BaseChannel:    base,
		bot:            bot,
		cfg:            cfg,
		pairingService: pairingSvc,
	}, nil
}

// Type returns the channel type.
func (c *Channel) Type() string { return "line" }

// Start begins listening. Webhook mode — nothing to poll.
func (c *Channel) Start(_ context.Context) error {
	c.SetRunning(true)
	slog.Info("LINE channel started (webhook mode)")
	return nil
}

// Stop shuts down the channel.
func (c *Channel) Stop(_ context.Context) error {
	c.SetRunning(false)
	slog.Info("LINE channel stopped")
	return nil
}

// WebhookHandler returns the HTTP path and handler for LINE webhook callbacks.
func (c *Channel) WebhookHandler() (string, http.Handler) {
	return "/webhook/line", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		events, err := c.bot.ParseRequest(r)
		if err != nil {
			if err == linebot.ErrInvalidSignature {
				slog.Warn("LINE webhook: invalid signature")
				w.WriteHeader(http.StatusBadRequest)
			} else {
				slog.Error("LINE webhook: parse error", "err", err)
				w.WriteHeader(http.StatusInternalServerError)
			}
			return
		}

		for _, event := range events {
			go c.handleEvent(event)
		}

		w.WriteHeader(http.StatusOK)
	})
}
