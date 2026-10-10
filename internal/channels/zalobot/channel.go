package zalobot

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	maxChunkLength = 2000
)

type Channel struct {
	*channels.BaseChannel
	cfg     config.ZaloBotConfig
	client  *Client
	botUser *BotUser
	handler *InboundHandler
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	lastOff int64
	mu      sync.Mutex
}

func New(cfg config.ZaloBotConfig, msgBus *bus.MessageBus, pairingSvc store.PairingStore) (*Channel, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("zalo bot token is required")
	}

	base := channels.NewBaseChannel(channels.TypeZaloBot, msgBus, cfg.AllowFrom)
	base.SetName("zalo_bot")
	base.SetType(channels.TypeZaloBot)
	base.SetPairingService(pairingSvc)

	client := NewClient(cfg.Token)

	ch := &Channel{
		BaseChannel: base,
		cfg:         cfg,
		client:      client,
	}

	return ch, nil
}

func (c *Channel) Start(ctx context.Context) error {
	botUser, err := c.client.GetMe(ctx)
	if err != nil {
		return fmt.Errorf("getMe failed: %w", err)
	}

	c.mu.Lock()
	c.botUser = botUser
	c.handler = NewInboundHandler(c.BaseChannel, botUser, c.cfg, func(ctx context.Context, chatID, text string) error {
		return c.client.SendMessage(ctx, chatID, text)
	})
	c.mu.Unlock()

	slog.Info("zalo_bot channel authorized", "bot_id", botUser.ID, "bot_name", botUser.Name, "username", botUser.Username)

	pollCtx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel

	c.wg.Add(1)
	go c.pollLoop(pollCtx)

	return nil
}

func (c *Channel) Stop(ctx context.Context) error {
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()
	return nil
}

func (c *Channel) pollLoop(ctx context.Context) {
	defer c.wg.Done()

	timeoutSec := c.cfg.PollTimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = 30
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		updates, err := c.client.GetUpdates(ctx, c.lastOff, 50, timeoutSec)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("zalobot getUpdates error, backing off", "error", err)
			select {
			case <-time.After(3 * time.Second):
			case <-ctx.Done():
				return
			}
			continue
		}

		for _, u := range updates {
			if u.UpdateID >= c.lastOff {
				c.lastOff = u.UpdateID + 1
			}

			c.mu.Lock()
			handler := c.handler
			c.mu.Unlock()

			if handler != nil {
				if err := handler.ProcessUpdate(ctx, u); err != nil {
					slog.Warn("failed to process zalobot update", "update_id", u.UpdateID, "error", err)
				}
			}
		}
	}
}

func (c *Channel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	chatID := msg.ChatID
	if chatID == "" {
		return fmt.Errorf("missing target chat id")
	}

	// 1. Send photos if any
	for _, m := range msg.Media {
		caption := msg.Content
		if err := c.client.SendPhoto(ctx, chatID, m.URL, zalo.StripMarkdown(caption)); err != nil {
			return fmt.Errorf("send photo: %w", err)
		}
		// If photo sent with caption, we don't repeat the caption as a text message
		return nil
	}

	// 2. Send text message with stripped markdown and chunking
	plainText := zalo.StripMarkdown(msg.Content)
	if plainText == "" {
		return nil
	}

	chunks := channels.ChunkMarkdown(plainText, maxChunkLength)
	if len(chunks) == 0 {
		chunks = []string{plainText}
	}

	for _, chunk := range chunks {
		if err := c.client.SendMessage(ctx, chatID, chunk); err != nil {
			return fmt.Errorf("send text chunk: %w", err)
		}
	}

	return nil
}
