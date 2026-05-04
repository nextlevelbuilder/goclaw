// Package bot implements the Zalo Bot channel (static-token variant).
// API: https://bot-api.zaloplatforms.com
// DM only, text limit 2000 chars, polling + webhook modes.
package bot

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/typing"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/common"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const (
	maxTextLength     = 2000
	defaultMediaMaxMB = 5
	pairingDebounce   = 60 * time.Second
)

// Channel connects to the Zalo Bot API.
type Channel struct {
	*channels.BaseChannel
	token      string
	dmPolicy   string
	mediaMaxMB int
	blockReply *bool
	stopCh      chan struct{}
	client      *http.Client
	pollClient  *http.Client
	mediaClient *http.Client

	transport     string // "webhook" (default) | "polling"
	webhookPath   string // slug suffix appended to /channels/zalo/webhook/
	webhookSecret string // guards X-Bot-Api-Secret-Token in webhook mode
	botID         string // from getMe; used to filter self-echoes
	instanceID    uuid.UUID

	webhookRouter *common.Router
	resolvedSlug  string

	bootstrapDroppedCount atomic.Int64

	pollWG   sync.WaitGroup
	stopOnce sync.Once

	legacyPhotoSentinelWarn sync.Once

	typingCtrls sync.Map
}

func (c *Channel) SetInstanceID(id uuid.UUID) { c.instanceID = id }

// inBootstrap: webhook with no secret yet — slug registered so Zalo's
// setWebhook ping returns 200, but events drop until secret is pasted.
func (c *Channel) inBootstrap() bool {
	return c.transport == "webhook" && c.webhookSecret == ""
}

var _ channels.WebhookChannel = (*Channel)(nil)

// WebhookHandler returns (path, handler) on the first caller across the
// shared router; subsequent calls return ("", nil). Per-instance dispatch
// uses the slug suffix of the path: /channels/zalo/webhook/<slug>.
func (c *Channel) WebhookHandler() (string, http.Handler) {
	return common.SharedRouter().MountRoute()
}

// ResolvedWebhookSlug returns the slug the channel registered with the shared
// router (empty if not yet started or polling mode).
func (c *Channel) ResolvedWebhookSlug() string { return c.resolvedSlug }

// New creates a Zalo Bot channel.
func New(cfg config.ZaloConfig, msgBus *bus.MessageBus, pairingSvc store.PairingStore) (*Channel, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("zalo token is required")
	}

	base := channels.NewBaseChannel("zalo", msgBus, cfg.AllowFrom)
	base.ValidatePolicy(cfg.DMPolicy, "")

	dmPolicy := cfg.DMPolicy
	if dmPolicy == "" {
		dmPolicy = "pairing"
	}

	mediaMax := cfg.MediaMaxMB
	if mediaMax <= 0 {
		mediaMax = defaultMediaMaxMB
	}

	transport := cfg.Transport
	if transport == "" {
		transport = "webhook"
	}

	ch := &Channel{
		BaseChannel:   base,
		token:         cfg.Token,
		dmPolicy:      dmPolicy,
		mediaMaxMB:    mediaMax,
		blockReply:    cfg.BlockReply,
		stopCh:        make(chan struct{}),
		client:        &http.Client{Timeout: 60 * time.Second},
		pollClient:    &http.Client{Timeout: 0},
		mediaClient:   tools.NewSSRFSafeClient(60 * time.Second),
		transport:     transport,
		webhookPath:   cfg.WebhookPath,
		webhookSecret: cfg.WebhookSecret,
		webhookRouter: common.SharedRouter(),
	}
	ch.SetPairingService(pairingSvc)
	return ch, nil
}

// BlockReplyEnabled returns the per-channel block_reply override
// (nil = inherit gateway default).
func (c *Channel) BlockReplyEnabled() *bool { return c.blockReply }

// Start begins listening. polling: long-poll getUpdates.
// webhook: register with common.Router; HandleWebhookEvent dispatches.
func (c *Channel) Start(ctx context.Context) error {
	info, err := c.getMe()
	if err != nil {
		return fmt.Errorf("zalo getMe failed: %w", err)
	}
	c.botID = info.ID
	slog.Info("zalo bot connected",
		"bot_id", info.ID, "bot_name", info.Name, "transport", c.transport)

	c.SetRunning(true)

	switch c.transport {
	case "webhook":
		slug := c.webhookPath
		if slug == "" {
			slug = common.DeriveSlugFromName(c.Name())
		}
		if err := c.webhookRouter.RegisterInstance(c.instanceID, c, c.TenantID(), slug); err != nil {
			c.MarkFailed(
				"webhook slug invalid",
				err.Error(),
				channels.ChannelFailureKindConfig,
				false,
			)
			c.SetRunning(false)
			return nil
		}
		c.resolvedSlug = slug

		if c.inBootstrap() {
			c.MarkBootstrap(
				channels.ChannelBootstrapAwaitingSecret,
				"awaiting webhook secret",
				"Bot Webhook Secret not yet set. Webhook acks Zalo's setWebhook verification ping (HTTP 200) but drops events. Paste the same secret you registered with setWebhook in Credentials → Webhook Secret to enable X-Bot-Api-Secret-Token verification.",
				channels.ChannelFailureKindConfig,
				true,
			)
			slog.Info("zalo_bot.webhook.bootstrap_active",
				"instance_id", c.instanceID, "bot_id", c.botID, "slug", slug)
			return nil
		}

		slog.Info("zalo_bot.webhook.registered",
			"instance_id", c.instanceID, "bot_id", c.botID, "slug", slug)
		c.MarkHealthy("webhook")
	case "polling":
		// getUpdates 400s while a webhook URL is registered; clear it.
		if err := c.deleteWebhook(); err != nil {
			slog.Warn("zalo_bot.poll.delete_webhook_failed",
				"instance_id", c.instanceID, "bot_id", c.botID, "err", err)
		}
		c.pollWG.Add(1)
		go c.pollLoop(ctx)
		c.MarkHealthy("polling")
	default:
		c.MarkFailed(
			"unknown transport",
			fmt.Sprintf("zalo_bot: unknown transport %q (expected webhook or polling)", c.transport),
			channels.ChannelFailureKindConfig,
			false,
		)
		c.SetRunning(false)
		return nil
	}
	return nil
}

// Stop shuts down the bot; webhook mode unregisters from the shared router.
func (c *Channel) Stop(_ context.Context) error {
	slog.Info("stopping zalo bot", "transport", c.transport)
	if c.transport == "webhook" && c.webhookRouter != nil {
		c.webhookRouter.UnregisterInstance(c.instanceID)
	}
	c.stopOnce.Do(func() { close(c.stopCh) })
	c.SetRunning(false)

	c.typingCtrls.Range(func(key, val any) bool {
		if ctrl, ok := val.(*typing.Controller); ok {
			ctrl.Stop()
		}
		c.typingCtrls.Delete(key)
		return true
	})
	c.pollWG.Wait()
	return nil
}

// Send delivers an outbound message.
func (c *Channel) Send(_ context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return fmt.Errorf("zalo bot not running")
	}

	if ctrl, ok := c.typingCtrls.LoadAndDelete(msg.ChatID); ok {
		ctrl.(*typing.Controller).Stop()
	}

	// Zalo Bot doesn't render markup.
	msg.Content = common.StripMarkdown(msg.Content)

	if strings.Contains(msg.Content, "[photo:") {
		c.legacyPhotoSentinelWarn.Do(func() {
			slog.Warn("zalo_bot.send.legacy_photo_sentinel_detected",
				"chat_id", msg.ChatID,
				"hint", "switch caller to bus.OutboundMessage.Media[]")
		})
	}

	if len(msg.Media) == 0 {
		return c.sendChunkedText(msg.ChatID, msg.Content)
	}
	if len(msg.Media) > 1 {
		slog.Info("zalo_bot.send.extra_media_skipped",
			"chat_id", msg.ChatID, "extra", len(msg.Media)-1)
	}

	m := msg.Media[0]
	if !isHTTPURL(m.URL) {
		return fmt.Errorf("zalo_bot: local file media not supported; use zalo_oa channel (got %q)", m.URL)
	}
	caption := mergeTrailingText(m.Caption, msg.Content)
	return c.sendPhoto(msg.ChatID, m.URL, caption)
}

