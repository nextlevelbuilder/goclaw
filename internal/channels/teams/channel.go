package teams

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/typing"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

const (
	defaultWebhookPath    = "/webhooks/teams"
	maxServiceURLEntries  = 10000 // cap to prevent unbounded memory growth
	serviceURLEvictBatch  = 1000  // evict this many oldest entries when cap reached
)

// Compile-time interface assertions.
var (
	_ channels.Channel           = (*Channel)(nil)
	_ channels.WebhookChannel    = (*Channel)(nil)
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
	typingCtrls sync.Map // conversationID → *typing.Controller
}

// New creates a new Microsoft Teams channel.
func New(cfg config.TeamsConfig, msgBus *bus.MessageBus) (*Channel, error) {
	if cfg.BotID == "" || cfg.BotPassword == "" {
		return nil, fmt.Errorf("teams bot_id and bot_password are required")
	}

	// Default and validate bot type
	if cfg.BotType == "" {
		cfg.BotType = "SingleTenant"
	}
	switch cfg.BotType {
	case "SingleTenant", "MultiTenant":
		// valid
	default:
		return nil, fmt.Errorf("teams bot_type must be 'SingleTenant' or 'MultiTenant', got %q", cfg.BotType)
	}
	if cfg.WebhookPath == "" {
		cfg.WebhookPath = defaultWebhookPath
	}

	// SingleTenant requires tenant_id (must be a valid UUID)
	tenantID := cfg.TenantID
	if cfg.BotType == "SingleTenant" && tenantID == "" {
		return nil, fmt.Errorf("teams tenant_id is required for SingleTenant bot")
	}
	if tenantID != "" {
		if _, err := uuid.Parse(tenantID); err != nil {
			return nil, fmt.Errorf("teams tenant_id must be a valid UUID: %w", err)
		}
	}
	// MultiTenant: don't enforce tenant_id in JWT validation or token acquisition
	if cfg.BotType == "MultiTenant" {
		tenantID = ""
	}

	base := channels.NewBaseChannel(channels.TypeTeams, msgBus, cfg.AllowFrom)
	base.ValidatePolicy(cfg.DMPolicy, cfg.GroupPolicy)

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

// Stop gracefully shuts down the channel and cleans up typing controllers.
func (c *Channel) Stop(_ context.Context) error {
	c.SetRunning(false)
	c.typingCtrls.Range(func(key, value any) bool {
		value.(*typing.Controller).Stop()
		c.typingCtrls.Delete(key)
		return true
	})
	slog.Info("teams channel stopped")
	return nil
}

// sendTyping sends a typing indicator activity to a conversation.
func (c *Channel) sendTyping(conversationID string) error {
	serviceURL, ok := c.serviceURLs.Load(conversationID)
	if !ok {
		return nil // no serviceURL yet, skip silently
	}
	activity := Activity{Type: "typing"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.client.doSendActivity(ctx, serviceURL.(string), conversationID, activity)
}

// startTyping starts a typing indicator controller for a conversation.
func (c *Channel) startTyping(conversationID string) {
	ctrl := typing.New(typing.Options{
		MaxDuration:       60 * time.Second,
		KeepaliveInterval: 3 * time.Second, // Teams typing auto-expires in 3s
		StartFn: func() error {
			return c.sendTyping(conversationID)
		},
	})
	ctrl.Start()
	// Stop previous controller if exists (rapid messages)
	if prev, loaded := c.typingCtrls.Swap(conversationID, ctrl); loaded {
		prev.(*typing.Controller).Stop()
	}
}

// stopTyping stops and removes the typing indicator for a conversation.
func (c *Channel) stopTyping(chatID string) {
	if ctrl, ok := c.typingCtrls.LoadAndDelete(chatID); ok {
		ctrl.(*typing.Controller).Stop()
	}
}

// Send delivers an outbound message to a Teams conversation.
func (c *Channel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return fmt.Errorf("teams: channel not running")
	}

	// Stop typing indicator before sending reply
	c.stopTyping(msg.ChatID)

	serviceURL, ok := c.serviceURLs.Load(msg.ChatID)
	if !ok {
		// Fallback: extract serviceURL from message metadata (survives restart).
		// The inbound webhook handler stores service_url in metadata.
		if surl := msg.Metadata["service_url"]; surl != "" && isValidServiceURL(surl) {
			c.storeServiceURL(msg.ChatID, surl)
			serviceURL = surl
			ok = true
		}
	}
	if !ok {
		return fmt.Errorf("teams: no serviceURL for conversation %s", msg.ChatID)
	}

	text := msg.Content
	if text == "" {
		return nil
	}

	// Sanitize markdown and split into chunks (Teams limit: 80KB)
	text = sanitizeForTeams(text)
	chunks := chunkMarkdown(text, teamsMaxMessageBytes)

	for _, chunk := range chunks {
		activity := Activity{
			Type: "message",
			Text: chunk,
			// textFormat defaults to "markdown" in Teams — no need to set
		}
		if err := c.client.retrySendActivity(ctx, serviceURL.(string), msg.ChatID, activity); err != nil {
			slog.Error("teams: failed to send chunk",
				"conversation", msg.ChatID,
				"chunk_len", len(chunk),
				"error", err,
			)
			return err
		}
	}

	return nil
}

// WebhookHandler returns the HTTP handler and path for mounting on the gateway mux.
// DB instances get per-instance paths (e.g. /webhooks/teams/my-bot) to support
// multi-bot deployments with different Azure Bot registrations.
// Config-based channel (name == "teams") keeps the default path.
func (c *Channel) WebhookHandler() (string, http.Handler) {
	path := c.cfg.WebhookPath
	if name := c.Name(); name != "" && name != channels.TypeTeams {
		path = strings.TrimRight(path, "/") + "/" + name
	}
	return path, http.HandlerFunc(c.handleWebhook)
}

// BlockReplyEnabled returns the channel-level block_reply override.
func (c *Channel) BlockReplyEnabled() *bool {
	return c.cfg.BlockReply
}

// storeServiceURL validates and stores the serviceURL for a conversation.
// Only accepts HTTPS URLs from known Bot Framework domains to prevent SSRF.
// Evicts oldest entries when the map exceeds maxServiceURLEntries to prevent unbounded memory growth.
func (c *Channel) storeServiceURL(conversationID, serviceURL string) {
	if !isValidServiceURL(serviceURL) {
		slog.Warn("security.teams: rejected invalid serviceURL",
			"service_url", serviceURL,
			"conversation", conversationID,
		)
		return
	}
	c.serviceURLs.Store(conversationID, serviceURL)

	// Evict oldest entries if over cap. Range iteration order is non-deterministic,
	// which provides pseudo-random eviction. Active conversations will re-store
	// their URL from the next inbound webhook.
	count := 0
	c.serviceURLs.Range(func(_, _ any) bool { count++; return true })
	if count > maxServiceURLEntries {
		evicted := 0
		c.serviceURLs.Range(func(key, _ any) bool {
			if key.(string) == conversationID {
				return true // don't evict the one we just stored
			}
			c.serviceURLs.Delete(key)
			evicted++
			return evicted < serviceURLEvictBatch
		})
		slog.Debug("teams: evicted stale serviceURL entries", "evicted", evicted)
	}
}

// isValidServiceURL checks that a serviceURL is HTTPS and from a known Bot Framework domain.
// Real Teams serviceURLs: smba.trafficmanager.net, *.botframework.com, *.teams.microsoft.com.
// Only smba.trafficmanager.net is allowed (not all *.trafficmanager.net) to prevent token exfiltration.
func isValidServiceURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	allowedSuffixes := []string{
		".botframework.com",
		".teams.microsoft.com",
	}
	allowedExact := []string{
		"botframework.com",
		"teams.microsoft.com",
		"smba.trafficmanager.net",
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
