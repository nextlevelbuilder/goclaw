package teams

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

const defaultWebhookPath = "/webhooks/teams"

// Compile-time interface assertions.
var (
	_ channels.Channel          = (*Channel)(nil)
	_ channels.WebhookChannel   = (*Channel)(nil)
	_ channels.BlockReplyChannel = (*Channel)(nil)
)

// Channel connects to Microsoft Teams via Azure Bot Framework REST API.
// Implements channels.Channel, channels.WebhookChannel, and channels.BlockReplyChannel.
type Channel struct {
	*channels.BaseChannel
	cfg         config.TeamsConfig
	validator   *tokenValidator
	client      *botClient
	serviceURLs sync.Map // conversationID → serviceURL (string)
}

// New creates a new Microsoft Teams channel.
func New(cfg config.TeamsConfig, msgBus *bus.MessageBus) (*Channel, error) {
	if cfg.BotID == "" || cfg.BotPassword == "" {
		return nil, fmt.Errorf("teams bot_id and bot_password are required")
	}

	// Default bot type to SingleTenant
	if cfg.BotType == "" {
		cfg.BotType = "SingleTenant"
	}
	if cfg.WebhookPath == "" {
		cfg.WebhookPath = defaultWebhookPath
	}

	// SingleTenant requires tenant_id
	tenantID := cfg.TenantID
	if cfg.BotType == "SingleTenant" && tenantID == "" {
		return nil, fmt.Errorf("teams tenant_id is required for SingleTenant bot")
	}
	// MultiTenant: don't enforce tenant_id in JWT validation or token acquisition
	if cfg.BotType == "MultiTenant" {
		tenantID = ""
	}

	base := channels.NewBaseChannel(channels.TypeTeams, msgBus, cfg.AllowFrom)
	base.ValidatePolicy(cfg.DMPolicy, "")

	ch := &Channel{
		BaseChannel: base,
		cfg:         cfg,
		validator:   newTokenValidator(cfg.BotID, tenantID),
		client:      newBotClient(cfg.BotID, cfg.BotPassword, tenantID),
	}

	return ch, nil
}

// Start is a no-op for webhook-based channels (HTTP handler is mounted by gateway).
func (c *Channel) Start(_ context.Context) error {
	c.SetRunning(true)
	slog.Info("teams channel started", "webhook_path", c.cfg.WebhookPath)
	return nil
}

// Stop gracefully shuts down the channel.
func (c *Channel) Stop(_ context.Context) error {
	c.SetRunning(false)
	slog.Info("teams channel stopped")
	return nil
}

// Send delivers an outbound message to a Teams conversation.
func (c *Channel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return fmt.Errorf("teams: channel not running")
	}

	serviceURL, ok := c.serviceURLs.Load(msg.ChatID)
	if !ok {
		return fmt.Errorf("teams: no serviceURL for conversation %s", msg.ChatID)
	}

	text := msg.Content
	if text == "" {
		return nil
	}

	// Truncate to Teams message limit (~28KB, use 25000 chars as safe limit)
	text = channels.Truncate(text, 25000)

	if err := c.client.SendReply(ctx, serviceURL.(string), msg.ChatID, text); err != nil {
		slog.Error("teams: failed to send reply",
			"conversation", msg.ChatID,
			"error", err,
		)
		return err
	}

	return nil
}

// WebhookHandler returns the HTTP handler and path for mounting on the gateway mux.
func (c *Channel) WebhookHandler() (string, http.Handler) {
	return c.cfg.WebhookPath, http.HandlerFunc(c.handleWebhook)
}

// BlockReplyEnabled returns the channel-level block_reply override.
func (c *Channel) BlockReplyEnabled() *bool {
	return c.cfg.BlockReply
}

// storeServiceURL validates and stores the serviceURL for a conversation.
// Only accepts HTTPS URLs from known Bot Framework domains to prevent SSRF.
func (c *Channel) storeServiceURL(conversationID, serviceURL string) {
	if !isValidServiceURL(serviceURL) {
		slog.Warn("security.teams: rejected invalid serviceURL",
			"service_url", serviceURL,
			"conversation", conversationID,
		)
		return
	}
	c.serviceURLs.Store(conversationID, serviceURL)
}

// isValidServiceURL checks that a serviceURL is HTTPS and from a known Bot Framework domain.
// Real Teams serviceURLs include: smba.trafficmanager.net, *.botframework.com, *.teams.microsoft.com
func isValidServiceURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	allowedSuffixes := []string{
		".botframework.com",
		".teams.microsoft.com",
		".trafficmanager.net",
	}
	allowedExact := []string{
		"botframework.com",
		"teams.microsoft.com",
	}
	for _, s := range allowedSuffixes {
		if strings.HasSuffix(host, s) {
			return true
		}
	}
	for _, s := range allowedExact {
		if host == s {
			return true
		}
	}
	return false
}
